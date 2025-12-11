package tool

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/jsonschema"

	. "github.com/lifthrasiir/angel/gemini"
	. "github.com/lifthrasiir/angel/internal/types"
)

// PendingConfirmation is a special error type used to signal that user confirmation is required.
type PendingConfirmation struct {
	Data any
}

func (e *PendingConfirmation) Error() string {
	return "user confirmation required"
}

// HandlerParams contains parameters passed to a tool's handler function.
type HandlerParams struct {
	ModelName            string
	SessionId            string
	BranchId             string
	ConfirmationReceived bool
}

// HandlerResults contains the result of a tool's handler function, including its value and any attachments.
type HandlerResults struct {
	Value       map[string]interface{}
	Attachments []FileAttachment
}

// Definition represents a tool with its schema and handler function.
type Definition struct {
	Name        string
	Description string
	Parameters  *Schema
	Handler     func(ctx context.Context, args map[string]interface{}, params HandlerParams) (HandlerResults, error)
}

// Tools manages all tool state including built-in tools and MCP connections
type Tools struct {
	builtinTools       map[string]Definition
	mcpManager         *MCPManager
	mu                 sync.RWMutex
	mcpToolNameMapping map[string]string // MappedName -> OriginalName
}

// NewTools creates a new Tools instance
func NewTools() *Tools {
	return &Tools{
		builtinTools:       make(map[string]Definition),
		mcpManager:         &MCPManager{connections: make(map[string]*MCPConnection)},
		mcpToolNameMapping: make(map[string]string),
	}
}

// Register registers a built-in tool
func (t *Tools) Register(def Definition) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.builtinTools[def.Name] = def
}

// BuiltinNames returns a set of all built-in tool names
func (t *Tools) BuiltinNames() map[string]bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	builtinToolNames := make(map[string]bool)
	for toolName := range t.builtinTools {
		builtinToolNames[toolName] = true
	}
	return builtinToolNames
}

// InitMCPManager initializes MCP connections from database
func (t *Tools) InitMCPManager(db *sql.DB) {
	t.mcpManager.init(t, db)
}

// ForGemini returns a slice of Tool for Gemini API
func (t *Tools) ForGemini() []Tool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var tools []Tool
	var functionDeclarations []FunctionDeclaration

	// Add local tools
	builtinToolNames := make(map[string]bool)
	for toolName, toolDef := range t.builtinTools {
		functionDeclarations = append(functionDeclarations, FunctionDeclaration{
			Name:        toolName,
			Description: toolDef.Description,
			Parameters:  toolDef.Parameters,
		})
		builtinToolNames[toolDef.Name] = true
	}

	// Update MCP tool name mapping
	t.updateToolNameMapping(builtinToolNames)

	// Add tools from active MCP connections with name conflict resolution
	for mcpName, conn := range t.mcpManager.connections {
		if conn.IsEnabled && conn.Session != nil {
			toolsIterator := conn.Session.Tools(context.Background(), nil)
			for tool, err := range toolsIterator {
				if err != nil {
					log.Printf("Failed to list tools from MCP server %s: %v", mcpName, err)
					break
				}
				mappedName := tool.Name
				if _, exists := builtinToolNames[tool.Name]; exists {
					mappedName = mcpName + "__" + tool.Name
				}
				functionDeclarations = append(functionDeclarations, FunctionDeclaration{
					Name:        mappedName,
					Description: tool.Description,
					Parameters:  convertJSONSchemaToGeminiSchema(tool.InputSchema),
				})
			}
		}
	}

	tools = append(tools, Tool{FunctionDeclarations: functionDeclarations})
	return tools
}

// updateToolNameMapping updates the mapping for MCP tool names
func (t *Tools) updateToolNameMapping(builtinToolNames map[string]bool) {
	t.mcpToolNameMapping = make(map[string]string)

	for mcpName, conn := range t.mcpManager.connections {
		if conn.IsEnabled && conn.Session != nil {
			toolsIterator := conn.Session.Tools(context.Background(), nil)
			for tool, err := range toolsIterator {
				if err != nil {
					log.Printf("Failed to list tools from MCP server %s for mapping: %v", mcpName, err)
					continue
				}
				mappedName := tool.Name
				if _, exists := builtinToolNames[tool.Name]; exists {
					mappedName = mcpName + "__" + tool.Name
				}
				t.mcpToolNameMapping[mappedName] = tool.Name
			}
		}
	}
}

// Call executes the handler for the given function call
func (t *Tools) Call(ctx context.Context, fc FunctionCall, params HandlerParams) (HandlerResults, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	// Check if it's a local tool first
	if toolDef, ok := t.builtinTools[fc.Name]; ok {
		return toolDef.Handler(ctx, fc.Args, params)
	}

	// Check if it's an MCP tool (potentially with a mapped name)
	originalToolName, isMCPTool := t.mcpToolNameMapping[fc.Name]
	if isMCPTool {
		// Find the MCP server that provides this original tool name
		for mcpName, conn := range t.mcpManager.connections {
			if conn.IsEnabled && conn.Session != nil {
				toolsIterator := conn.Session.Tools(context.Background(), nil)
				for tool, err := range toolsIterator {
					if err != nil {
						break // Cannot check this server
					}
					if tool.Name == originalToolName {
						log.Printf("Dispatching tool call '%s' (originally '%s') to MCP server '%s'", fc.Name, originalToolName, mcpName)
						val, err := t.mcpManager.DispatchToolCall(context.Background(), mcpName, originalToolName, fc.Args)
						return HandlerResults{Value: val}, err
					}
				}
			}
		}
	}

	return HandlerResults{}, fmt.Errorf("unknown tool: %s", fc.Name)
}

// GetMCPConnections returns a snapshot of the current MCP connections
func (t *Tools) GetMCPConnections() map[string]*MCPConnection {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.mcpManager.GetMCPConnections()
}

// DispatchCall sends a tool call to the appropriate MCP server
func (t *Tools) DispatchCall(ctx context.Context, mcpServerName string, toolName string, args map[string]interface{}) (map[string]interface{}, error) {
	return t.mcpManager.DispatchToolCall(ctx, mcpServerName, toolName, args)
}

// GetMCPManager returns the internal MCP manager for advanced operations
func (t *Tools) GetMCPManager() *MCPManager {
	return t.mcpManager
}

// jsonSchemaTypeToGeminiType converts JSON schema type to Gemini type
func jsonSchemaTypeToGeminiType(jsonType string) Type {
	switch jsonType {
	case "string":
		return TypeString
	case "number":
		return TypeNumber
	case "integer":
		return TypeInteger
	case "boolean":
		return TypeBoolean
	case "array":
		return TypeArray
	case "object":
		return TypeObject
	case "null":
		return TypeNull
	default:
		return TypeUnspecified
	}
}

// convertJSONSchemaToGeminiSchema converts a jsonschema.Schema to a Gemini API compatible Schema
func convertJSONSchemaToGeminiSchema(jsonSchema *jsonschema.Schema) *Schema {
	if jsonSchema == nil {
		return nil
	}

	geminiSchema := &Schema{}

	// Handle Type
	if jsonSchema.Type != "" {
		geminiSchema.Type = jsonSchemaTypeToGeminiType(jsonSchema.Type)
	} else if len(jsonSchema.Types) > 0 {
		geminiSchema.Type = jsonSchemaTypeToGeminiType(jsonSchema.Types[0]) // Take the first type if multiple are present
	}

	// Handle Properties recursively
	if len(jsonSchema.Properties) > 0 {
		geminiSchema.Properties = make(map[string]*Schema)
		for key, propSchema := range jsonSchema.Properties {
			geminiSchema.Properties[key] = convertJSONSchemaToGeminiSchema(propSchema)
		}
	}

	// Handle Required
	if len(jsonSchema.Required) > 0 {
		geminiSchema.Required = jsonSchema.Required
	}

	return geminiSchema
}

// EnsureKnownKeys checks if all keys in 'args' are present in 'keys'
func EnsureKnownKeys(toolName string, args map[string]interface{}, keys ...string) error {
	knownKeys := make(map[string]bool)
	for _, k := range keys {
		knownKeys[k] = true
	}

	for argKey := range args {
		if _, ok := knownKeys[argKey]; !ok {
			return fmt.Errorf("unknown argument `%s` in %s tool", argKey, toolName)
		}
	}
	return nil
}
