package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

var (
	oauthStates       = make(map[string]string) // Stores randomState -> originalQueryString
	GlobalGeminiState GeminiState
)

// ensureGeminiClientInitialized checks if GeminiClient is initialized and attempts to re-initialize if needed
func ensureGeminiClientInitialized(handlerName string) bool {
	if GlobalGeminiState.GeminiClient == nil {
		log.Printf("%s: GeminiClient not initialized, attempting to re-initialize...", handlerName)
		if GlobalGeminiState.SelectedAuthType == AuthTypeLoginWithGoogle && GlobalGeminiState.Token != nil && GlobalGeminiState.Token.Valid() {
			GlobalGeminiState.InitGeminiClient()
		}
		if GlobalGeminiState.GeminiClient == nil {
			log.Printf("%s: GeminiClient still not initialized after re-attempt.", handlerName)
			return false
		}
	}
	return true
}

func init() {
	InitDB()
	GlobalGeminiState.OAuthState = "random"
	GlobalGeminiState.LoadToken()
	InitAuth(&GlobalGeminiState)

	initEmbeddedZip()
}

func main() {
	defer func() {
		if exeFile != nil {
			exeFile.Close()
		}
	}()
	router := mux.NewRouter()

	// OAuth2 handler is only active for LOGIN_WITH_GOOGLE method
	if GlobalGeminiState.SelectedAuthType == AuthTypeLoginWithGoogle {
		router.HandleFunc("/login", makeAuthHandler(&GlobalGeminiState, HandleGoogleLogin)).Methods("GET")
		router.HandleFunc("/oauth2callback", makeAuthHandler(&GlobalGeminiState, HandleGoogleCallback)).Methods("GET")
	}

	// API handlers
	router.HandleFunc("/new", newChatSession).Methods("GET")
	router.HandleFunc("/api/chat/message", chatMessage).Methods("POST")
	router.HandleFunc("/api/chat/load", loadChatSession).Methods("GET")                        // New endpoint to load chat session
	router.HandleFunc("/api/chat/sessions", listChatSessions).Methods("GET")                   // New endpoint to list all chat sessions
	router.HandleFunc("/api/chat/countTokens", countTokensHandler).Methods("POST")             // Add countTokens handler
	router.HandleFunc("/api/chat/newSessionAndMessage", newSessionAndMessage).Methods("POST")  // New endpoint to create a new session and send the first message
	router.HandleFunc("/api/default-system-prompt", getDefaultSystemPrompt).Methods("GET")     // New endpoint to get the default system prompt
	router.HandleFunc("/api/chat/updateSessionName", updateSessionNameHandler).Methods("POST") // New endpoint to update session name
	router.HandleFunc("/api/userinfo", getUserInfoHandler).Methods("GET")                      // New endpoint to get user info
	router.HandleFunc("/api/logout", handleLogout).Methods("POST")                             // New endpoint for logout

	// Serve frontend static files
	router.PathPrefix("/").HandlerFunc(serveStaticFiles)

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
	serveStaticFiles(w, r)
}

func GenerateSessionID() string {
	b := make([]byte, 8) // 8 bytes will result in an 11-character base64 string
	if _, err := rand.Read(b); err != nil {
		log.Printf("Error generating random session ID: %v", err)
		// Fallback to UUID or handle error appropriately
		return uuid.New().String() // Fallback to UUID if random generation fails
	}
	return base64.RawURLEncoding.EncodeToString(b)
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"email": GlobalGeminiState.UserEmail})
}

// New handler to update session name
func updateSessionNameHandler(w http.ResponseWriter, r *http.Request) {
	var requestBody struct {
		SessionID string `json:"sessionId"`
		Name      string `json:"name"`
	}

	if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
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

// Add countTokens handler

func countTokensHandler(w http.ResponseWriter, r *http.Request) {
	if !ensureGeminiClientInitialized("countTokensHandler") {
		http.Error(w, "CodeAssist client not initialized. Check authentication method.", http.StatusUnauthorized)
		return
	}

	var requestBody struct {
		Text string `json:"text"`
	}

	if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int{"totalTokens": resp.TotalTokens})
}

func callFunction(fc FunctionCall) (map[string]interface{}, error) {
	return CallToolFunction(fc)
}
