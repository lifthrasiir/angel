package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	fsPkg "github.com/lifthrasiir/angel/fs"
)

const todoFilePath = "TODO.json"

// TodoItem represents a single TODO item.
type TodoItem struct {
	Content  string `json:"content"`
	Status   string `json:"status"`
	Priority string `json:"priority"`
	ID       string `json:"id"`
}

// readTodos reads the current TODO list from the SessionFS.
func readTodos(sf *fsPkg.SessionFS) ([]TodoItem, error) {
	data, err := sf.ReadFile(todoFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return []TodoItem{}, nil // Return empty list if file does not exist
		}
		return nil, fmt.Errorf("failed to read TODOs file: %w", err)
	}

	if len(data) == 0 {
		return []TodoItem{}, nil
	}

	var todos []TodoItem
	err = json.Unmarshal(data, &todos)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal TODOs: %w", err)
	}
	return todos, nil
}

// writeTodos writes the given TODO list to the SessionFS.
func writeTodos(sf *fsPkg.SessionFS, todos []TodoItem) error {
	jsonData, err := json.MarshalIndent(todos, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal TODOs: %w", err)
	}

	err = sf.WriteFile(todoFilePath, jsonData)
	if err != nil {
		return fmt.Errorf("failed to write TODOs to file: %w", err)
	}
	return nil
}

// generateNextTodoID generates a unique ID for a new TODO item.
func generateNextTodoID(currentTodos []TodoItem) string {
	maxID := 0
	for _, item := range currentTodos {
		id, err := strconv.Atoi(item.ID)
		if err == nil && id > maxID {
			maxID = id
		}
	}
	return strconv.Itoa(maxID + 1)
}

var validStatuses = map[string]bool{
	"pending":     true,
	"in_progress": true,
	"completed":   true,
}

var validPriorities = map[string]bool{
	"low":    true,
	"medium": true,
	"high":   true,
}

// WriteTodoTool handles the write_todo tool call.
func WriteTodoTool(ctx context.Context, args map[string]interface{}, params ToolHandlerParams) (map[string]interface{}, error) {
	sf, err := getSessionFS(ctx, params.SessionId)
	if err != nil {
		return nil, fmt.Errorf("failed to get SessionFS for write_todo: %w", err)
	}
	defer releaseSessionFS(params.SessionId)

	action, ok := args["action"].(string)
	if !ok || action == "" {
		return map[string]interface{}{"error": "Action is required for 'write_todo' tool."},
			fmt.Errorf("missing or invalid 'action' argument")
	}

	currentTodos, err := readTodos(sf)
	if err != nil {
		return map[string]interface{}{"error": fmt.Sprintf("Failed to read TODOs: %v", err)},
			err
	}

	switch action {
	case "add":
		if err := EnsureKnownKeys("write_todo(add)", args, "action", "content", "priority", "status"); err != nil {
			return nil, err
		}
		content, ok := args["content"].(string)
		if !ok || content == "" {
			return map[string]interface{}{"error": "Content is required for 'add' action."},
				fmt.Errorf("missing or invalid 'content' argument for add action")
		}

		newID := generateNextTodoID(currentTodos)
		newTodo := TodoItem{
			Content:  content,
			Status:   "pending",
			Priority: "medium",
			ID:       newID,
		}

		if p, ok := args["priority"].(string); ok {
			if !validPriorities[p] {
				return map[string]interface{}{"error": fmt.Sprintf("Invalid priority: %s. Must be low, medium, or high.", p)},
					fmt.Errorf("invalid priority: %s", p)
			}
			newTodo.Priority = p
		}
		if s, ok := args["status"].(string); ok {
			if !validStatuses[s] {
				return map[string]interface{}{"error": fmt.Sprintf("Invalid status: %s. Must be pending, in_progress, or completed.", s)},
					fmt.Errorf("invalid status: %s", s)
			}
			newTodo.Status = s
		}

		currentTodos = append(currentTodos, newTodo)
		if err := writeTodos(sf, currentTodos); err != nil {
			return map[string]interface{}{"error": fmt.Sprintf("Failed to add TODO: %v", err)},
				err
		}
		return map[string]interface{}{"status": "success", "message": fmt.Sprintf("TODO added with ID: %s", newID), "todo": newTodo}, nil

	case "update":
		if err := EnsureKnownKeys("write_todo(update)", args, "action", "id", "content", "priority", "status"); err != nil {
			return nil, err
		}
		id, ok := args["id"].(string)
		if !ok || id == "" {
			return map[string]interface{}{"error": "ID is required for 'update' action."},
				fmt.Errorf("missing or invalid 'id' argument for update action")
		}

		found := false
		for i := range currentTodos {
			if currentTodos[i].ID == id {
				if c, ok := args["content"].(string); ok {
					currentTodos[i].Content = c
				}
				if s, ok := args["status"].(string); ok {
					if !validStatuses[s] {
						return map[string]interface{}{"error": fmt.Sprintf("Invalid status: %s. Must be pending, in_progress, or completed.", s)},
							fmt.Errorf("invalid status: %s", s)
					}
					currentTodos[i].Status = s
				}
				if p, ok := args["priority"].(string); ok {
					if !validPriorities[p] {
						return map[string]interface{}{"error": fmt.Sprintf("Invalid priority: %s. Must be low, medium, or high.", p)},
							fmt.Errorf("invalid priority: %s", p)
					}
					currentTodos[i].Priority = p
				}
				found = true
				break
			}
		}
		if !found {
			return map[string]interface{}{"error": fmt.Sprintf("TODO with ID %s not found.", id)},
				fmt.Errorf("TODO with ID %s not found", id)
		}

		if err := writeTodos(sf, currentTodos); err != nil {
			return map[string]interface{}{"error": fmt.Sprintf("Failed to update TODO: %v", err)},
				err
		}
		return map[string]interface{}{"status": "success", "message": fmt.Sprintf("TODO with ID %s updated.", id)}, nil

	case "delete":
		if err := EnsureKnownKeys("write_todo(delete)", args, "action", "id"); err != nil {
			return nil, err
		}
		id, ok := args["id"].(string)
		if !ok || id == "" {
			return map[string]interface{}{"error": "ID is required for 'delete' action."},
				fmt.Errorf("missing or invalid 'id' argument for delete action")
		}

		newTodos := []TodoItem{}
		found := false
		for _, item := range currentTodos {
			if item.ID == id {
				found = true
				continue
			}
			newTodos = append(newTodos, item)
		}
		if !found {
			return map[string]interface{}{"error": fmt.Sprintf("TODO with ID %s not found.", id)},
				fmt.Errorf("TODO with ID %s not found", id)
		}

		if err := writeTodos(sf, newTodos); err != nil {
			return map[string]interface{}{"error": fmt.Sprintf("Failed to delete TODO: %v", err)},
				err
		}
		return map[string]interface{}{"status": "success", "message": fmt.Sprintf("TODO with ID %s deleted.", id)}, nil

	default:
		return map[string]interface{}{"error": fmt.Sprintf("Invalid action: %s", action)},
			fmt.Errorf("invalid action: %s", action)
	}
}

var writeTodoToolDefinition = ToolDefinition{
	Name:        "write_todo",
	Description: "Manages the TODO list (available as `" + todoFilePath + "` at the anonymous working directory). Can add new TODOs, update status, priority, content, delete TODOs, or list all TODOs.",
	Parameters: &Schema{
		Type: TypeObject,
		Properties: map[string]*Schema{
			"action": {
				Type:        TypeString,
				Description: "Action to perform: \"add\", \"update\", \"delete\", or \"list\".",
				Enum:        []string{"add", "update", "delete", "list"},
			},
			"id": {
				Type:        TypeString,
				Description: "ID of the TODO item. Required for 'update' and 'delete' actions.",
			},
			"content": {
				Type:        TypeString,
				Description: "Content of the TODO item. Required for 'add' action.",
			},
			"status": {
				Type:        TypeString,
				Description: "Status of the TODO item: 'pending', 'in_progress', or 'completed'.",
				Enum:        []string{"pending", "in_progress", "completed"},
			},
			"priority": {
				Type:        TypeString,
				Description: "Priority of the TODO item: 'low', 'medium', or 'high'.",
				Enum:        []string{"low", "medium", "high"},
			},
		},
		Required: []string{"action"},
	},
	Handler: WriteTodoTool,
}
