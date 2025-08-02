package main

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

//go:embed frontend/dist
var embeddedFiles embed.FS

var (
	oauthStates       = make(map[string]string) // Stores randomState -> originalQueryString
	GlobalGeminiState GeminiState
)

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

// validateAuthAndProject performs common auth and project validation for handlers
func validateAuthAndProject(handlerName string, w http.ResponseWriter) bool {
	// Check if GeminiClient is initialized and attempt to re-initialize if needed
	if GlobalGeminiState.GeminiClient == nil {
		log.Printf("%s: GeminiClient not initialized, attempting to re-initialize...", handlerName)
		if GlobalGeminiState.SelectedAuthType == AuthTypeLoginWithGoogle && GlobalGeminiState.Token != nil && GlobalGeminiState.Token.Valid() {
			GlobalGeminiState.InitGeminiClient()
		}
		if GlobalGeminiState.GeminiClient == nil {
			log.Printf("%s: GeminiClient still not initialized after re-attempt.", handlerName)
			http.Error(w, "CodeAssist client not initialized. Check authentication method.", http.StatusUnauthorized)
			return false
		}
	}

	if GlobalGeminiState.SelectedAuthType == AuthTypeLoginWithGoogle && GlobalGeminiState.ProjectID == "" {
		log.Printf("%s: Project ID is not set. Please log in again.", handlerName)
		http.Error(w, "Project ID is not set. Please log in again.", http.StatusUnauthorized)
		return false
	}
	return true
}

// setupSSEHeaders sets up Server-Sent Events headers
func setupSSEHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
}

// decodeJSONRequest decodes JSON request body with error handling
func decodeJSONRequest(r *http.Request, w http.ResponseWriter, target interface{}, handlerName string) bool {
	if err := json.NewDecoder(r.Body).Decode(target); err != nil {
		log.Printf("%s: Invalid request body: %v", handlerName, err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return false
	}
	return true
}

// sendJSONResponse sets JSON headers and encodes response
func sendJSONResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func init() {
	InitDB()
	GlobalGeminiState.OAuthState = "random"
	GlobalGeminiState.LoadToken()
	InitAuth(&GlobalGeminiState)
}

func main() {
	router := mux.NewRouter()

	// OAuth2 handler is only active for LOGIN_WITH_GOOGLE method
	if GlobalGeminiState.SelectedAuthType == AuthTypeLoginWithGoogle {
		router.HandleFunc("/login", makeAuthHandler(&GlobalGeminiState, HandleGoogleLogin)).Methods("GET")
		router.HandleFunc("/oauth2callback", makeAuthHandler(&GlobalGeminiState, HandleGoogleCallback)).Methods("GET")
	}

	router.HandleFunc("/new", newChatSession).Methods("GET")
	router.HandleFunc("/{sessionId}", handleSessionPage).Methods("GET") // New handler for /:sessionId

	// API handlers
	router.HandleFunc("/api/chat", listChatSessions).Methods("GET")
	router.HandleFunc("/api/chat", newSessionAndMessage).Methods("POST")
	router.HandleFunc("/api/chat/{sessionId}", chatMessage).Methods("POST")
	router.HandleFunc("/api/chat/{sessionId}", loadChatSession).Methods("GET")
	router.HandleFunc("/api/chat/{sessionId}/name", updateSessionNameHandler).Methods("POST")
	router.HandleFunc("/api/chat/{sessionId}/call", handleCall).Methods("GET", "DELETE")
	router.HandleFunc("/api/chat/{sessionId}", deleteSession).Methods("DELETE")
	router.HandleFunc("/api/userinfo", getUserInfoHandler).Methods("GET")
	router.HandleFunc("/api/logout", handleLogout).Methods("POST")
	router.HandleFunc("/api/countTokens", countTokensHandler).Methods("POST")
	router.HandleFunc("/api/evaluatePrompt", handleEvaluatePrompt).Methods("POST")

	// Serve frontend static files
	router.PathPrefix("/").Handler(http.HandlerFunc(serveStaticFiles))
	router.NotFoundHandler = http.HandlerFunc(serveNotFound)

	fmt.Println("Server started at http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", router))
}

func makeAuthHandler(gs *GeminiState, handler func(gs *GeminiState, w http.ResponseWriter, r *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handler(gs, w, r)
	}
}

// New chat session start handler
func newChatSession(w http.ResponseWriter, r *http.Request) {
	serveIndexHTML(w, r)
}

// handleSessionPage handles requests for /:sessionId
func handleSessionPage(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	sessionId := vars["sessionId"]

	// Check if sessionId contains at least one uppercase letter
	if !strings.ContainsAny(sessionId, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") {
		serveNotFound(w, r)
		return
	}

	// Check if the session exists
	_, err := GetSession(sessionId) // Use GetSession instead of GetChatSession
	if err != nil {
		log.Printf("Session %s not found: %v", sessionId, err)
		serveNotFound(w, r)
		return
	}

	// If session exists and is valid, serve index.html
	serveIndexHTML(w, r)
}

// serveIndexHTML serves the embedded index.html file
func serveIndexHTML(w http.ResponseWriter, r *http.Request) {
	indexEmbedded, err := embeddedFiles.ReadFile("frontend/dist/index.html")
	if err != nil {
		log.Printf("Error reading embedded index.html: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(indexEmbedded)
}

// serveNotFound serves the custom 404.html page
func serveNotFound(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotFound)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// Read the embedded 404.html
	page404, err := embeddedFiles.ReadFile("frontend/dist/404.html")
	if err != nil {
		log.Printf("Error reading embedded 404.html: %v", err)
		http.Error(w, "404 Not Found", http.StatusNotFound)
		return
	}
	w.Write(page404)
}

func GenerateSessionID() string {
	for {
		b := make([]byte, 8) // 8 bytes will result in an 11-character base64 string
		if _, err := rand.Read(b); err != nil {
			log.Printf("Error generating random session ID: %v", err)
			// Fallback to UUID if random generation fails, as a last resort
			return uuid.New().String()
		}
		sessionID := base64.RawURLEncoding.EncodeToString(b)
		if strings.ContainsAny(sessionID, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") {
			return sessionID
		}
	}
}

// New handler to get user info
func getUserInfoHandler(w http.ResponseWriter, r *http.Request) {
	// Check if logged in
	if GlobalGeminiState.SelectedAuthType == AuthTypeLoginWithGoogle && (GlobalGeminiState.Token == nil || !GlobalGeminiState.Token.Valid()) {
		http.Error(w, "Not logged in", http.StatusUnauthorized)
		return
	}

	// If UserEmail is empty but token is valid, try to re-fetch user info
	if GlobalGeminiState.SelectedAuthType == AuthTypeLoginWithGoogle && GlobalGeminiState.UserEmail == "" && GlobalGeminiState.Token != nil {
		log.Println("getUserInfoHandler: UserEmail is empty, attempting to fetch from Google API...")
		ctx := context.Background()
		userInfoClient := GlobalGeminiState.GoogleOauthConfig.Client(ctx, GlobalGeminiState.Token)
		userInfoResp, err := userInfoClient.Get("https://www.googleapis.com/oauth2/v2/userinfo")
		if err != nil {
			log.Printf("getUserInfoHandler: Failed to fetch user info: %v", err)
		} else {
			defer userInfoResp.Body.Close()
			var userInfo struct {
				Email string `json:"email"`
			}
			if err := json.NewDecoder(userInfoResp.Body).Decode(&userInfo); err != nil {
				log.Printf("getUserInfoHandler: Failed to decode user info JSON: %v", err)
			} else {

				GlobalGeminiState.UserEmail = userInfo.Email
				// Update the token in DB with user email
				GlobalGeminiState.SaveToken(GlobalGeminiState.Token)
			}
		}
	}

	sendJSONResponse(w, map[string]string{"email": GlobalGeminiState.UserEmail})
}

// New handler to update session name
func updateSessionNameHandler(w http.ResponseWriter, r *http.Request) {
	var requestBody struct {
		SessionID string `json:"sessionId"`
		Name      string `json:"name"`
	}

	if !decodeJSONRequest(r, w, &requestBody, "updateSessionNameHandler") {
		return
	}

	if err := UpdateSessionName(requestBody.SessionID, requestBody.Name); err != nil {
		log.Printf("Failed to update session name for %s: %v", requestBody.SessionID, err)
		http.Error(w, fmt.Sprintf("Failed to update session name: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "Session name updated successfully")
}

// Handle logout
func handleLogout(w http.ResponseWriter, r *http.Request) {
	GlobalGeminiState.Token = nil
	GlobalGeminiState.UserEmail = ""
	GlobalGeminiState.GeminiClient = nil

	// Delete token from DB
	if err := DeleteOAuthToken(); err != nil {
		log.Printf("Failed to delete OAuth token from DB: %v", err)
		http.Error(w, "Failed to logout", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "Logged out successfully")
}

func countTokensHandler(w http.ResponseWriter, r *http.Request) {
	if !validateAuthAndProject("countTokensHandler", w) {
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

	resp, err := GlobalGeminiState.GeminiClient.CountTokens(context.Background(), contents, modelName)
	if err != nil {
		log.Printf("CountTokens API call failed: %v", err)
		http.Error(w, fmt.Sprintf("CountTokens API call failed: %v", err), http.StatusInternalServerError)
		return
	}

	sendJSONResponse(w, map[string]int{"totalTokens": resp.TotalTokens})
}

func callFunction(fc FunctionCall) (map[string]interface{}, error) {
	return CallToolFunction(fc)
}

// handleCall handles GET and DELETE requests for /api/calls/{sessionId}
func handleCall(w http.ResponseWriter, r *http.Request) {
	if !validateAuthAndProject("handleCall", w) {
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
