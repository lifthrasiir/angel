package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
)

func handleSessionPage(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)

	vars := mux.Vars(r)
	sessionId := vars["sessionId"]

	if sessionId == "" {
		http.NotFound(w, r)
		return
	}

	exists, err := SessionExists(db, sessionId)
	if err != nil {
		log.Printf("handleSessionPage: Failed to check session existence for %s: %v", sessionId, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if !exists {
		http.NotFound(w, r)
		return
	}

	serveSPAIndex(w, r)
}

func updateSessionNameHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)

	vars := mux.Vars(r)
	sessionId := vars["sessionId"]
	if sessionId == "" {
		http.Error(w, "Session ID is required", http.StatusBadRequest)
		return
	}

	var requestBody struct {
		Name string `json:"name"`
	}

	if !decodeJSONRequest(r, w, &requestBody, "updateSessionNameHandler") {
		return
	}

	if err := UpdateSessionName(db, sessionId, requestBody.Name); err != nil {
		log.Printf("Failed to update session name for %s: %v", sessionId, err)
		http.Error(w, fmt.Sprintf("Failed to update session name: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "Session name updated successfully")
}

func getUserInfoHandler(w http.ResponseWriter, r *http.Request) {
	auth := getAuth(w, r)

	// Use Validate to ensure token is valid and refreshed if necessary
	if !auth.Validate("getUserInfoHandler", w, r) {
		// Validate already sent an error response
		return
	}

	// If UserEmail is empty but token is valid, try to re-fetch user info
	userEmail, err := auth.GetUserEmail(r)
	if err != nil {
		log.Printf("getUserInfoHandler: Failed to get user email: %v", err)
		// Non-fatal, continue without email
	}

	sendJSONResponse(w, map[string]string{"email": userEmail})
}

func createWorkspaceHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)

	var requestBody struct {
		Name string `json:"name"`
	}

	if !decodeJSONRequest(r, w, &requestBody, "createWorkspaceHandler") {
		return
	}

	workspaceID := generateID() // Reusing session ID generation for workspace ID

	if err := CreateWorkspace(db, workspaceID, requestBody.Name, ""); err != nil {
		log.Printf("Failed to create workspace %s: %v", requestBody.Name, err)
		http.Error(w, fmt.Sprintf("Failed to create workspace: %v", err), http.StatusInternalServerError)
		return
	}

	sendJSONResponse(w, map[string]string{"id": workspaceID, "name": requestBody.Name})
}

func listWorkspacesHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)

	workspaces, err := GetAllWorkspaces(db)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to retrieve workspaces: %v", err), http.StatusInternalServerError)
		return
	}

	sendJSONResponse(w, workspaces)
}

func deleteWorkspaceHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)

	workspaceID := mux.Vars(r)["id"]
	if workspaceID == "" {
		http.Error(w, "Workspace ID is required", http.StatusBadRequest)
		return
	}

	if err := DeleteWorkspace(db, workspaceID); err != nil {
		log.Printf("Failed to delete workspace %s: %v", workspaceID, err)
		http.Error(w, fmt.Sprintf("Failed to delete workspace: %v", err), http.StatusInternalServerError)
		return
	}

	sendJSONResponse(w, map[string]string{"status": "success", "message": "Workspace deleted successfully"})
}

func countTokensHandler(w http.ResponseWriter, r *http.Request) {
	auth := getAuth(w, r)

	if !auth.Validate("countTokensHandler", w, r) {
		return
	}

	var requestBody struct {
		Text  string `json:"text"`
		Model string `json:"model"` // Add model field to request body
	}

	if !decodeJSONRequest(r, w, &requestBody, "countTokensHandler") {
		return
	}

	modelName := requestBody.Model
	if modelName == "" {
		modelName = DefaultGeminiModel // Default model if not provided
	}

	provider, ok := CurrentProviders[modelName]
	if !ok {
		http.Error(w, fmt.Sprintf("Unsupported model: %s", modelName), http.StatusBadRequest)
		return
	}

	contents := []Content{
		{
			Role:  "user",
			Parts: []Part{{Text: requestBody.Text}},
		},
	}

	resp, err := provider.CountTokens(context.Background(), contents, modelName)
	if err != nil {
		log.Printf("CountTokens API call failed: %v", err)
		if apiErr, ok := err.(*APIError); ok {
			http.Error(w, fmt.Sprintf("CountTokens API call failed: %v", apiErr.Message), apiErr.StatusCode)
		} else {
			http.Error(w, fmt.Sprintf("CountTokens API call failed: %v", err), http.StatusInternalServerError)
		}
		return
	}

	sendJSONResponse(w, map[string]int{"totalTokens": resp.TotalTokens})
}

// handleCall handles GET and DELETE requests for /api/calls/{sessionId}
func handleCall(w http.ResponseWriter, r *http.Request) {
	auth := getAuth(w, r)

	if !auth.Validate("handleCall", w, r) {
		return
	}

	vars := mux.Vars(r)
	sessionId := vars["sessionId"]
	if sessionId == "" {
		http.Error(w, "Session ID is required", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case "GET":
		isActive := hasActiveCall(sessionId)
		sendJSONResponse(w, map[string]bool{"isActive": isActive})
	case "DELETE":
		if err := cancelCall(sessionId); err != nil {
			log.Printf("handleCall: Failed to cancel call for session %s: %v", sessionId, err)
			http.Error(w, fmt.Sprintf("Failed to cancel call: %v", err), http.StatusInternalServerError)
			return
		}
		sendJSONResponse(w, map[string]string{"status": "success", "message": "Call cancelled successfully"})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleEvaluatePrompt evaluates a Go template string and returns the result
func handleEvaluatePrompt(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r) // Get DB from context

	var requestBody struct {
		Template    string `json:"template"`
		WorkspaceID string `json:"workspaceId"` // Add WorkspaceID
	}

	if !decodeJSONRequest(r, w, &requestBody, "handleEvaluatePrompt") {
		return
	}

	var workspaceName string
	if requestBody.WorkspaceID != "" {
		workspace, err := GetWorkspace(db, requestBody.WorkspaceID)
		if err != nil {
			log.Printf("handleEvaluatePrompt: Failed to get workspace %s: %v", requestBody.WorkspaceID, err)
			http.Error(w, fmt.Sprintf("Failed to get workspace: %v", err), http.StatusInternalServerError)
			return
		}
		workspaceName = workspace.Name
	}

	data := PromptData{workspaceName: workspaceName}
	evaluatedPrompt, err := data.EvaluatePrompt(requestBody.Template)
	if err != nil {
		log.Printf("Error evaluating prompt template: %v", err)
		http.Error(w, fmt.Sprintf("Error evaluating prompt template: %v", err), http.StatusBadRequest)
		return
	}

	sendJSONResponse(w, map[string]string{"evaluatedPrompt": evaluatedPrompt})
}

// FrontendMCPConfig is a struct that combines DB config with live status for the frontend.
type FrontendMCPConfig struct {
	Name           string          `json:"name"`
	ConfigJSON     json.RawMessage `json:"config_json"`
	Enabled        bool            `json:"enabled"`
	IsConnected    bool            `json:"is_connected"`
	AvailableTools []string        `json:"available_tools,omitempty"`
}

func getMCPConfigsHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)

	dbConfigs, err := GetMCPServerConfigs(db)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to retrieve MCP configs from DB: %v", err), http.StatusInternalServerError)
		return
	}

	activeConnections := mcpManager.GetMCPConnections()
	frontendConfigs := make([]FrontendMCPConfig, len(dbConfigs))

	for i, dbConfig := range dbConfigs {
		conn, isConnected := activeConnections[dbConfig.Name]

		frontendConfig := FrontendMCPConfig{
			Name:        dbConfig.Name,
			ConfigJSON:  dbConfig.ConfigJSON,
			Enabled:     dbConfig.Enabled,
			IsConnected: isConnected && conn.IsEnabled,
		}

		if frontendConfig.IsConnected {
			toolsIterator := conn.Session.Tools(context.Background(), nil)
			var tools []string
			builtinToolNames := GetBuiltinToolNames()
			for tool, err := range toolsIterator {
				if err != nil {
					log.Printf("Error iterating tools for %s: %v", dbConfig.Name, err)
					break
				}
				mappedName := tool.Name
				if _, exists := builtinToolNames[tool.Name]; exists {
					mappedName = dbConfig.Name + "__" + tool.Name
				}
				tools = append(tools, mappedName)
			}
			sort.Strings(tools) // Sort tools for consistent output
			frontendConfig.AvailableTools = tools
		}

		frontendConfigs[i] = frontendConfig
	}

	sendJSONResponse(w, frontendConfigs)
}

func saveMCPConfigHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)

	var requestBody struct {
		Name       string `json:"name"`
		ConfigJSON string `json:"config_json"`
		Enabled    bool   `json:"enabled"`
	}

	if !decodeJSONRequest(r, w, &requestBody, "saveMCPConfigHandler") {
		return
	}

	config := MCPServerConfig{
		Name:       requestBody.Name,
		ConfigJSON: json.RawMessage(requestBody.ConfigJSON),
		Enabled:    requestBody.Enabled,
	}

	if err := SaveMCPServerConfig(db, config); err != nil {
		log.Printf("Failed to save MCP config %s: %v", config.Name, err)
		http.Error(w, fmt.Sprintf("Failed to save MCP config: %v", err), http.StatusInternalServerError)
		return
	}

	// Start or stop the connection based on the new enabled state
	if config.Enabled {
		mcpManager.startConnection(config)
	} else {
		mcpManager.stopConnection(config.Name)
	}

	sendJSONResponse(w, config)
}

func deleteMCPConfigHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)

	name := mux.Vars(r)["name"]
	if name == "" {
		http.Error(w, "MCP config name is required", http.StatusBadRequest)
		return
	}

	if err := DeleteMCPServerConfig(db, name); err != nil {
		log.Printf("Failed to delete MCP config %s: %v", name, err)
		http.Error(w, fmt.Sprintf("Failed to delete MCP config: %v", err), http.StatusInternalServerError)
		return
	}

	mcpManager.stopConnection(name)

	sendJSONResponse(w, map[string]string{"status": "success", "message": "MCP config deleted successfully"})
}

// handleNotFound handles requests for paths that don't match any other routes.
func handleNotFound(w http.ResponseWriter, r *http.Request) {
	http.NotFound(w, r)
}

// ModelInfo represents the information about an LLM model.
type ModelInfo struct {
	Name      string `json:"name"`
	MaxTokens int    `json:"maxTokens"`
}

// sortableModelInfo is an internal struct used for sorting models before sending to frontend.
type sortableModelInfo struct {
	Name                 string
	MaxTokens            int
	RelativeDisplayOrder int
}

func listModelsHandler(w http.ResponseWriter, r *http.Request) {
	var sortableModels []sortableModelInfo
	for modelName, provider := range CurrentProviders {
		sortableModels = append(sortableModels, sortableModelInfo{
			Name:                 modelName,
			MaxTokens:            provider.MaxTokens(),
			RelativeDisplayOrder: provider.RelativeDisplayOrder(),
		})
	}

	// Sort models:
	// 1. By RelativeDisplayOrder in descending order
	// 2. Then by Name in ascending order
	sort.Slice(sortableModels, func(i, j int) bool {
		if sortableModels[i].RelativeDisplayOrder != sortableModels[j].RelativeDisplayOrder {
			return sortableModels[i].RelativeDisplayOrder > sortableModels[j].RelativeDisplayOrder // Descending
		}
		return sortableModels[i].Name < sortableModels[j].Name // Ascending
	})

	var models []ModelInfo
	for _, sm := range sortableModels {
		models = append(models, ModelInfo{
			Name:      sm.Name,
			MaxTokens: sm.MaxTokens,
		})
	}

	sendJSONResponse(w, models)
}

// handleDownloadBlob retrieves a blob by its message ID and attachment index and serves it as a download.
func handleDownloadBlob(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)

	vars := mux.Vars(r)
	sessionId := vars["sessionId"]
	messageIdStr := vars["messageId"]
	blobIndexStr := vars["blobIndex"]

	if sessionId == "" || messageIdStr == "" || blobIndexStr == "" {
		http.Error(w, "Session ID, Message ID, and Blob Index are required", http.StatusBadRequest)
		return
	}

	messageId, err := strconv.Atoi(messageIdStr)
	if err != nil {
		http.Error(w, "Invalid Message ID", http.StatusBadRequest)
		return
	}

	blobIndex, err := strconv.Atoi(blobIndexStr)
	if err != nil {
		http.Error(w, "Invalid Blob Index", http.StatusBadRequest)
		return
	}

	// Get the message from the database
	message, err := GetMessageByID(db, messageId) // GetMessageByID returns Message struct
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			http.Error(w, "Message not found", http.StatusNotFound)
		} else {
			log.Printf("Failed to retrieve message %d: %v", messageId, err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
		return
	}

	if message == nil || message.SessionID != sessionId {
		http.Error(w, "Message not found in this session", http.StatusNotFound)
		return
	}

	// message.Attachments is already []FileAttachment type
	attachments := message.Attachments
	if attachments == nil { // Handle case where there are no attachments
		http.Error(w, "No attachments found for this message", http.StatusNotFound)
		return
	}

	if blobIndex < 0 || blobIndex >= len(attachments) {
		http.Error(w, "Blob index out of range", http.StatusBadRequest)
		return
	}

	// Use the Hash field for the blob hash
	blobHash := attachments[blobIndex].Hash
	fileName := attachments[blobIndex].FileName
	mimeType := attachments[blobIndex].MimeType

	data, err := GetBlob(db, blobHash) // GetBlob works with hash
	if err != nil {
		if strings.Contains(err.Error(), "blob not found") {
			http.Error(w, "Blob data not found", http.StatusNotFound)
		} else {
			log.Printf("Failed to retrieve blob data for hash %s: %v", blobHash, err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
		return
	}

	// Set appropriate headers for file download
	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", fileName))

	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))

	_, err = w.Write(data)
	if err != nil {
		log.Printf("Failed to write blob data for hash %s: %v", blobHash, err)
		// Error writing to client, but response headers might already be sent
	}
}

// getSystemPromptsHandler handles GET requests for /api/systemPrompts
func getSystemPromptsHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)

	prompts, err := GetGlobalPrompts(db)
	if err != nil {
		log.Printf("Failed to retrieve global prompts: %v", err)
		http.Error(w, fmt.Sprintf("Failed to retrieve global prompts: %v", err), http.StatusInternalServerError)
		return
	}

	// If no prompts are found, initialize with default values and save them
	if len(prompts) == 0 {
		prompts = []PredefinedPrompt{
			{Label: "Default prompt", Value: "{{.Builtin.SystemPrompt}}"},
			{Label: "Empty prompt", Value: ""},
		}
		// Save these defaults to the DB immediately
		if err := SaveGlobalPrompts(db, prompts); err != nil {
			log.Printf("Failed to save default global prompts: %v", err)
			// Continue even if saving fails, as prompts are in memory
		}
	}

	sendJSONResponse(w, prompts)
}

// saveSystemPromptsHandler handles PUT requests for /api/systemPrompts
func saveSystemPromptsHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)

	var prompts []PredefinedPrompt
	if !decodeJSONRequest(r, w, &prompts, "saveSystemPromptsHandler") {
		return
	}

	if err := SaveGlobalPrompts(db, prompts); err != nil {
		log.Printf("Failed to save global prompts: %v", err)
		http.Error(w, fmt.Sprintf("Failed to save global prompts: %v", err), http.StatusInternalServerError)
		return
	}

	sendJSONResponse(w, map[string]string{"status": "success", "message": "Global prompts updated successfully"})
}
