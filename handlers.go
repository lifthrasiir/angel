package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"golang.org/x/oauth2"

	. "github.com/lifthrasiir/angel/gemini"
	"github.com/lifthrasiir/angel/internal/chat"
	"github.com/lifthrasiir/angel/internal/database"
	"github.com/lifthrasiir/angel/internal/env"
	"github.com/lifthrasiir/angel/internal/llm"
	"github.com/lifthrasiir/angel/internal/prompts"
	. "github.com/lifthrasiir/angel/internal/types"
)

// authHandler handles authentication requests.
func authHandler(w http.ResponseWriter, r *http.Request) {
	ga := getGeminiAuth(w, r)

	// Capture the entire raw query string from the /login request.
	// This will contain both 'redirect_to' and 'draft_message'.
	redirectToQueryString := r.URL.RawQuery
	if redirectToQueryString == "" {
		// If no query parameters, default to redirecting to the root.
		// This case might not be hit if frontend always sends redirect_to.
		redirectToQueryString = "redirect_to=/" // Ensure a default redirect_to
	}

	// Parse query parameters to get provider
	queryParams, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		log.Printf("Error parsing query parameters: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	provider := queryParams.Get("provider")
	if provider == "" {
		provider = "geminicli" // default provider
	}

	// Generate auth URL
	authURL, err := ga.GenerateAuthURL(provider, redirectToQueryString)
	if err != nil {
		log.Printf("Error generating auth URL: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
}

// authCallbackHandler handles authentication callback requests.
func authCallbackHandler(w http.ResponseWriter, r *http.Request) {
	ga := getGeminiAuth(w, r)
	db := getDb(w, r)

	stateParam := r.FormValue("state")
	code := r.FormValue("code")

	// Handle callback using business logic
	redirectURL, oauthToken, err := ga.HandleCallback(r.Context(), stateParam, code)
	if err != nil {
		log.Printf("Error handling callback: %v", err)
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}

	err = database.SaveOAuthToken(db, oauthToken)
	if err != nil {
		log.Printf("Error saving OAuth token to DB: %v", err)
		// Continue even if saving fails
	}

	// Redirect to the original path after successful authentication
	http.Redirect(w, r, redirectURL, http.StatusTemporaryRedirect)
}

// logoutHandler handles logout requests.
func logoutHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)

	// Parse request body to get which account to logout
	var request struct {
		Email string `json:"email"`
		ID    int    `json:"id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		log.Printf("Failed to decode logout request: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	var err error
	if request.ID != 0 {
		// Delete specific account by ID
		err = database.DeleteOAuthTokenByID(db, request.ID)
		log.Printf("Logged out account with ID %d", request.ID)
	} else if request.Email != "" {
		// Delete specific account by email
		err = database.DeleteOAuthTokenByEmail(db, request.Email, "geminicli")
		log.Printf("Logged out account for email %s", request.Email)
	} else {
		http.Error(w, "Either email or id must be provided", http.StatusBadRequest)
		return
	}

	if err != nil {
		log.Printf("Failed to delete OAuth token from DB: %v", err)
		http.Error(w, "Failed to logout", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "Account logged out successfully")
}
func handleSessionPage(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)

	vars := mux.Vars(r)
	sessionId := vars["sessionId"]

	if sessionId == "" {
		sendNotFoundError(w, r, "Session not found")
		return
	}

	exists, err := database.SessionExists(db, sessionId)
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

// listAccountsHandler returns a list of all OAuth accounts (geminicli, antigravity, etc.) associated with the user
func listAccountsHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)

	tokens, err := database.GetOAuthTokens(db)
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to get OAuth tokens")
		return
	}

	// Prepare response for all OAuth tokens (excluding sensitive projectId)
	accounts := make([]map[string]interface{}, 0)
	for _, token := range tokens {
		account := map[string]interface{}{
			"id":         token.ID,
			"email":      token.UserEmail,
			"createdAt":  token.CreatedAt,
			"updatedAt":  token.UpdatedAt,
			"kind":       token.Kind,
			"hasProject": token.ProjectID != "", // Include project ID status
		}
		accounts = append(accounts, account)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(accounts); err != nil {
		log.Printf("Failed to encode accounts response: %v", err)
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// AccountDetailsResponse is the unified response format for the account details endpoint
type AccountDetailsResponse struct {
	Source string                  `json:"source"` // "models" or "quota"
	Models map[string]ModelDetails `json:"models"`
}

// getAccountDetailsHandler retrieves detailed information about an OAuth account
// using v1internal:fetchAvailableModels or v1internal:retrieveUserQuota (fallback)
func getAccountDetailsHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db := getDb(w, r)

	// Extract account ID from URL
	vars := mux.Vars(r)
	accountIDStr := vars["id"]
	accountID, err := strconv.Atoi(accountIDStr)
	if err != nil {
		sendBadRequestError(w, r, "Invalid account ID")
		return
	}

	// Load OAuth token from database
	token, err := database.GetOAuthToken(db, accountID)
	if err != nil {
		if err == sql.ErrNoRows {
			sendNotFoundError(w, r, "Account not found")
		} else {
			sendInternalServerError(w, r, err, "Failed to load account")
		}
		return
	}

	// Parse OAuth token
	var oauthToken oauth2.Token
	if err := json.Unmarshal([]byte(token.TokenData), &oauthToken); err != nil {
		sendInternalServerError(w, r, err, "Failed to parse OAuth token")
		return
	}

	// Get GeminiAuth to access OAuth config
	ga, err := llm.GeminiAuthFromContext(ctx)
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to get authentication context")
		return
	}

	// Create token source with OAuth config
	oauthConfig := ga.OAuthConfig(token.Kind)
	tokenSource := oauthConfig.TokenSource(context.Background(), &oauthToken)

	// Create CodeAssistClient
	client := NewCodeAssistClient(TokenSourceClientProvider(tokenSource), token.ProjectID, token.Kind)

	switch token.Kind {
	case "antigravity":
		modelsResp, err := client.FetchAvailableModels(ctx)
		if err != nil {
			// Some other error - return it
			sendInternalServerError(w, r, err, "Failed to fetch available models")
			return
		}

		// Success - calculate usages for each model
		models := calculateModelUsages(modelsResp)

		response := AccountDetailsResponse{
			Source: "models",
			Models: models,
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			log.Printf("Failed to encode response: %v", err)
			http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		}

	default:
		quotaResp, err := client.RetrieveUserQuota(ctx)
		if err != nil {
			sendInternalServerError(w, r, err, "Failed to retrieve user quota")
			return
		}

		// Transform quota buckets into models map
		models := make(map[string]ModelDetails)
		for _, bucket := range quotaResp.Buckets {
			models[bucket.ModelID] = ModelDetails{
				DisplayName: bucket.ModelID, // Fallback: use ID as name
				QuotaInfo: &QuotaInfo{
					RemainingFraction: bucket.RemainingFraction,
					ResetTime:         bucket.ResetTime,
				},
			}
		}

		response := AccountDetailsResponse{
			Source: "quota",
			Models: models,
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			log.Printf("Failed to encode response: %v", err)
			http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		}
	}
}

// calculateModelUsages calculates the usages array for each model based on the usage category arrays
func calculateModelUsages(resp *FetchAvailableModelsResponse) map[string]ModelDetails {
	models := make(map[string]ModelDetails)

	// Create a map for quick lookup of which categories each model belongs to
	usageCategories := make(map[string][]string)

	// Check each usage category
	for _, modelID := range resp.CommandModelIds {
		usageCategories[modelID] = append(usageCategories[modelID], "command")
	}
	for _, modelID := range resp.TabModelIds {
		usageCategories[modelID] = append(usageCategories[modelID], "tab")
	}
	for _, modelID := range resp.ImageGenerationModelIds {
		usageCategories[modelID] = append(usageCategories[modelID], "imageGeneration")
	}
	for _, modelID := range resp.MqueryModelIds {
		usageCategories[modelID] = append(usageCategories[modelID], "mquery")
	}
	for _, modelID := range resp.WebSearchModelIds {
		usageCategories[modelID] = append(usageCategories[modelID], "webSearch")
	}

	// Copy models and add usages
	for modelID, details := range resp.Models {
		details.Usages = usageCategories[modelID]
		models[modelID] = details
	}

	return models
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

	if err := database.UpdateSessionName(db, sessionId, requestBody.Name); err != nil {
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
		_, err := database.GetWorkspace(db, requestBody.WorkspaceID)
		if err != nil {
			sendNotFoundError(w, r, "Workspace not found")
			return
		}
	}

	// Verify that the session exists
	exists, err := database.SessionExists(db, sessionId)
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to check session existence")
		return
	}
	if !exists {
		sendNotFoundError(w, r, "Session not found")
		return
	}

	if err := database.UpdateSessionWorkspace(db, sessionId, requestBody.WorkspaceID); err != nil {
		sendInternalServerError(w, r, err, "Failed to update session workspace")
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "Session workspace updated successfully")
}

func createWorkspaceHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)

	var requestBody struct {
		Name string `json:"name"`
	}

	if !decodeJSONRequest(r, w, &requestBody, "createWorkspaceHandler") {
		return
	}

	workspaceID := database.GenerateID() // Reusing session ID generation for workspace ID

	if err := database.CreateWorkspace(db, workspaceID, requestBody.Name, ""); err != nil {
		sendInternalServerError(w, r, err, "Failed to create workspace")
		return
	}

	sendJSONResponse(w, map[string]string{"id": workspaceID, "name": requestBody.Name})
}

func listWorkspacesHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)

	workspaces, err := database.GetAllWorkspaces(db)
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

	if err := database.DeleteWorkspace(db, workspaceID); err != nil {
		sendInternalServerError(w, r, err, "Failed to delete workspace")
		return
	}

	sendJSONResponse(w, map[string]string{"status": "success", "message": "Workspace deleted successfully"})
}

func countTokensHandler(w http.ResponseWriter, r *http.Request) {
	models := getModels(w, r)

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

	modelProvider, err := models.GetModelProvider(modelName)
	if err != nil {
		sendBadRequestError(w, r, err.Error())
		return
	}

	contents := []Content{
		{
			Role:  RoleUser,
			Parts: []Part{{Text: requestBody.Text}},
		},
	}

	resp, err := modelProvider.CountTokens(context.Background(), contents)
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
	vars := mux.Vars(r)
	sessionId := vars["sessionId"]
	if sessionId == "" {
		sendBadRequestError(w, r, "Session ID is required")
		return
	}

	switch r.Method {
	case "GET":
		isActive := chat.HasActiveCall(sessionId)
		sendJSONResponse(w, map[string]bool{"isActive": isActive})
	case "DELETE":
		if err := chat.CancelCall(sessionId); err != nil {
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
		workspace, err := database.GetWorkspace(db, requestBody.WorkspaceID)
		if err != nil {
			sendInternalServerError(w, r, err, fmt.Sprintf("Failed to get workspace %s", requestBody.WorkspaceID))
			return
		}
		workspaceName = workspace.Name
	}

	data := prompts.NewPromptData(workspaceName)
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
	tools := getTools(w, r)

	dbConfigs, err := database.GetMCPServerConfigs(db)
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to retrieve MCP configs from DB")
		return
	}

	activeConnections := tools.GetMCPConnections()
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
			builtinToolNames := tools.BuiltinNames()
			var tools []string
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
	tools := getTools(w, r)

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

	if err := database.SaveMCPServerConfig(db, config); err != nil {
		sendInternalServerError(w, r, err, fmt.Sprintf("Failed to save MCP config %s", config.Name))
		return
	}

	// Start or stop the connection based on the new enabled state
	if config.Enabled {
		tools.GetMCPManager().StartConnection(config)
	} else {
		tools.GetMCPManager().StopConnection(config.Name)
	}

	sendJSONResponse(w, config)
}

func deleteMCPConfigHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)
	tools := getTools(w, r)

	name := mux.Vars(r)["name"]
	if name == "" {
		sendBadRequestError(w, r, "MCP config name is required")
		return
	}

	if err := database.DeleteMCPServerConfig(db, name); err != nil {
		sendInternalServerError(w, r, err, fmt.Sprintf("Failed to delete MCP config %s", name))
		return
	}

	tools.GetMCPManager().StopConnection(name)

	sendJSONResponse(w, map[string]string{"status": "success", "message": "MCP config deleted successfully"})
}

// sendInternalServerError logs the error and sends a 500 Internal Server Error response.
// As special cases, BadRequestError and NotFoundError types are handled to send 400 and 404 responses respectively.
func sendInternalServerError(w http.ResponseWriter, r *http.Request, err error, msg string) {
	switch err.(type) {
	case chat.BadRequestError:
		sendBadRequestError(w, r, err.Error())
	case chat.NotFoundError:
		sendNotFoundError(w, r, err.Error())
	default:
		log.Printf("%s: %v", msg, err)
		http.Error(w, fmt.Sprintf("Internal Server Error: %s", msg), http.StatusInternalServerError)
	}
}

// sendBadRequestError sends a 400 Bad Request response.
func sendBadRequestError(w http.ResponseWriter, _ *http.Request, msg string) {
	http.Error(w, msg, http.StatusBadRequest)
}

// sendNotFoundError sends a 404 Not Found response.
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

func listModelsHandler(w http.ResponseWriter, r *http.Request) {
	models := getModels(w, r)

	var modelInfos []ModelInfo
	for _, model := range models.GetAllModels() {
		modelInfos = append(modelInfos, ModelInfo{
			Name:      model.Name,
			MaxTokens: model.MaxTokens,
		})
	}

	sendJSONResponse(w, modelInfos)
}

func updateSessionRootsHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)

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
	sessionFS, err := env.GetSessionFS(r.Context(), sessionId)
	if err != nil {
		sendInternalServerError(w, r, err, fmt.Sprintf("Failed to get SessionFS for session %s", sessionId))
		return
	}
	defer env.ReleaseSessionFS(sessionId)

	// Get current roots before update for EnvChanged calculation
	oldRoots := sessionFS.Roots()

	// Update SessionFS with the new roots
	if err := sessionFS.SetRoots(requestBody.Roots); err != nil {
		sendInternalServerError(w, r, err, fmt.Sprintf("Failed to set roots for session %s", sessionId))
		return
	}

	// Update the database
	_, err = database.AddSessionEnv(db, sessionId, requestBody.Roots)
	if err != nil {
		sendInternalServerError(w, r, err, fmt.Sprintf("Failed to update session roots in DB for session %s", sessionId))
		return
	}

	// Calculate EnvChanged
	rootsChanged, err := env.CalculateRootsChanged(oldRoots, requestBody.Roots)
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

	data, err := database.GetBlob(db, blobHash) // GetBlob works with hash
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

	prompts, err := database.GetGlobalPrompts(db)
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
		if err := database.SaveGlobalPrompts(db, prompts); err != nil {
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

	if err := database.SaveGlobalPrompts(db, prompts); err != nil {
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
	Results []database.SearchResult `json:"results"`
	HasMore bool                    `json:"has_more"`
}

// searchMessagesHandler handles POST requests to /api/search
func searchMessagesHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)

	var req SearchRequest
	if !decodeJSONRequest(r, w, &req, "searchMessagesHandler") {
		return
	}

	results, hasMore, err := database.SearchMessages(db, req.Query, req.MaxID, req.Limit, req.WorkspaceID)
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

	configs, err := database.GetOpenAIConfigs(db)
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to retrieve OpenAI configs")
		return
	}

	sendJSONResponse(w, configs)
}

// saveOpenAIConfigHandler handles POST requests for /api/openai-configs
func saveOpenAIConfigHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)
	models := getModels(w, r)

	var config OpenAIConfig
	if !decodeJSONRequest(r, w, &config, "saveOpenAIConfigHandler") {
		return
	}

	// Generate ID if not provided
	if config.ID == "" {
		config.ID = database.GenerateID()
	}

	if err := database.SaveOpenAIConfig(db, config); err != nil {
		sendInternalServerError(w, r, err, fmt.Sprintf("Failed to save OpenAI config %s", config.Name))
		return
	}

	// Reload OpenAI providers to reflect the changes
	llm.ReloadOpenAIProviders(db, models)

	sendJSONResponse(w, config)
}

// deleteOpenAIConfigHandler handles DELETE requests for /api/openai-configs/{id}
func deleteOpenAIConfigHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)
	models := getModels(w, r)

	id := mux.Vars(r)["id"]
	if id == "" {
		sendBadRequestError(w, r, "OpenAI config ID is required")
		return
	}

	if err := database.DeleteOpenAIConfig(db, id); err != nil {
		sendInternalServerError(w, r, err, fmt.Sprintf("Failed to delete OpenAI config %s", id))
		return
	}

	// Reload OpenAI providers to reflect the deletion
	llm.ReloadOpenAIProviders(db, models)

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
	config, err := database.GetOpenAIConfig(db, id)
	if err != nil {
		sendNotFoundError(w, r, fmt.Sprintf("OpenAI config with ID %s not found", id))
		return
	}

	// Create client and get models
	client := llm.NewOpenAIClient(config)
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
	config, err := database.GetOpenAIConfig(db, id)
	if err != nil {
		sendNotFoundError(w, r, fmt.Sprintf("OpenAI config with ID %s not found", id))
		return
	}

	// Create client and refresh models
	client := llm.NewOpenAIClient(config)
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

	configs, err := database.GetGeminiAPIConfigs(db)
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
		config.ID = database.GenerateID()
	}

	if err := database.SaveGeminiAPIConfig(db, config); err != nil {
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

	if err := database.DeleteGeminiAPIConfig(db, id); err != nil {
		sendInternalServerError(w, r, err, fmt.Sprintf("Failed to delete Gemini API config %s", id))
		return
	}

	w.WriteHeader(http.StatusOK)
}
