package main

import (
	"context"
	"fmt"
	"log"

	"github.com/modelcontextprotocol/go-sdk/jsonschema"
)

// ToolHandlerParams contains parameters passed to a tool's handler function.
type ToolHandlerParams struct {
	ModelName            string
	SessionId            string
	BranchId             string
	ConfirmationReceived bool
}

// ToolHandlerResults contains the result of a tool's handler function, including its value and any attachments.
type ToolHandlerResults struct {
	Value       map[string]interface{}
	Attachments []FileAttachment
}

// ToolDefinition represents a tool with its schema and handler function.
type ToolDefinition struct {
	Name        string
	Description string
	Parameters  *Schema
	Handler     func(ctx context.Context, args map[string]interface{}, params ToolHandlerParams) (ToolHandlerResults, error)
}

// Define all available tools here
var availableTools = map[string]ToolDefinition{
	// tools_fs.go
	"list_directory": listDirectoryToolDefinition,
	"read_file":      readFileToolDefinition,
	"write_file":     writeFileToolDefinition,

	// tools_webfetch.go
	"web_fetch": webFetchToolDefinition,

	// tools_todo.go
	"write_todo": writeTodoToolDefinition,

	// tools_shell.go
	"run_shell_command":  runShellCommandToolDefinition,
	"poll_shell_command": pollShellCommandToolDefinition,
	"kill_shell_command": killShellCommandToolDefinition,

	// task_subagent_tool_definitions.go - Subagent tools (Removed to avoid cyclic dependency)
	// "subagent_spawn": subagentSpawnToolDefinition,
	// "subagent_turn":  subagentTurnToolDefinition,
}

func GetBuiltinToolNames() map[string]bool {
	builtinToolNames := make(map[string]bool)
	for toolName := range availableTools {
		builtinToolNames[toolName] = true
	}
	return builtinToolNames
}

// GetToolsForGemini returns a slice of Tool for Gemini API.
func GetToolsForGemini() []Tool {
	var tools []Tool
	var functionDeclarations []FunctionDeclaration

	// Add local tools
	builtinToolNames := make(map[string]bool)
	for toolName, toolDef := range availableTools {
		functionDeclarations = append(functionDeclarations, FunctionDeclaration{
			Name:        toolName,
			Description: toolDef.Description,
			Parameters:  toolDef.Parameters,
		})
		builtinToolNames[toolDef.Name] = true
	}

	// Update MCP tool name mapping
	mcpManager.UpdateToolNameMapping(builtinToolNames)

	// Add tools from active MCP connections with name conflict resolution
	mcpManager.mu.RLock()
	defer mcpManager.mu.RUnlock()
	for mcpName, conn := range mcpManager.connections {
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

// convertJSONSchemaToGeminiSchema converts a jsonschema.Schema to a Gemini API compatible Schema.
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

// CallToolFunction executes the handler for the given function call.
func CallToolFunction(ctx context.Context, fc FunctionCall, params ToolHandlerParams) (ToolHandlerResults, error) {
	// Check if it's a local tool first
	if toolDef, ok := availableTools[fc.Name]; ok {
		// Pass the DB instance from params to the tool handler
		return toolDef.Handler(ctx, fc.Args, params)
	}

	// Check if it's an MCP tool (potentially with a mapped name)
	mcpManager.mu.RLock()
	defer mcpManager.mu.RUnlock()

	originalToolName, isMCPTool := mcpManager.mcpToolNameMapping[fc.Name]
	if isMCPTool {
		// Find the MCP server that provides this original tool name
		for mcpName, conn := range mcpManager.connections {
			if conn.IsEnabled && conn.Session != nil {
				toolsIterator := conn.Session.Tools(context.Background(), nil)
				for tool, err := range toolsIterator {
					if err != nil {
						break // Cannot check this server
					}
					if tool.Name == originalToolName {
						log.Printf("Dispatching tool call '%s' (originally '%s') to MCP server '%s'", fc.Name, originalToolName, mcpName)
						val, err := mcpManager.DispatchToolCall(context.Background(), mcpName, originalToolName, fc.Args)
						return ToolHandlerResults{Value: val}, err
					}
				}
			}
		}
	}

	return ToolHandlerResults{}, fmt.Errorf("unknown tool: %s", fc.Name)
}

// EnsureKnownKeys checks if all keys in 'args' are present in 'keys'.
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
