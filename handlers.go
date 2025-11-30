package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"

	"github.com/fvbommel/sortorder"
	"github.com/gorilla/mux"

	. "github.com/lifthrasiir/angel/gemini"
)

func handleSessionPage(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)

	vars := mux.Vars(r)
	sessionId := vars["sessionId"]

	if sessionId == "" {
		sendNotFoundError(w, r, "Session not found")
		return
	}

	exists, err := SessionExists(db, sessionId)
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to check session existence")
		return
	}

	if !exists {
		sendNotFoundError(w, r, "Session not found")
		return
	}

	serveSPAIndex(w, r)
}

func updateSessionNameHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)

	vars := mux.Vars(r)
	sessionId := vars["sessionId"]
	if sessionId == "" {
		sendBadRequestError(w, r, "Session ID is required")
		return
	}

	var requestBody struct {
		Name string `json:"name"`
	}

	if !decodeJSONRequest(r, w, &requestBody, "updateSessionNameHandler") {
		return
	}

	if err := UpdateSessionName(db, sessionId, requestBody.Name); err != nil {
		sendInternalServerError(w, r, err, "Failed to update session name")
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "Session name updated successfully")
}

func updateSessionWorkspaceHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)

	vars := mux.Vars(r)
	sessionId := vars["sessionId"]
	if sessionId == "" {
		sendBadRequestError(w, r, "Session ID is required")
		return
	}

	var requestBody struct {
		WorkspaceID string `json:"workspaceId"`
	}

	if !decodeJSONRequest(r, w, &requestBody, "updateSessionWorkspaceHandler") {
		return
	}

	// Verify that the workspace exists (allow empty string for anonymous workspace)
	if requestBody.WorkspaceID != "" {
		_, err := GetWorkspace(db, requestBody.WorkspaceID)
		if err != nil {
			sendNotFoundError(w, r, "Workspace not found")
			return
		}
	}

	// Verify that the session exists
	exists, err := SessionExists(db, sessionId)
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to check session existence")
		return
	}
	if !exists {
		sendNotFoundError(w, r, "Session not found")
		return
	}

	if err := UpdateSessionWorkspace(db, sessionId, requestBody.WorkspaceID); err != nil {
		sendInternalServerError(w, r, err, "Failed to update session workspace")
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "Session workspace updated successfully")
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
		sendInternalServerError(w, r, err, "Failed to create workspace")
		return
	}

	sendJSONResponse(w, map[string]string{"id": workspaceID, "name": requestBody.Name})
}

func listWorkspacesHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)

	workspaces, err := GetAllWorkspaces(db)
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to retrieve workspaces")
		return
	}

	sendJSONResponse(w, workspaces)
}

func deleteWorkspaceHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)

	workspaceID := mux.Vars(r)["id"]
	if workspaceID == "" {
		sendBadRequestError(w, r, "Workspace ID is required")
		return
	}

	if err := DeleteWorkspace(db, workspaceID); err != nil {
		sendInternalServerError(w, r, err, "Failed to delete workspace")
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

	provider := GlobalModelsRegistry.GetProvider(modelName)
	if provider == nil {
		sendBadRequestError(w, r, fmt.Sprintf("Unsupported model: %s", modelName))
		return
	}

	contents := []Content{
		{
			Role:  RoleUser,
			Parts: []Part{{Text: requestBody.Text}},
		},
	}

	resp, err := provider.CountTokens(context.Background(), modelName, contents)
	if err != nil {
		if apiErr, ok := err.(*APIError); ok {
			http.Error(w, fmt.Sprintf("CountTokens API call failed: %v", apiErr.Message), apiErr.StatusCode)
		} else {
			sendInternalServerError(w, r, err, "CountTokens API call failed")
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
		sendBadRequestError(w, r, "Session ID is required")
		return
	}

	switch r.Method {
	case "GET":
		isActive := hasActiveCall(sessionId)
		sendJSONResponse(w, map[string]bool{"isActive": isActive})
	case "DELETE":
		if err := cancelCall(sessionId); err != nil {
			sendInternalServerError(w, r, err, fmt.Sprintf("Failed to cancel call for session %s", sessionId))
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
			sendInternalServerError(w, r, err, fmt.Sprintf("Failed to get workspace %s", requestBody.WorkspaceID))
			return
		}
		workspaceName = workspace.Name
	}

	data := PromptData{workspaceName: workspaceName}
	evaluatedPrompt, err := data.EvaluatePrompt(requestBody.Template)
	if err != nil {
		sendBadRequestError(w, r, fmt.Sprintf("Error evaluating prompt template: %v", err))
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
		sendInternalServerError(w, r, err, "Failed to retrieve MCP configs from DB")
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
		sendInternalServerError(w, r, err, fmt.Sprintf("Failed to save MCP config %s", config.Name))
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
		sendBadRequestError(w, r, "MCP config name is required")
		return
	}

	if err := DeleteMCPServerConfig(db, name); err != nil {
		sendInternalServerError(w, r, err, fmt.Sprintf("Failed to delete MCP config %s", name))
		return
	}

	mcpManager.stopConnection(name)

	sendJSONResponse(w, map[string]string{"status": "success", "message": "MCP config deleted successfully"})
}

// sendInternalServerError logs the error and sends a 500 Internal Server Error response.
func sendInternalServerError(w http.ResponseWriter, _ *http.Request, err error, msg string) {
	log.Printf("%s: %v", msg, err)
	http.Error(w, fmt.Sprintf("Internal Server Error: %s", msg), http.StatusInternalServerError)
}

// sendBadRequestError sends a 400 Bad Request response.
func sendBadRequestError(w http.ResponseWriter, _ *http.Request, msg string) {
	http.Error(w, msg, http.StatusBadRequest)
}

// sendNotFoundError sends a 4.04 Not Found response.
func sendNotFoundError(w http.ResponseWriter, _ *http.Request, msg string) {
	http.Error(w, msg, http.StatusNotFound)
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
	if GlobalModelsRegistry == nil {
		sendJSONResponse(w, []ModelInfo{})
		return
	}

	var sortableModels []sortableModelInfo
	for modelName, provider := range GlobalModelsRegistry.providers {
		sortableModels = append(sortableModels, sortableModelInfo{
			Name:                 modelName,
			MaxTokens:            provider.MaxTokens(modelName),
			RelativeDisplayOrder: provider.RelativeDisplayOrder(modelName),
		})
	}

	// Sort models:
	// 1. By RelativeDisplayOrder in descending order
	// 2. Then by Name in natural ascending order
	sort.Slice(sortableModels, func(i, j int) bool {
		if sortableModels[i].RelativeDisplayOrder != sortableModels[j].RelativeDisplayOrder {
			return sortableModels[i].RelativeDisplayOrder > sortableModels[j].RelativeDisplayOrder
		}
		return sortorder.NaturalLess(sortableModels[i].Name, sortableModels[j].Name)
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

func updateSessionRootsHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)
	auth := getAuth(w, r)

	if !auth.Validate("updateSessionRootsHandler", w, r) {
		return
	}

	vars := mux.Vars(r)
	sessionId := vars["sessionId"]
	if sessionId == "" {
		sendBadRequestError(w, r, "Session ID is required")
		return
	}

	var requestBody struct {
		Roots []string `json:"roots"`
	}

	if !decodeJSONRequest(r, w, &requestBody, "updateSessionRootsHandler") {
		return
	}

	// Get the SessionFS instance for this session
	sessionFS, err := getSessionFS(r.Context(), sessionId)
	if err != nil {
		sendInternalServerError(w, r, err, fmt.Sprintf("Failed to get SessionFS for session %s", sessionId))
		return
	}
	defer releaseSessionFS(sessionId)

	// Get current roots before update for EnvChanged calculation
	oldRoots := sessionFS.Roots()

	// Update SessionFS with the new roots
	if err := sessionFS.SetRoots(requestBody.Roots); err != nil {
		sendInternalServerError(w, r, err, fmt.Sprintf("Failed to set roots for session %s", sessionId))
		return
	}

	// Update the database
	_, err = AddSessionEnv(db, sessionId, requestBody.Roots)
	if err != nil {
		sendInternalServerError(w, r, err, fmt.Sprintf("Failed to update session roots in DB for session %s", sessionId))
		return
	}

	// Calculate EnvChanged
	rootsChanged, err := calculateRootsChanged(oldRoots, requestBody.Roots)
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to calculate environment changes")
		return
	}

	// Send EnvChanged as JSON response
	sendJSONResponse(w, rootsChanged)
}

// handleDownloadBlobByHash retrieves a blob by its hash and serves it directly.
func handleDownloadBlobByHash(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)

	vars := mux.Vars(r)
	blobHash := vars["blobHash"]

	if blobHash == "" {
		sendBadRequestError(w, r, "Blob hash is required")
		return
	}

	// Extract extension from hash if present (format: hash.extension)
	var extension string
	if dotIndex := strings.LastIndex(blobHash, "."); dotIndex != -1 {
		extension = blobHash[dotIndex+1:]
		blobHash = blobHash[:dotIndex]
	}

	data, err := GetBlob(db, blobHash) // GetBlob works with hash
	if err != nil {
		if strings.Contains(err.Error(), "blob not found") {
			sendNotFoundError(w, r, "Blob data not found")
		} else {
			sendInternalServerError(w, r, err, fmt.Sprintf("Failed to retrieve blob data for hash %s", blobHash))
		}
		return
	}

	// Determine MIME type based on extension or content detection
	mimeType := ""
	if extension != "" {
		// Common extension to MIME type mapping
		switch strings.ToLower(extension) {
		case "jpg", "jpeg":
			mimeType = "image/jpeg"
		case "png":
			mimeType = "image/png"
		case "gif":
			mimeType = "image/gif"
		case "webp":
			mimeType = "image/webp"
		case "pdf":
			mimeType = "application/pdf"
		case "txt":
			mimeType = "text/plain"
		case "json":
			mimeType = "application/json"
		case "xml":
			mimeType = "application/xml"
		case "csv":
			mimeType = "text/csv"
		default:
			mimeType = "application/octet-stream"
		}
	} else {
		// If no extension, try to detect from content
		mimeType = http.DetectContentType(data)
	}

	// Set appropriate headers
	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))

	// For images, set cache control headers
	if strings.HasPrefix(mimeType, "image/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000") // 1 year cache
	}

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
		sendInternalServerError(w, r, err, "Failed to retrieve global prompts")
		return
	}

	// If no prompts are found, initialize with default values and save them
	if len(prompts) == 0 {
		prompts = []PredefinedPrompt{
			{Label: "Default prompt", Value: "{{.Builtin.SystemPrompt}}"},
			{Label: "Default prompt for coding agents", Value: "{{.Builtin.SystemPromptForCoding}}"},
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
		sendInternalServerError(w, r, err, "Failed to save global prompts")
		return
	}

	sendJSONResponse(w, map[string]string{"status": "success", "message": "Global prompts updated successfully"})
}

// SearchRequest represents the search request payload
type SearchRequest struct {
	Query       string `json:"query"`
	MaxID       int    `json:"max_id,omitempty"`
	Limit       int    `json:"limit,omitempty"`
	WorkspaceID string `json:"workspace_id,omitempty"`
}

// SearchResponse represents the search response
type SearchResponse struct {
	Results []SearchResult `json:"results"`
	HasMore bool           `json:"has_more"`
}

// searchMessagesHandler handles POST requests to /api/search
func searchMessagesHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)

	var req SearchRequest
	if !decodeJSONRequest(r, w, &req, "searchMessagesHandler") {
		return
	}

	results, hasMore, err := SearchMessages(db, req.Query, req.MaxID, req.Limit, req.WorkspaceID)
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to search messages")
		return
	}

	response := SearchResponse{
		Results: results,
		HasMore: hasMore,
	}

	sendJSONResponse(w, response)
}

// getOpenAIConfigsHandler handles GET requests for /api/openai-configs
func getOpenAIConfigsHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)

	configs, err := GetOpenAIConfigs(db)
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to retrieve OpenAI configs")
		return
	}

	sendJSONResponse(w, configs)
}

// saveOpenAIConfigHandler handles POST requests for /api/openai-configs
func saveOpenAIConfigHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)

	var config OpenAIConfig
	if !decodeJSONRequest(r, w, &config, "saveOpenAIConfigHandler") {
		return
	}

	// Generate ID if not provided
	if config.ID == "" {
		config.ID = generateID()
	}

	if err := SaveOpenAIConfig(db, config); err != nil {
		sendInternalServerError(w, r, err, fmt.Sprintf("Failed to save OpenAI config %s", config.Name))
		return
	}

	// Reload OpenAI providers to reflect the changes
	ReloadOpenAIProviders(db)

	sendJSONResponse(w, config)
}

// deleteOpenAIConfigHandler handles DELETE requests for /api/openai-configs/{id}
func deleteOpenAIConfigHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)

	id := mux.Vars(r)["id"]
	if id == "" {
		sendBadRequestError(w, r, "OpenAI config ID is required")
		return
	}

	if err := DeleteOpenAIConfig(db, id); err != nil {
		sendInternalServerError(w, r, err, fmt.Sprintf("Failed to delete OpenAI config %s", id))
		return
	}

	// Reload OpenAI providers to reflect the deletion
	ReloadOpenAIProviders(db)

	sendJSONResponse(w, map[string]string{"status": "success", "message": "OpenAI config deleted successfully"})
}

// getOpenAIModelsHandler handles GET requests for /api/openai-configs/{id}/models
func getOpenAIModelsHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)

	id := mux.Vars(r)["id"]
	if id == "" {
		sendBadRequestError(w, r, "OpenAI config ID is required")
		return
	}

	// Get config
	config, err := GetOpenAIConfig(db, id)
	if err != nil {
		sendNotFoundError(w, r, fmt.Sprintf("OpenAI config with ID %s not found", id))
		return
	}

	// Create client and get models
	client := NewOpenAIClient(config)
	models, err := client.GetModels(r.Context())
	if err != nil {
		sendInternalServerError(w, r, err, fmt.Sprintf("Failed to fetch models for OpenAI config %s", id))
		return
	}

	sendJSONResponse(w, models)
}

// refreshOpenAIModelsHandler handles POST requests for /api/openai-configs/{id}/models/refresh
func refreshOpenAIModelsHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)

	id := mux.Vars(r)["id"]
	if id == "" {
		sendBadRequestError(w, r, "OpenAI config ID is required")
		return
	}

	// Get config
	config, err := GetOpenAIConfig(db, id)
	if err != nil {
		sendNotFoundError(w, r, fmt.Sprintf("OpenAI config with ID %s not found", id))
		return
	}

	// Create client and refresh models
	client := NewOpenAIClient(config)
	models, err := client.RefreshModels(r.Context())
	if err != nil {
		sendInternalServerError(w, r, err, fmt.Sprintf("Failed to refresh models for OpenAI config %s", id))
		return
	}

	sendJSONResponse(w, models)
}

// getGeminiAPIConfigsHandler handles GET requests for /api/gemini-api-configs
func getGeminiAPIConfigsHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)

	configs, err := GetGeminiAPIConfigs(db)
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to retrieve Gemini API configs")
		return
	}

	sendJSONResponse(w, configs)
}

// saveGeminiAPIConfigHandler handles POST requests for /api/gemini-api-configs
func saveGeminiAPIConfigHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)

	var config GeminiAPIConfig
	if !decodeJSONRequest(r, w, &config, "saveGeminiAPIConfigHandler") {
		return
	}

	// Generate ID if not provided
	if config.ID == "" {
		config.ID = generateID()
	}

	if err := SaveGeminiAPIConfig(db, config); err != nil {
		sendInternalServerError(w, r, err, "Failed to save Gemini API config")
		return
	}

	sendJSONResponse(w, config)
}

// deleteGeminiAPIConfigHandler handles DELETE requests for /api/gemini-api-configs/{id}
func deleteGeminiAPIConfigHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)

	id := mux.Vars(r)["id"]
	if id == "" {
		sendBadRequestError(w, r, "Gemini API config ID is required")
		return
	}

	if err := DeleteGeminiAPIConfig(db, id); err != nil {
		sendInternalServerError(w, r, err, fmt.Sprintf("Failed to delete Gemini API config %s", id))
		return
	}

	w.WriteHeader(http.StatusOK)
}
