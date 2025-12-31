package server

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/csrf"
	"github.com/gorilla/mux"

	"github.com/lifthrasiir/angel/filesystem"
	"github.com/lifthrasiir/angel/internal/database"
	"github.com/lifthrasiir/angel/internal/env"
	"github.com/lifthrasiir/angel/internal/llm"
	"github.com/lifthrasiir/angel/internal/tool"
	"github.com/lifthrasiir/angel/internal/tool/file"
	"github.com/lifthrasiir/angel/internal/tool/search_chat"
	"github.com/lifthrasiir/angel/internal/tool/shell"
	"github.com/lifthrasiir/angel/internal/tool/subagent"
	"github.com/lifthrasiir/angel/internal/tool/todo"
	"github.com/lifthrasiir/angel/internal/tool/webfetch"
	. "github.com/lifthrasiir/angel/internal/types"
)

// InitTools initializes all built-in tools
func InitTools(tools *tool.Tools) {
	tools.Register(file.AllTools...)
	tools.Register(search_chat.AllTools...)
	tools.Register(shell.AllTools...)
	tools.Register(subagent.AllTools...)
	tools.Register(todo.AllTools...)
	tools.Register(webfetch.AllTools...)
}

// getExecutableName returns the appropriate executable name for the current platform
func getExecutableName() string {
	if runtime.GOOS == "windows" {
		return "angel.exe"
	}
	return "./angel"
}

func Main(config *env.EnvConfig, embeddedFiles embed.FS, loginUnavailableHTML []byte, modelsJSON []byte) {
	// Initialize tools registry
	tools := tool.NewTools()
	InitTools(tools)

	models, err := llm.LoadModels(modelsJSON)
	if err != nil {
		log.Fatalf("Failed to load models.json: %v", err)
	}

	// Parse port from command line argument (default: 8080)
	port := 8080
	if len(os.Args) > 1 {
		if parsedPort, err := strconv.Atoi(os.Args[1]); err == nil && parsedPort > 0 && parsedPort <= 65535 {
			port = parsedPort
		} else {
			log.Fatalf("Invalid port number: %s. Please provide a valid port (1-65535).", os.Args[1])
		}
	}

	checkNetworkFilesystem(config.DBPath())

	ctx := env.ContextWithEnvConfig(context.Background(), config)
	db, err := database.InitDB(ctx, config.DBPath())
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	// Start the shell command manager
	shell.StartShellCommandManager(db) // Pass the database connection

	jobs := []HousekeepingJob{
		database.Job(db),
		&tempSessionCleanupJob{
			db:             db,
			olderThan:      48 * time.Hour,
			sandboxBaseDir: config.SessionDir(),
		},
	}

	StartHousekeepingJobs(jobs)

	// Retrieve or generate CSRF key
	csrfKey, err := database.GetAppConfig(db, database.CSRFKeyName)
	if err != nil {
		log.Fatalf("Failed to retrieve CSRF key from DB: %v", err)
	}
	if csrfKey == nil {
		csrfKey = make([]byte, 32)
		if _, err := rand.Read(csrfKey); err != nil {
			log.Fatalf("Failed to generate CSRF key: %v", err)
		}
		if err := database.SetAppConfig(db, database.CSRFKeyName, csrfKey); err != nil {
			log.Fatalf("Failed to save CSRF key to DB: %v", err)
		}
		log.Println("Generated and saved new CSRF key.")
	} else {
		log.Println("Loaded CSRF key from DB.")
	}

	// Initialize MCP connections
	tools.InitMCPManager(db)

	geminiAuth := llm.NewGeminiAuth("http://localhost:8080/oauth2callback")

	// Initialize OpenAI endpoints from database configurations
	models.InitializeOpenAIEndpoints(db)

	// Add angel-eval provider after all other providers are initialized
	models.SetAngelEvalProvider(&llm.AngelEvalProvider{})

	router := mux.NewRouter()
	router.Use(MakeContextMiddleware(db, models, geminiAuth, tools, config))

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

	// OAuth2 handler is only active on default port 8080
	if port == 8080 {
		router.HandleFunc("/login", authHandler).Methods("GET")
		router.HandleFunc("/oauth2callback", authCallbackHandler).Methods("GET")
	} else {
		// Add /login handler that shows error message when not on port 8080
		router.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusServiceUnavailable)

			// Read and modify the embedded HTML template
			htmlContent := string(loginUnavailableHTML)
			htmlContent = strings.ReplaceAll(htmlContent, "{{PORT}}", fmt.Sprintf("%d", port))
			htmlContent = strings.ReplaceAll(htmlContent, "{{EXECUTABLE}}", getExecutableName())

			w.Write([]byte(htmlContent))
		}).Methods("GET")

		log.Printf("WARNING: OAuth2 login is disabled when running on port %d.", port)
		log.Printf("OAuth2 callback URL is hardcoded to http://localhost:8080/oauth2callback and cannot be changed.")
		log.Printf("To use login functionality, please run the server on port 8080 or authenticate first on port 8080 and then copy the configuration.")
	}
	router.HandleFunc("/api/logout", logoutHandler).Methods("POST")

	InitRouter(router, embeddedFiles)

	// Setup graceful shutdown
	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: router,
	}

	// Start server in a goroutine
	go func() {
		fmt.Printf("Server started at http://localhost:%d\n", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed to start: %v", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	StopHousekeepingJobs()

	// Shutdown server with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited")
}

func checkNetworkFilesystem(dbPath string) {
	absDBPath, err := filepath.Abs(dbPath)
	if err != nil {
		log.Fatalf("Failed to get absolute path for angel.db: %v", err)
	}

	isNetwork, fsType, err := filesystem.IsNetworkFilesystem(absDBPath)
	if err != nil {
		log.Printf("Warning: Could not determine if angel.db is on a network filesystem: %v", err)
	} else if isNetwork {
		if fsType != "" {
			fsType = fmt.Sprintf(" (%s)", fsType)
		}
		log.Fatalf("ERROR: angel.db is located on a network filesystem%s. This is not supported due to potential performance and data corruption issues. Please move angel.db to a local drive.", fsType)
	}
}

func InitRouter(router *mux.Router, embeddedFiles embed.FS) {
	serveSPAIndex := func(w http.ResponseWriter, r *http.Request) {
		serveSPAIndex(embeddedFiles, w, r)
	}
	serveStaticFiles := func(w http.ResponseWriter, r *http.Request) {
		serveStaticFiles(embeddedFiles, w, r)
	}
	handleSessionPage := func(w http.ResponseWriter, r *http.Request) {
		handleSessionPage(embeddedFiles, w, r)
	}

	// Explicitly handle root-level static files
	router.HandleFunc("/favicon.ico", serveFile(embeddedFiles, "favicon.ico")).Methods("GET")
	router.HandleFunc("/manifest.webmanifest", serveFile(embeddedFiles, "manifest.webmanifest")).Methods("GET")
	router.HandleFunc("/angel-logo-colored.svg", serveFile(embeddedFiles, "angel-logo-colored.svg")).Methods("GET")

	router.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/new", http.StatusFound)
	}).Methods("GET")

	router.HandleFunc("/new", serveSPAIndex).Methods("GET")
	router.HandleFunc("/temp", serveSPAIndex).Methods("GET")
	router.HandleFunc("/search", serveSPAIndex).Methods("GET")
	router.HandleFunc("/settings", serveSPAIndex).Methods("GET")

	router.HandleFunc("/w", handleNotFound).Methods("GET")
	router.HandleFunc("/w/new", serveSPAIndex).Methods("GET")
	router.HandleFunc("/w/{workspaceId}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, fmt.Sprintf("/w/%s/new", mux.Vars(r)["workspaceId"]), http.StatusFound)
	}).Methods("GET")
	router.HandleFunc("/w/{workspaceId}/new", serveSPAIndex).Methods("GET")
	router.HandleFunc("/w/{workspaceId}/temp", serveSPAIndex).Methods("GET")
	router.HandleFunc("/w/{workspaceId}/{sessionId}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, fmt.Sprintf("/%s", mux.Vars(r)["sessionId"]), http.StatusFound)
	}).Methods("GET")

	// API handlers
	router.HandleFunc("/api/workspaces", createWorkspaceHandler).Methods("POST")
	router.HandleFunc("/api/workspaces", listWorkspacesHandler).Methods("GET")
	router.HandleFunc("/api/workspaces/{id}", deleteWorkspaceHandler).Methods("DELETE")

	router.HandleFunc("/api/chat", listSessionsByWorkspaceHandler).Methods("GET")
	router.HandleFunc("/api/chat", newSessionAndMessageHandler).Methods("POST")
	router.HandleFunc("/api/chat/temp", newTempSessionAndMessageHandler).Methods("POST")
	router.HandleFunc("/api/chat/new/envChanged", calculateNewSessionEnvChangedHandler).Methods("GET")
	router.HandleFunc("/api/chat/{sessionId}", chatMessageHandler).Methods("POST")
	router.HandleFunc("/api/chat/{sessionId}", loadChatSessionHandler).Methods("GET")
	router.HandleFunc("/api/chat/{sessionId}/name", updateSessionNameHandler).Methods("POST")
	router.HandleFunc("/api/chat/{sessionId}/workspace", updateSessionWorkspaceHandler).Methods("POST")
	router.HandleFunc("/api/chat/{sessionId}/roots", updateSessionRootsHandler).Methods("POST")
	router.HandleFunc("/api/chat/{sessionId}/call", handleCall).Methods("GET", "DELETE")
	router.HandleFunc("/api/chat/{sessionId}", deleteSessionHandler).Methods("DELETE")
	router.HandleFunc("/api/chat/{sessionId}/branch", createBranchHandler).Methods("POST")
	router.HandleFunc("/api/chat/{sessionId}/branch", switchBranchHandler).Methods("PUT")
	router.HandleFunc("/api/chat/{sessionId}/branch/{branchId}/confirm", confirmBranchHandler).Methods("POST")
	router.HandleFunc("/api/chat/{sessionId}/branch/{branchId}/retry-error", retryErrorBranchHandler).Methods("POST")
	router.HandleFunc("/api/chat/{sessionId}/compress", compressSessionHandler).Methods("POST")
	router.HandleFunc("/api/chat/{sessionId}/extract", extractSessionHandler).Methods("POST")
	router.HandleFunc("/api/chat/{sessionId}/command", commandHandler).Methods("POST")

	router.HandleFunc("/api/accounts", listAccountsHandler).Methods("GET")
	router.HandleFunc("/api/accounts/{id}/details", getAccountDetailsHandler).Methods("GET")
	router.HandleFunc("/api/countTokens", countTokensHandler).Methods("POST")
	router.HandleFunc("/api/evaluatePrompt", handleEvaluatePrompt).Methods("POST")
	router.HandleFunc("/api/mcp/configs", getMCPConfigsHandler).Methods("GET")
	router.HandleFunc("/api/mcp/configs", saveMCPConfigHandler).Methods("POST")
	router.HandleFunc("/api/mcp/configs/{name}", deleteMCPConfigHandler).Methods("DELETE")
	router.HandleFunc("/api/models", listModelsHandler).Methods("GET")
	router.HandleFunc("/api/systemPrompts", getSystemPromptsHandler).Methods("GET")
	router.HandleFunc("/api/systemPrompts", saveSystemPromptsHandler).Methods("PUT")
	router.HandleFunc("/api/search", searchMessagesHandler).Methods("POST")

	// OpenAI configuration endpoints
	router.HandleFunc("/api/openai-configs", getOpenAIConfigsHandler).Methods("GET")
	router.HandleFunc("/api/openai-configs", saveOpenAIConfigHandler).Methods("POST")
	router.HandleFunc("/api/openai-configs/{id}", deleteOpenAIConfigHandler).Methods("DELETE")
	router.HandleFunc("/api/openai-configs/{id}/models", getOpenAIModelsHandler).Methods("GET")
	router.HandleFunc("/api/openai-configs/{id}/models/refresh", refreshOpenAIModelsHandler).Methods("POST")

	// Gemini API configuration endpoints
	router.HandleFunc("/api/gemini-api-configs", getGeminiAPIConfigsHandler).Methods("GET")
	router.HandleFunc("/api/gemini-api-configs", saveGeminiAPIConfigHandler).Methods("POST")
	router.HandleFunc("/api/gemini-api-configs/{id}", deleteGeminiAPIConfigHandler).Methods("DELETE")

	router.HandleFunc("/api/ui/directory", handleDirectoryNavigation).Methods("GET")
	router.HandleFunc("/api/ui/directory", handlePickDirectory).Methods("POST")
	router.HandleFunc("/api", handleNotFound)

	router.PathPrefix("/assets/").HandlerFunc(serveStaticFiles)
	router.PathPrefix("/sourcemaps/").HandlerFunc(serveSourcemapFiles)

	router.HandleFunc("/{sessionId}/@{blobHash}", handleDownloadBlobByHash).Methods("GET")
	router.HandleFunc("/{sessionId}", handleSessionPage).Methods("GET")
}

// serveStaticFiles serves static files from the filesystem first, then from embedded files
func serveStaticFiles(embeddedFiles embed.FS, w http.ResponseWriter, r *http.Request) {
	// Strip the /assets/ prefix for serving from frontend/dist/assets
	assetPath := strings.TrimPrefix(r.URL.Path, "/assets/")
	fsPath := filepath.Join("frontend", "dist", "assets", assetPath)

	// Check if the requested path is for a file that exists on disk
	if _, err := os.Stat(fsPath); err == nil {
		http.ServeFile(w, r, fsPath)
		return
	}

	// If not found on filesystem, try to serve from embedded files
	fsys, err := fs.Sub(embeddedFiles, "frontend/dist")
	if err != nil {
		sendInternalServerError(w, r, err, "Error creating sub-filesystem for assets")
		return
	}
	http.FileServer(http.FS(fsys)).ServeHTTP(w, r)
}

// serveSourcemapFiles serves sourcemap files from the frontend/sourcemaps directory
func serveSourcemapFiles(w http.ResponseWriter, r *http.Request) {
	// Strip the /sourcemaps/ prefix for serving from frontend/sourcemaps
	sourcemapPath := strings.TrimPrefix(r.URL.Path, "/sourcemaps/")
	fsPath := filepath.Join("frontend", "sourcemaps", sourcemapPath)

	// Check if the requested path is for a file that exists on disk
	if _, err := os.Stat(fsPath); err == nil {
		http.ServeFile(w, r, fsPath)
		return
	}

	// If not found on filesystem, return 404 (sourcemaps are not embedded)
	http.NotFound(w, r)
}

// serveSPAIndex serves the index.html file for SPA fallback
func serveSPAIndex(embeddedFiles embed.FS, w http.ResponseWriter, r *http.Request) {
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
func serveFile(embeddedFiles embed.FS, filePath string) http.HandlerFunc {
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

func contextWithGlobals(
	ctx context.Context,
	db *database.Database,
	models *llm.Models,
	ga *llm.GeminiAuth,
	tools *tool.Tools,
	config *env.EnvConfig,
) context.Context {
	ctx = database.ContextWith(ctx, db)
	ctx = llm.ContextWithModels(ctx, models)
	ctx = llm.ContextWithGeminiAuth(ctx, ga)
	ctx = tool.ContextWith(ctx, tools)
	ctx = env.ContextWithEnvConfig(ctx, config)
	return ctx
}

func MakeContextMiddleware(
	db *database.Database,
	models *llm.Models,
	ga *llm.GeminiAuth,
	tools *tool.Tools,
	config *env.EnvConfig,
) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r = r.WithContext(contextWithGlobals(r.Context(), db, models, ga, tools, config))
			r = csrf.PlaintextHTTPRequest(r) // Required because we are typically working on localhost

			next.ServeHTTP(w, r)
		})
	}
}

func getDb(w http.ResponseWriter, r *http.Request) *database.Database {
	db, err := database.FromContext(r.Context())
	if err != nil {
		http.Error(w, "Internal Server Error: Database connection missing.", http.StatusInternalServerError)
		runtime.Goexit()
	}
	return db
}

func getModels(w http.ResponseWriter, r *http.Request) *llm.Models {
	models, err := llm.ModelsFromContext(r.Context())
	if err != nil {
		http.Error(w, "Internal Server Error: Models missing.", http.StatusInternalServerError)
		runtime.Goexit()
	}
	return models
}

func getGeminiAuth(w http.ResponseWriter, r *http.Request) *llm.GeminiAuth {
	ga, err := llm.GeminiAuthFromContext(r.Context())
	if err != nil {
		http.Error(w, "Internal Server Error: GeminiAuth missing.", http.StatusInternalServerError)
		runtime.Goexit()
	}
	return ga
}

func getTools(w http.ResponseWriter, r *http.Request) *tool.Tools {
	tools, err := tool.FromContext(r.Context())
	if err != nil {
		http.Error(w, "Internal Server Error: Tools missing.", http.StatusInternalServerError)
		runtime.Goexit()
	}
	return tools
}

func getEnvConfig(w http.ResponseWriter, r *http.Request) *env.EnvConfig {
	config, err := env.EnvConfigFromContext(r.Context())
	if err != nil {
		http.Error(w, "Internal Server Error: EnvConfig missing.", http.StatusInternalServerError)
		runtime.Goexit()
	}
	return config
}
