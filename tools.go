package main

import (
	"context"
	"fmt"
	"log"

	"github.com/modelcontextprotocol/go-sdk/jsonschema"
)

// ToolHandlerParams contains parameters passed to a tool's handler function.
type ToolHandlerParams struct {
	ModelName string
	SessionId string
}

// ToolDefinition represents a tool with its schema and handler function.
type ToolDefinition struct {
	Name        string
	Description string
	Parameters  *Schema
	Handler     func(ctx context.Context, args map[string]interface{}, params ToolHandlerParams) (map[string]interface{}, error)
}

// Define all available tools here
var availableTools = map[string]ToolDefinition{
	"list_directory": {
		Name:        "list_directory",
		Description: "Lists a directory. Can be also used to access the session-local anonymous working directory, which is useful for e.g. storing `NOTES.md`.",
		Parameters: &Schema{
			Type: TypeObject,
			Properties: map[string]*Schema{
				"path": {
					Type:        TypeString,
					Description: "The path to the directory to list. Both absolute and relative paths are supported. Relative paths are resolved against the session's anonymous working directory.",
				},
			},
			Required: []string{"path"},
		},
		Handler: ListDirectoryTool,
	},
	"read_file": {
		Name:        "read_file",
		Description: "Reads a file. Can be also used to access the session-local anonymous working directory, which is useful for e.g. storing `NOTES.md`.",
		Parameters: &Schema{
			Type: TypeObject,
			Properties: map[string]*Schema{
				"file_path": {
					Type:        TypeString,
					Description: "The path to the file to read. Both absolute and relative paths are supported. Relative paths are resolved against the session's anonymous working directory.",
				},
			},
			Required: []string{"file_path"},
		},
		Handler: ReadFileTool,
	},
	"write_file": {
		Name:        "write_file",
		Description: "Writes content to a specified file. Can be also used to access the session-local anonymous working directory, which is useful for e.g. storing `NOTES.md`.",
		Parameters: &Schema{
			Type: TypeObject,
			Properties: map[string]*Schema{
				"file_path": {
					Type:        TypeString,
					Description: "The path to the file to write to. Both absolute and relative paths are supported. Relative paths are resolved against the session's anonymous working directory.",
				},
				"content": {
					Type:        TypeString,
					Description: "The content to write to the file.",
				},
			},
			Required: []string{"file_path", "content"},
		},
		Handler: WriteFileTool,
	},
	"web_fetch": {
		Name:        "web_fetch",
		Description: "Processes content from URL(s). Your prompt and instructions are forwarded to an internal AI agent that fetches and interprets the content. While not a direct search engine, it can retrieve content from HTML-only search result pages (e.g., `https://html.duckduckgo.com/html?q=query`). Without explicit instructions, the agent may summarize or extract key data for efficient information retrieval. Clear directives are required to obtain the original or full content.",
		Parameters: &Schema{
			Type: TypeObject,
			Properties: map[string]*Schema{
				"prompt": {
					Type:        TypeString,
					Description: "A comprehensive prompt that includes the URL(s) (up to 20) to fetch and specific instructions on how to process their content (e.g., \"Summarize https://example.com/article and extract key points from https://another.com/data\"). To retrieve the full, unsummarized content, you must include explicit instructions such as 'return full content', 'do not summarize', or 'provide original text'. Must contain at least one URL starting with http:// or https://.",
				},
			},
			Required: []string{"prompt"},
		},
		Handler: WebFetchTool,
	},
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
	for _, toolDef := range availableTools {
		functionDeclarations = append(functionDeclarations, FunctionDeclaration{
			Name:        toolDef.Name,
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
func CallToolFunction(ctx context.Context, fc FunctionCall, params ToolHandlerParams) (map[string]interface{}, error) {
	// Check if it's a local tool first
	if toolDef, ok := availableTools[fc.Name]; ok {
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
						return mcpManager.DispatchToolCall(context.Background(), mcpName, originalToolName, fc.Args)
					}
				}
			}
		}
	}

	return nil, fmt.Errorf("unknown tool: %s", fc.Name)
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
