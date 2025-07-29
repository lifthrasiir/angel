package main

import (
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

	for _, toolDef := range availableTools {
		functionDeclarations = append(functionDeclarations, FunctionDeclaration{
			Name:        toolDef.Name,
			Description: toolDef.Description,
			Parameters:  toolDef.Parameters,
		})
	}
	tools = append(tools, Tool{FunctionDeclarations: functionDeclarations})
	return tools
}

// CallToolFunction executes the handler for the given function call.
func CallToolFunction(fc FunctionCall) (map[string]interface{}, error) {
	toolDef, ok := availableTools[fc.Name]
	if !ok {
		return nil, fmt.Errorf("unknown tool: %s", fc.Name)
	}
	return toolDef.Handler(fc.Args)
}
