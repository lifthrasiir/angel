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
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

func main() {
	db, err := InitDB("angel.db")
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	InitMCPManager(db)

	ga := NewGeminiAuth(db)
	ga.Init()

	// Add angel-eval provider after default models are initialized
	CurrentProviders["angel-eval"] = &AngelEvalProvider{}

	router := mux.NewRouter()
	router.Use(makeContextMiddleware(db, ga))

	// OAuth2 handler is only active for LOGIN_WITH_GOOGLE method
	if ga.GetCurrentProvider() == string(AuthTypeLoginWithGoogle) {
		router.HandleFunc("/login", http.HandlerFunc(ga.GetAuthHandler().ServeHTTP)).Methods("GET")
		router.HandleFunc("/oauth2callback", http.HandlerFunc(ga.GetAuthCallbackHandler().ServeHTTP)).Methods("GET")
	}
	router.HandleFunc("/api/logout", http.HandlerFunc(ga.GetLogoutHandler().ServeHTTP)).Methods("POST")

	InitRouter(router)

	fmt.Println("Server started at http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", router))
}

func InitRouter(router *mux.Router) {
	// Explicitly handle root-level static files
	router.HandleFunc("/favicon.ico", serveFile("favicon.ico")).Methods("GET")
	router.HandleFunc("/manifest.webmanifest", serveFile("manifest.webmanifest")).Methods("GET")
	router.HandleFunc("/angel-logo-colored.svg", serveFile("angel-logo-colored.svg")).Methods("GET")

	router.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/new", http.StatusFound)
	}).Methods("GET")

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
	router.HandleFunc("/api/chat/{sessionId}/roots", updateSessionRootsHandler).Methods("POST")
	router.HandleFunc("/api/chat/{sessionId}/call", handleCall).Methods("GET", "DELETE")
	router.HandleFunc("/api/chat/{sessionId}", deleteSession).Methods("DELETE")
	router.HandleFunc("/api/chat/{sessionId}/branch", createBranchHandler).Methods("POST")
	router.HandleFunc("/api/chat/{sessionId}/branch", switchBranchHandler).Methods("PUT")
	router.HandleFunc("/api/chat/{sessionId}/blob/{messageId}.{blobIndex}", handleDownloadBlob).Methods("GET")
	router.HandleFunc("/api/chat/{sessionId}/compress", compressSessionHandler).Methods("POST")

	router.HandleFunc("/api/userinfo", getUserInfoHandler).Methods("GET")
	router.HandleFunc("/api/countTokens", countTokensHandler).Methods("POST")
	router.HandleFunc("/api/evaluatePrompt", handleEvaluatePrompt).Methods("POST")
	router.HandleFunc("/api/mcp/configs", getMCPConfigsHandler).Methods("GET")
	router.HandleFunc("/api/mcp/configs", saveMCPConfigHandler).Methods("POST")
	router.HandleFunc("/api/mcp/configs/{name}", deleteMCPConfigHandler).Methods("DELETE")
	router.HandleFunc("/api/models", listModelsHandler).Methods("GET")
	router.HandleFunc("/api/systemPrompts", getSystemPromptsHandler).Methods("GET")
	router.HandleFunc("/api/systemPrompts", saveSystemPromptsHandler).Methods("PUT")
	router.HandleFunc("/api/ui/directory", handlePickDirectory).Methods("POST")
	router.HandleFunc("/api", handleNotFound)

	router.PathPrefix("/assets/").HandlerFunc(serveStaticFiles)

	router.HandleFunc("/{sessionId}", handleSessionPage).Methods("GET")
}

//go:embed frontend/dist
var embeddedFiles embed.FS

// serveStaticFiles serves static files from the filesystem first, then from embedded files
func serveStaticFiles(w http.ResponseWriter, r *http.Request) {
	// Strip the /assets/ prefix for serving from frontend/dist/assets
	assetPath := strings.TrimPrefix(r.URL.Path, "/assets/")
	fsPath := filepath.Join("frontend", "dist", "assets", assetPath)

	// Check if the requested path is for a file that exists on disk
	if _, err := os.Stat(fsPath); err == nil {
		http.ServeFile(w, r, fsPath)
		return
	}

	// If not found on filesystem, try to serve from embedded files
	fsys, err := fs.Sub(embeddedFiles, "frontend/dist/assets")
	if err != nil {
		log.Printf("Error creating sub-filesystem for assets: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	http.FileServer(http.FS(fsys)).ServeHTTP(w, r)
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

// serveFile serves a specific file from frontend/dist (or embedded)
func serveFile(filePath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Try to serve from filesystem first (for development)
		fsPath := filepath.Join("frontend", "dist", filePath)

		if _, err := os.Stat(fsPath); err == nil {
			http.ServeFile(w, r, fsPath)
			return
		}

		// If not found on filesystem, try to serve from embedded files
		file, err := embeddedFiles.Open(filepath.Join("frontend/dist", filePath))
		if err != nil {
			log.Printf("Error opening embedded file %s: %v", filePath, err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		defer file.Close()

		content, err := io.ReadAll(file)
		if err != nil {
			log.Printf("Error reading embedded file %s: %v", filePath, err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		// Determine content type based on file extension
		contentType := ""
		switch filepath.Ext(filePath) {
		case ".ico":
			contentType = "image/x-icon"
		case ".webmanifest":
			contentType = "application/manifest+json"
		case ".svg":
			contentType = "image/svg+xml"
		case ".png":
			contentType = "image/png"
		default:
			contentType = "application/octet-stream" // Fallback
		}
		w.Header().Set("Content-Type", contentType)
		w.Write(content)
	}
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

// Context keys for storing values in context.Context
type contextKey uint8

const (
	dbKey contextKey = iota
	// gaKey
)

func contextWithGlobals(ctx context.Context, db *sql.DB, auth Auth) context.Context {
	ctx = context.WithValue(ctx, dbKey, db)
	ctx = auth.SetAuthContext(ctx, auth) // Use the Auth interface's SetAuthContext method
	return ctx
}

func makeContextMiddleware(db *sql.DB, auth Auth) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r = r.WithContext(contextWithGlobals(r.Context(), db, auth))

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

// getDbFromContext retrieves the *sql.DB instance from the given context.Context.
func getDbFromContext(ctx context.Context) (*sql.DB, error) {
	db, ok := ctx.Value(dbKey).(*sql.DB)
	if !ok {
		return nil, fmt.Errorf("database connection not found in context")
	}
	return db, nil
}

func getAuth(w http.ResponseWriter, r *http.Request) Auth {
	auth, ok := r.Context().Value(authContextKey).(Auth)
	if !ok {
		http.Error(w, "Internal Server Error: Auth interface missing.", http.StatusInternalServerError)
		runtime.Goexit()
	}
	return auth
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
