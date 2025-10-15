package main

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// ChatSearchResult represents a single chat search result
type ChatSearchResult struct {
	Excerpt     string `json:"excerpt"`
	SessionName string `json:"session_name"`
	Who         string `json:"who"` // "user" or "model"
	Date        string `json:"date"`
}

var searchChatToolDefinition = ToolDefinition{
	Name:        "search_chat",
	Description: "Search through chat history using keywords. Returns recent matching messages with context excerpts.",
	Parameters: &Schema{
		Type:        TypeObject,
		Description: "Search for messages containing specific keywords",
		Properties: map[string]*Schema{
			"keywords": {
				Type:        TypeString,
				Description: "Keywords to search for in chat messages",
			},
		},
		Required: []string{"keywords"},
	},
	Handler: func(ctx context.Context, args map[string]interface{}, params ToolHandlerParams) (ToolHandlerResults, error) {
		// Validate arguments
		if err := EnsureKnownKeys("search_chat", args, "keywords"); err != nil {
			return ToolHandlerResults{}, err
		}

		keywords, ok := args["keywords"].(string)
		if !ok {
			return ToolHandlerResults{}, fmt.Errorf("keywords must be a string")
		}

		if strings.TrimSpace(keywords) == "" {
			return ToolHandlerResults{}, fmt.Errorf("keywords cannot be empty")
		}

		// Get database connection from global context (assuming it's available)
		db, err := getDbFromContext(ctx)
		if err != nil {
			return ToolHandlerResults{}, fmt.Errorf("failed to get database connection: %w", err)
		}

		// Get current session info
		currentSessionID := params.SessionId
		currentWorkspaceID := ""
		if currentSessionID != "" {
			var session Session
			session, err = GetSession(db, currentSessionID)
			if err != nil {
				return ToolHandlerResults{}, fmt.Errorf("failed to get current session: %w", err)
			}
			currentWorkspaceID = session.WorkspaceID
		}

		// Perform search
		results, err := searchChatHistory(db, keywords, currentWorkspaceID, currentSessionID)
		if err != nil {
			return ToolHandlerResults{}, fmt.Errorf("failed to search chat history: %w", err)
		}

		// Convert results to JSON-serializable format
		var resultsArray []map[string]interface{}
		for _, result := range results {
			resultsArray = append(resultsArray, map[string]interface{}{
				"excerpt":      result.Excerpt,
				"session_name": result.SessionName,
				"who":          result.Who,
				"date":         result.Date,
			})
		}

		return ToolHandlerResults{
			Value: map[string]interface{}{
				"results": resultsArray,
				"count":   len(resultsArray),
			},
		}, nil
	},
}

// searchChatHistory searches through chat history using the common SearchMessages function
func searchChatHistory(db *sql.DB, keywords, workspaceID, currentSessionID string) ([]ChatSearchResult, error) {
	// Use the common SearchMessages function
	searchResults, _, err := SearchMessages(db, keywords, 0, 20, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("failed to search messages: %w", err)
	}

	var results []ChatSearchResult
	for _, result := range searchResults {
		// Skip current session and its sub-sessions
		if currentSessionID != "" {
			if result.SessionID == currentSessionID || strings.HasPrefix(result.SessionID, currentSessionID+".") {
				continue
			}
		}

		// Determine session name and who field
		resultSessionName, who := determineSessionInfo(result.SessionID, result.SessionName, result.Type)

		// Format date
		formattedDate := formatCreatedAt(result.CreatedAt)

		results = append(results, ChatSearchResult{
			Excerpt:     result.Excerpt,
			SessionName: resultSessionName,
			Who:         who,
			Date:        formattedDate,
		})
	}

	return results, nil
}

// determineSessionInfo determines the session name and who field based on session ID and message type
func determineSessionInfo(sessionID, sessionName, msgType string) (string, string) {
	// Check if this is a sub-session (contains dots)
	if strings.Contains(sessionID, ".") {
		// For sub-sessions, who is always "model" and session name is main session + " (Subagent)"
		mainSessionID := strings.Split(sessionID, ".")[0]
		mainSessionName := mainSessionID
		if sessionName != "" && !strings.Contains(sessionID, sessionName) {
			// Use the actual session name if available and not already containing sub-session info
			mainSessionName = sessionName
		}
		return mainSessionName + " (Subagent)", "model"
	}

	// For regular sessions
	who := "user"
	if msgType == "model" {
		who = "model"
	}

	// Use session name if available, otherwise use session ID
	if sessionName == "" {
		sessionName = sessionID
	}

	return sessionName, who
}

// formatCreatedAt formats the creation time in a readable format
func formatCreatedAt(createdAt string) string {
	// Parse the timestamp and format it
	t, err := time.Parse("2006-01-02 15:04:05", createdAt)
	if err != nil {
		// Try alternative format
		t, err = time.Parse(time.RFC3339, createdAt)
		if err != nil {
			// If parsing fails, return original string
			return createdAt
		}
	}

	return t.Format("2006-01-02 15:04:05")
}
