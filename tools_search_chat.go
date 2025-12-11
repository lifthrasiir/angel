package main

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	. "github.com/lifthrasiir/angel/gemini"
	"github.com/lifthrasiir/angel/internal/database"
	"github.com/lifthrasiir/angel/internal/tool"
	. "github.com/lifthrasiir/angel/internal/types"
)

// ChatSearchResult represents a single chat search result
type ChatSearchResult struct {
	Excerpt     string `json:"excerpt"`
	SessionName string `json:"session_name"`
	Who         string `json:"who"` // "user" or "model"
	Date        string `json:"date"`
}

// SearchChatTool handles searching through chat history
func SearchChatTool(ctx context.Context, args map[string]interface{}, params tool.HandlerParams) (tool.HandlerResults, error) {
	// Validate arguments
	if err := tool.EnsureKnownKeys("search_chat", args, "keywords"); err != nil {
		return tool.HandlerResults{}, err
	}

	keywords, ok := args["keywords"].(string)
	if !ok {
		return tool.HandlerResults{}, fmt.Errorf("keywords must be a string")
	}

	if strings.TrimSpace(keywords) == "" {
		return tool.HandlerResults{}, fmt.Errorf("keywords cannot be empty")
	}

	// Get database connection from global context (assuming it's available)
	db, err := database.FromContext(ctx)
	if err != nil {
		return tool.HandlerResults{}, err
	}

	// Get current session info
	currentSessionID := params.SessionId
	currentWorkspaceID := ""
	if currentSessionID != "" {
		var session Session
		session, err = database.GetSession(db, currentSessionID)
		if err != nil {
			return tool.HandlerResults{}, fmt.Errorf("failed to get current session: %w", err)
		}
		currentWorkspaceID = session.WorkspaceID
	}

	// Perform search
	results, err := searchChatHistory(db, keywords, currentWorkspaceID, currentSessionID)
	if err != nil {
		return tool.HandlerResults{}, fmt.Errorf("failed to search chat history: %w", err)
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

	return tool.HandlerResults{
		Value: map[string]interface{}{
			"results": resultsArray,
			"count":   len(resultsArray),
		},
	}, nil
}

// searchChatHistory searches through chat history using the common SearchMessages function
func searchChatHistory(db *sql.DB, keywords, workspaceID, currentSessionID string) ([]ChatSearchResult, error) {
	// Use the common SearchMessages function
	searchResults, _, err := database.SearchMessages(db, keywords, 0, 20, workspaceID)
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

// RecallTool handles recalling unprocessed binary content
func RecallTool(ctx context.Context, args map[string]interface{}, params tool.HandlerParams) (tool.HandlerResults, error) {
	// Validate arguments
	if err := tool.EnsureKnownKeys("recall", args, "query"); err != nil {
		return tool.HandlerResults{}, err
	}

	query, ok := args["query"].(string)
	if !ok {
		return tool.HandlerResults{}, fmt.Errorf("query must be a string")
	}

	if strings.TrimSpace(query) == "" {
		return tool.HandlerResults{}, fmt.Errorf("query cannot be empty")
	}

	// Get database connection from context
	db, err := database.FromContext(ctx)
	if err != nil {
		return tool.HandlerResults{}, err
	}

	// For now, treat query as a single hash
	// In the future, this could be expanded to handle multiple hashes or search functionality
	hash := strings.TrimSpace(query)

	// Try to retrieve the blob as a file attachment
	attachment, err := database.GetBlobAsFileAttachment(db, hash)
	if err != nil {
		return tool.HandlerResults{}, fmt.Errorf("failed to retrieve content for hash %s: %w", hash, err)
	}

	// Return the hash as response text and the content as attachment
	// This matches the generate_image tool response format
	result := map[string]interface{}{
		"response": fmt.Sprintf("Recalled content for hash %s follows:", hash),
	}

	return tool.HandlerResults{
		Value:       result,
		Attachments: []FileAttachment{attachment},
	}, nil
}

// registerSearchChatTools registers search and recall tools
func registerSearchChatTools(tools *tool.Tools) {
	tools.Register(tool.Definition{
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
		Handler: SearchChatTool,
	})

	tools.Register(tool.Definition{
		Name: "recall",
		Description: `**Retrieves content for binary hashes that are NOT directly rendered or explicitly described in the chat.**
When binary content (e.g., images, audio, PDFs) is provided directly in the chat with a hash reference (e.g., '[Binary with hash ... follows:]' immediately followed by the rendered content), you can directly perceive and understand its details. In such cases, 'recall' is generally NOT required for basic comprehension.
However, 'recall' is **ESSENTIAL** when you encounter messages explicitly stating that a binary with a given hash is **UNPROCESSED** (e.g., 'Binary with hash xyz is currently **UNPROCESSED**') and its content has not been rendered or described for you. In these situations, you **MUST** use 'recall' to access the content's details for internal analysis and understanding.
This tool recovers previously un-perceived or raw data from SHA-512/256 hashes, enabling you to accurately comprehend content details, formulate precise responses, or perform further processing.`,
		Parameters: &Schema{
			Type:        TypeObject,
			Description: "Recall unprocessed binary content for internal AI processing",
			Properties: map[string]*Schema{
				"query": {
					Type:        TypeString,
					Description: "The SHA-512/256 hash of the unprocessed binary content required for your internal comprehension and subsequent actions.",
				},
			},
			Required: []string{"query"},
		},
		Handler: RecallTool,
	})
}
