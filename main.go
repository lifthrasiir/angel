package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

//go:embed frontend/dist
var embeddedFiles embed.FS

// serveStaticFiles serves static files from the filesystem first, then from embedded files
func serveStaticFiles(w http.ResponseWriter, r *http.Request) {
	// Try to serve from filesystem first (for development)
	fsPath := filepath.Join("frontend", "dist", r.URL.Path)

	// Check if the requested path is for a file that exists on disk
	if _, err := os.Stat(fsPath); err == nil {
		http.ServeFile(w, r, fsPath)
		return
	}

	// If not found on filesystem, try to serve from embedded files
	// The embedded files are rooted at frontend/dist, so we need to strip the prefix
	// We need to create a sub-filesystem that is rooted at "frontend/dist" within the embedded files.
	// This ensures that http.FileServer correctly resolves paths like "/index.html" to "frontend/dist/index.html"
	// within the embedded filesystem.
	fsys, err := fs.Sub(embeddedFiles, "frontend/dist")
	if err != nil {
		log.Printf("Error creating sub-filesystem: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	http.FileServer(http.FS(fsys)).ServeHTTP(w, r)
	return
}

// serveSPAIndex serves the index.html file for SPA fallback
func serveSPAIndex(w http.ResponseWriter, r *http.Request) {
	// Try to serve from filesystem first (for development)
	fsPath := filepath.Join("frontend", "dist", "index.html")

	if _, err := os.Stat(fsPath); err == nil {
		http.ServeFile(w, r, fsPath)
		return
	}

	// If not found on filesystem, try to serve from embedded files
	file, err := embeddedFiles.Open("frontend/dist/index.html")
	if err != nil {
		log.Printf("Error opening embedded index.html: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		log.Printf("Error reading embedded index.html: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(content)
}

// decodeJSONRequest decodes JSON request body with error handling
func decodeJSONRequest(r *http.Request, w http.ResponseWriter, target interface{}, handlerName string) bool {
	if err := json.NewDecoder(r.Body).Decode(target); err != nil {
		// Check if it's an EOF error, which happens with empty body
		if err == io.EOF || err.Error() == "unexpected EOF" {
			log.Printf("%s: Empty request body", handlerName)
			http.Error(w, "Empty request body", http.StatusBadRequest)
		} else {
			log.Printf("%s: Invalid request body: %v", handlerName, err)
			http.Error(w, "Invalid request body", http.StatusBadRequest)
		}
		return false
	}
	return true
}

// sendJSONResponse sets JSON headers and encodes response
func sendJSONResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func main() {

	db, err := InitDB("angel.db")
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()
	InitMCPManager(db)

	var ga GeminiAuth
	ga.Init(db)

	router := mux.NewRouter()
	router.Use(makeContextMiddleware(db, &ga))

	// OAuth2 handler is only active for LOGIN_WITH_GOOGLE method
	if ga.SelectedAuthType == AuthTypeLoginWithGoogle {
		router.HandleFunc("/login", HandleGoogleLogin).Methods("GET")
		router.HandleFunc("/oauth2callback", HandleGoogleCallback).Methods("GET")
	}
	router.HandleFunc("/api/logout", ga.HandleLogout).Methods("POST")

	InitRouter(router)

	fmt.Println("Server started at http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", router))
}

func InitRouter(router *mux.Router) {
	router.HandleFunc("/new", serveSPAIndex).Methods("GET")
	router.HandleFunc("/settings", serveSPAIndex).Methods("GET")

	router.HandleFunc("/w", handleNotFound).Methods("GET")
	router.HandleFunc("/w/new", serveSPAIndex).Methods("GET")
	router.HandleFunc("/w/{workspaceId}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, fmt.Sprintf("/w/%s/new", mux.Vars(r)["workspaceId"]), http.StatusFound)
	}).Methods("GET")
	router.HandleFunc("/w/{workspaceId}/new", serveSPAIndex).Methods("GET")
	router.HandleFunc("/w/{workspaceId}/{sessionId}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, fmt.Sprintf("/%s", mux.Vars(r)["sessionId"]), http.StatusFound)
	}).Methods("GET")

	// API handlers
	router.HandleFunc("/api/workspaces", createWorkspaceHandler).Methods("POST")
	router.HandleFunc("/api/workspaces", listWorkspacesHandler).Methods("GET")
	router.HandleFunc("/api/workspaces/{id}", deleteWorkspaceHandler).Methods("DELETE")

	router.HandleFunc("/api/chat", listSessionsByWorkspaceHandler).Methods("GET")
	router.HandleFunc("/api/chat", newSessionAndMessage).Methods("POST")
	router.HandleFunc("/api/chat/{sessionId}", chatMessage).Methods("POST")
	router.HandleFunc("/api/chat/{sessionId}", loadChatSession).Methods("GET")
	router.HandleFunc("/api/chat/{sessionId}/name", updateSessionNameHandler).Methods("POST")
	router.HandleFunc("/api/chat/{sessionId}/call", handleCall).Methods("GET", "DELETE")
	router.HandleFunc("/api/chat/{sessionId}", deleteSession).Methods("DELETE")
	router.HandleFunc("/api/chat/{sessionId}/branch", createBranchHandler).Methods("POST")
	router.HandleFunc("/api/chat/{sessionId}/branch", switchBranchHandler).Methods("PUT")

	router.HandleFunc("/api/userinfo", getUserInfoHandler).Methods("GET")
	router.HandleFunc("/api/countTokens", countTokensHandler).Methods("POST")
	router.HandleFunc("/api/evaluatePrompt", handleEvaluatePrompt).Methods("POST")
	router.HandleFunc("/api/mcp/configs", getMCPConfigsHandler).Methods("GET")
	router.HandleFunc("/api/mcp/configs", saveMCPConfigHandler).Methods("POST")
	router.HandleFunc("/api/mcp/configs/{name}", deleteMCPConfigHandler).Methods("DELETE")
	router.HandleFunc("/api", handleNotFound)

	router.HandleFunc("/{sessionId}", handleSessionPage).Methods("GET")
	router.PathPrefix("/").HandlerFunc(serveStaticFiles)
}

// Context keys for storing values in context.Context
type contextKey uint8

const (
	dbKey contextKey = iota
	gaKey
)

func contextWithGlobals(ctx context.Context, db *sql.DB, ga *GeminiAuth) context.Context {
	ctx = context.WithValue(ctx, dbKey, db)
	ctx = context.WithValue(ctx, gaKey, ga)
	return ctx
}

func makeContextMiddleware(db *sql.DB, ga *GeminiAuth) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r = r.WithContext(contextWithGlobals(r.Context(), db, ga))

			next.ServeHTTP(w, r)
		})
	}
}

func getDb(w http.ResponseWriter, r *http.Request) *sql.DB {
	db, ok := r.Context().Value(dbKey).(*sql.DB)
	if !ok {
		http.Error(w, "Internal Server Error: Database connection missing.", http.StatusInternalServerError)
		runtime.Goexit()
	}
	return db
}

func getGeminiAuth(w http.ResponseWriter, r *http.Request) *GeminiAuth {
	ga, ok := r.Context().Value(gaKey).(*GeminiAuth)
	if !ok {
		http.Error(w, "Internal Server Error: GeminiAuth missing.", http.StatusInternalServerError)
		runtime.Goexit()
	}
	return ga
}

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

func generateID() string {
	for {
		b := make([]byte, 8) // 8 bytes will result in an 11-character base64 string
		if _, err := rand.Read(b); err != nil {
			log.Printf("Error generating random ID: %v", err)
			// Fallback to UUID or handle error appropriately
			return uuid.New().String() // Fallback to UUID if random generation fails
		}
		id := base64.RawURLEncoding.EncodeToString(b)
		// Check if the ID contains any uppercase letters
		hasUppercase := false
		for _, r := range id {
			if r >= 'A' && r <= 'Z' {
				hasUppercase = true
				break
			}
		}
		// If it has at least one uppercase letter, it's unlikely to collide with /new or /w/new
		if hasUppercase {
			return id
		}
		log.Println("Generated ID without uppercase, regenerating to avoid collision with /new or /w/new.")
	}
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
	db := getDb(w, r)
	ga := getGeminiAuth(w, r)

	// Check if logged in
	if ga.SelectedAuthType == AuthTypeLoginWithGoogle && (ga.Token == nil || !ga.Token.Valid()) {
		http.Error(w, "Not logged in", http.StatusUnauthorized)
		return
	}

	// If UserEmail is empty but token is valid, try to re-fetch user info
	if ga.SelectedAuthType == AuthTypeLoginWithGoogle && ga.UserEmail == "" && ga.Token != nil {
		log.Println("getUserInfoHandler: UserEmail is empty, attempting to fetch from Google API...")
		if err := ga.GetUserInfoAndSave(db); err != nil {
			log.Printf("getUserInfoHandler: Failed to fetch user info: %v", err)
		}
	}

	sendJSONResponse(w, map[string]string{"email": ga.UserEmail})
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
	ga := getGeminiAuth(w, r)

	if !ga.ValidateAuthAndProject("countTokensHandler", w, r) {
		return
	}

	var requestBody struct {
		Text string `json:"text"`
	}

	if !decodeJSONRequest(r, w, &requestBody, "countTokensHandler") {
		return
	}

	modelName := DefaultGeminiModel

	contents := []Content{
		{
			Role:  "user",
			Parts: []Part{{Text: requestBody.Text}},
		},
	}

	resp, err := CurrentProvider.CountTokens(context.Background(), contents, modelName)
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

func callFunction(fc FunctionCall) (map[string]interface{}, error) {
	return CallToolFunction(fc)
}

// handleCall handles GET and DELETE requests for /api/calls/{sessionId}
func handleCall(w http.ResponseWriter, r *http.Request) {
	ga := getGeminiAuth(w, r)

	if !ga.ValidateAuthAndProject("handleCall", w, r) {
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
	var requestBody struct {
		Template string `json:"template"`
	}

	if !decodeJSONRequest(r, w, &requestBody, "handleEvaluatePrompt") {
		return
	}

	evaluatedPrompt, err := EvaluatePrompt(requestBody.Template)
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
