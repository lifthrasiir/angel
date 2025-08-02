package main

import (
	"context"
	"fmt"
	"log"
)

// Define all available tools here
var availableTools = map[string]ToolDefinition{
	"list_directory": {
		Name:        "list_directory",
		Description: "Lists a directory.",
		Parameters: &Schema{
			Type: TypeObject,
			Properties: map[string]*Schema{
				"path": {
					Type:        TypeString,
					Description: "The absolute path to the directory to list.",
				},
			},
			Required: []string{"path"},
		},
		// Implement the actual function call logic here
		Handler: func(args map[string]interface{}) (map[string]interface{}, error) {
			path, ok := args["path"].(string)
			if !ok {
				return nil, fmt.Errorf("invalid path argument for list_directory")
			}
			// TODO: Call the actual list_directory function from sandbox
			// For now, let's mock it
			log.Printf("Calling mock list_directory for path: %s", path)
			result := []string{"file1.txt", "file2.txt", "subdir/"} // Mock result
			return map[string]interface{}{"files": result}, nil
		},
	},
	"read_file": {
		Name:        "read_file",
		Description: "Reads a file.",
		Parameters: &Schema{
			Type: TypeObject,
			Properties: map[string]*Schema{
				"absolute_path": {
					Type:        TypeString,
					Description: "The absolute path to the file to read.",
				},
			},
			Required: []string{"absolute_path"},
		},
		// Implement the actual function call logic here
		Handler: func(args map[string]interface{}) (map[string]interface{}, error) {
			absolutePath, ok := args["absolute_path"].(string)
			if !ok {
				return nil, fmt.Errorf("invalid absolute_path argument for read_file")
			}
			// TODO: Call the actual read_file function from sandbox
			// For now, let's mock it
			log.Printf("Calling mock read_file for path: %s", absolutePath)
			content := "Mock file content for " + absolutePath // Mock content
			return map[string]interface{}{"content": content}, nil
		},
	},
}

func GetBuiltinToolNames() map[string]bool {
	builtinToolNames := make(map[string]bool)
	for toolName := range availableTools {
		builtinToolNames[toolName] = true
	}
	return builtinToolNames
}

// ToolDefinition represents a tool with its schema and handler function.
type ToolDefinition struct {
	Name        string
	Description string
	Parameters  *Schema
	Handler     func(args map[string]interface{}) (map[string]interface{}, error)
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
					Parameters: &Schema{
						Type: TypeObject,
						Properties: map[string]*Schema{
							"args": {
								Type:        TypeObject,
								Description: "Arguments for the tool.",
							},
						},
					},
				})
			}
		}
	}

	tools = append(tools, Tool{FunctionDeclarations: functionDeclarations})
	return tools
}

// CallToolFunction executes the handler for the given function call.
func CallToolFunction(fc FunctionCall) (map[string]interface{}, error) {
	// Check if it's a local tool first
	if toolDef, ok := availableTools[fc.Name]; ok {
		return toolDef.Handler(fc.Args)
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
