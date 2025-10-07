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
	"github.com/gorilla/csrf"
	"github.com/gorilla/mux"

	fsPkg "github.com/lifthrasiir/angel/fs"
)

const dbPath = "angel.db"

func main() {
	checkNetworkFilesystem(dbPath)

	db, err := InitDB(dbPath)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	// Start the shell command manager
	StartShellCommandManager(db) // Pass the database connection

	// Initialize blob reference counts for existing data (first-time setup)
	log.Println("Initializing blob reference counts...")
	err = InitializeBlobRefCounts(db)
	if err != nil {
		log.Printf("Warning: Failed to initialize blob reference counts: %v", err)
	} else {
		log.Println("Blob reference counting initialized successfully")
	}

	// Retrieve or generate CSRF key
	csrfKey, err := GetAppConfig(db, CSRFKeyName)
	if err != nil {
		log.Fatalf("Failed to retrieve CSRF key from DB: %v", err)
	}
	if csrfKey == nil {
		csrfKey = make([]byte, 32)
		if _, err := rand.Read(csrfKey); err != nil {
			log.Fatalf("Failed to generate CSRF key: %v", err)
		}
		if err := SetAppConfig(db, CSRFKeyName, csrfKey); err != nil {
			log.Fatalf("Failed to save CSRF key to DB: %v", err)
		}
		log.Println("Generated and saved new CSRF key.")
	} else {
		log.Println("Loaded CSRF key from DB.")
	}

	InitMCPManager(db)

	ga := NewGeminiAuth(db)
	ga.Init()

	// Add angel-eval provider after default models are initialized
	CurrentProviders["angel-eval"] = &AngelEvalProvider{}

	router := mux.NewRouter()
	router.Use(makeContextMiddleware(db, ga))

	// Apply CSRF middleware.
	// For production, ensure csrf.Secure(true) is used with HTTPS.
	// csrf.SameSite(csrf.SameSiteStrictMode) is recommended for strong protection.
	csrfMiddleware := csrf.Protect(
		csrfKey,
		csrf.Secure(false), // Required because we are typically working on localhost
		csrf.HttpOnly(true),
		csrf.SameSite(csrf.SameSiteStrictMode),
		csrf.CookieName("_csrf"),
		csrf.Path("/"),
	)
	router.Use(csrfMiddleware)

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

func checkNetworkFilesystem(dbPath string) {
	absDBPath, err := filepath.Abs(dbPath)
	if err != nil {
		log.Fatalf("Failed to get absolute path for angel.db: %v", err)
	}

	isNetwork, fsType, err := fsPkg.IsNetworkFilesystem(absDBPath)
	if err != nil {
		log.Printf("Warning: Could not determine if angel.db is on a network filesystem: %v", err)
	} else if isNetwork {
		if fsType != "" {
			fsType = fmt.Sprintf(" (%s)", fsType)
		}
		log.Fatalf("ERROR: angel.db is located on a network filesystem%s. This is not supported due to potential performance and data corruption issues. Please move angel.db to a local drive.", fsType)
	}
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
	router.HandleFunc("/api/chat/new/envChanged", calculateNewSessionEnvChangedHandler).Methods("GET")
	router.HandleFunc("/api/chat/{sessionId}", chatMessage).Methods("POST")
	router.HandleFunc("/api/chat/{sessionId}", loadChatSession).Methods("GET")
	router.HandleFunc("/api/chat/{sessionId}/name", updateSessionNameHandler).Methods("POST")
	router.HandleFunc("/api/chat/{sessionId}/roots", updateSessionRootsHandler).Methods("POST")
	router.HandleFunc("/api/chat/{sessionId}/call", handleCall).Methods("GET", "DELETE")
	router.HandleFunc("/api/chat/{sessionId}", deleteSession).Methods("DELETE")
	router.HandleFunc("/api/chat/{sessionId}/branch", createBranchHandler).Methods("POST")
	router.HandleFunc("/api/chat/{sessionId}/branch", switchBranchHandler).Methods("PUT")
	router.HandleFunc("/api/chat/{sessionId}/branch/{branchId}/confirm", confirmBranchHandler).Methods("POST")
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
		sendInternalServerError(w, r, err, "Error creating sub-filesystem for assets")
		return
	}
	http.FileServer(http.FS(fsys)).ServeHTTP(w, r)
}

// serveSPAIndex serves the index.html file for SPA fallback
func serveSPAIndex(w http.ResponseWriter, r *http.Request) {
	// Try to serve from filesystem first (for development)
	fsPath := filepath.Join("frontend", "dist", "index.html")

	var content []byte
	var err error

	if _, err = os.Stat(fsPath); err == nil {
		content, err = os.ReadFile(fsPath)
		if err != nil {
			sendInternalServerError(w, r, err, "Error reading index.html from filesystem")
			return
		}
	} else {
		// If not found on filesystem, try to serve from embedded files
		file, openErr := embeddedFiles.Open("frontend/dist/index.html")
		if openErr != nil {
			sendInternalServerError(w, r, openErr, "Error opening embedded index.html")
			return
		}
		defer file.Close()

		content, err = io.ReadAll(file)
		if err != nil {
			sendInternalServerError(w, r, err, "Error reading embedded index.html")
			return
		}
	}

	// Inject CSRF token into the HTML
	csrfToken := csrf.Token(r)
	// Find the closing </head> tag and insert the meta tag before it
	headEndTag := "</head>"
	metaTag := fmt.Sprintf("<meta name=\"csrf-token\" content=\"%s\">", csrfToken)
	modifiedContent := strings.Replace(string(content), headEndTag, metaTag+"\n"+headEndTag, 1)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(modifiedContent))
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
			sendInternalServerError(w, r, err, fmt.Sprintf("Error opening embedded file %s", filePath))
			return
		}
		defer file.Close()

		content, err := io.ReadAll(file)
		if err != nil {
			sendInternalServerError(w, r, err, fmt.Sprintf("Error reading embedded file %s", filePath))
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
func decodeJSONRequest(r *http.Request, w http.ResponseWriter, target interface{}, _ string) bool {
	if err := json.NewDecoder(r.Body).Decode(target); err != nil {
		// Check if it's an EOF error, which happens with empty body
		if err == io.EOF || err.Error() == "unexpected EOF" {
			sendBadRequestError(w, r, "Empty request body")
		} else {
			sendBadRequestError(w, r, "Invalid request body")
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
			r = csrf.PlaintextHTTPRequest(r) // Required because we are typically working on localhost

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
