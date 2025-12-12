package test

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io"
	"iter"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	. "github.com/lifthrasiir/angel/gemini"
	"github.com/lifthrasiir/angel/internal/database"
	"github.com/lifthrasiir/angel/internal/llm"
	"github.com/lifthrasiir/angel/internal/server"
	"github.com/lifthrasiir/angel/internal/tool"
	. "github.com/lifthrasiir/angel/internal/types"
)

// Helper function to set up the test environment
func setupTest(t *testing.T) (*mux.Router, *sql.DB, *llm.Models) {
	// Initialize an in-memory database for testing with unique name
	testDB, err := database.InitTestDB(t.Name())
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}

	// Verify that all required tables exist
	requiredTables := []string{"sessions", "messages", "oauth_tokens", "workspaces", "mcp_configs", "branches"}
	for _, tableName := range requiredTables {
		_, err = testDB.Exec(fmt.Sprintf("SELECT 1 FROM %s LIMIT 1", tableName))
		if err != nil {
			t.Fatalf("%s table does not exist after InitDB: %v", tableName, err)
		}
	}

	// Initialize tools registry
	tools := tool.NewTools()
	server.InitTools(tools)
	tools.InitMCPManager(testDB)

	// Initialize Models by loading models.json
	modelsData, err := os.ReadFile("../../../models.json")
	if err != nil {
		t.Fatalf("Failed to read models.json: %v", err)
	}

	models, err := llm.LoadModels(modelsData)
	if err != nil {
		t.Fatalf("Failed to load models: %v", err)
	}

	// Override CurrentProvider with MockLLMProvider for testing
	mockLLMProvider := &llm.MockLLMProvider{
		SendMessageStreamFunc: func(ctx context.Context, modelName string, params llm.SessionParams) (iter.Seq[GenerateContentResponse], io.Closer, error) {
			// Default mock implementation: return an empty sequence
			return iter.Seq[GenerateContentResponse](func(yield func(GenerateContentResponse) bool) {}), io.NopCloser(nil), nil
		},
		GenerateContentOneShotFunc: func(ctx context.Context, modelName string, params llm.SessionParams) (llm.OneShotResult, error) {
			return llm.OneShotResult{Text: "Mocked one-shot response"}, nil
		},
		CountTokensFunc: func(ctx context.Context, modelName string, contents []Content) (*CaCountTokenResponse, error) {
			return &CaCountTokenResponse{TotalTokens: 10}, nil
		},
		MaxTokensFunc: func(modelName string) int {
			return 1048576 // Mocked max tokens
		},
	}
	models.SetGeminiProvider(mockLLMProvider)

	// Initialize GeminiAuth
	geminiAuth := llm.NewGeminiAuth("")

	// Create a new router for testing
	router := mux.NewRouter()
	router.Use(server.MakeContextMiddleware(testDB, models, geminiAuth, tools))
	server.InitRouter(router, embed.FS{})

	// Ensure the database connection is closed after the test
	t.Cleanup(func() {
		if testDB != nil {
			testDB.Close()
		}
	})

	return router, testDB, models
}

type Sse struct {
	Type    EventType
	Payload string
}

// parseSseStream parses an HTTP response body as an SSE stream.
// Assumes that the SSE stream itself is correct. Do not fix this!
func parseSseStream(t *testing.T, resp *http.Response) iter.Seq[Sse] {
	return func(yield func(Sse) bool) {
		scanner := bufio.NewScanner(resp.Body)
		var buffer string
		for scanner.Scan() {
			line := scanner.Text()
			buffer += line + "\n" // Add newline back as scanner consumes it

			// Check for end of event (double newline)
			if strings.HasSuffix(buffer, "\n\n") {
				content := strings.ReplaceAll(buffer[6:len(buffer)-2], "\ndata: ", "\n")
				header, payload, _ := strings.Cut(content, "\n")

				// The first character of the header is the EventType
				eventType := EventType(rune(header[0]))
				log.Printf("parseSseStream(%p): type=%c payload=[%s]", resp, eventType, strings.ReplaceAll(payload, "\n", "|"))

				if !yield(Sse{Type: eventType, Payload: payload}) {
					return
				}
				buffer = "" // Reset buffer for the next event
			}
		}
		if err := scanner.Err(); err != nil {
			t.Fatalf("scanner error: %v", err)
		}
	}
}

// testRequest sends an HTTP request and checks the status code.
func testRequest(t *testing.T, router *mux.Router, method, url string, payload []byte, expectedStatus int) *httptest.ResponseRecorder {
	var req *http.Request
	var err error
	if payload != nil {
		req, err = http.NewRequest(method, url, bytes.NewBuffer(payload))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req, err = http.NewRequest(method, url, nil)
		if strings.HasPrefix(url, "/api/chat/") {
			req.Header.Set("Accept", "text/event-stream") // SSE requested
		}
	}
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if status := rr.Code; status != expectedStatus {
		t.Fatalf("handler returned wrong status code: got %v want %v", status, expectedStatus)
	}
	return rr
}

// testStreamingRequest sends an HTTP request to a test server and returns the response.
// This is specifically for streaming responses (SSE).
func testStreamingRequest(t *testing.T, router *mux.Router, method, url string, payload []byte, expectedStatus int) *http.Response {
	// Create a test server
	ts := httptest.NewServer(router)
	t.Cleanup(func() {
		ts.Close()
	})

	var req *http.Request
	var err error
	if payload != nil {
		req, err = http.NewRequest(method, ts.URL+url, bytes.NewBuffer(payload))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req, err = http.NewRequest(method, ts.URL+url, nil)
		req.Header.Set("Accept", "text/event-stream") // SSE requested
	}
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to send request to test server: %v", err)
	}

	if status := resp.StatusCode; status != expectedStatus {
		t.Fatalf("handler returned wrong status code: got %v want %v", status, expectedStatus)
	}
	return resp
}

// querySingleRow executes a query that is expected to return a single row and handles errors.
func querySingleRow(t *testing.T, db *sql.DB, query string, args []interface{}, dest ...interface{}) {
	row := db.QueryRow(query, args...)
	err := row.Scan(dest...)
	if err != nil {
		t.Fatalf("Failed to query single row: %v (query: %s, args: %v)", err, query, args)
	}
}
