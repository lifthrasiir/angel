package main

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"iter"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"golang.org/x/oauth2"
)

// Helper function to set up the test environment
func setupTest(t *testing.T) (*mux.Router, *sql.DB, Auth) {
	// Initialize an in-memory database for testing with unique name
	dbName := fmt.Sprintf(":memory:?cache=shared&_txlock=immediate&_foreign_keys=1&_journal_mode=WAL&test=%s", t.Name())
	testDB, err := InitDB(dbName)
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

	InitMCPManager(testDB)

	// Reset GlobalGeminiAuth for each test
	ga := NewGeminiAuth(testDB)
	// Explicitly set auth type and ProjectID for testing
	ga.SelectedAuthType = AuthTypeLoginWithGoogle
	ga.ProjectID = "test-project"

	// Set a dummy token for testing to allow InitCurrentProvider to proceed
	ga.Token = &oauth2.Token{
		AccessToken: "dummy-access-token",
		TokenType:   "Bearer",
		Expiry:      time.Now().Add(time.Hour),
	}

	ga.InitCurrentProvider()

	// Override CurrentProvider with MockLLMProvider for testing
	mockLLMProvider := &MockLLMProvider{
		SendMessageStreamFunc: func(ctx context.Context, params SessionParams) (iter.Seq[CaGenerateContentResponse], io.Closer, error) {
			// Default mock implementation: return an empty sequence
			return iter.Seq[CaGenerateContentResponse](func(yield func(CaGenerateContentResponse) bool) {}), io.NopCloser(nil), nil
		},
		GenerateContentOneShotFunc: func(ctx context.Context, params SessionParams) (string, error) {
			return "Mocked one-shot response", nil
		},
		CountTokensFunc: func(ctx context.Context, contents []Content, modelName string) (*CaCountTokenResponse, error) {
			return &CaCountTokenResponse{TotalTokens: 10}, nil
		},
	}
	CurrentProvider = mockLLMProvider

	// Create a new router for testing
	router := mux.NewRouter()
	router.Use(makeContextMiddleware(testDB, ga))
	InitRouter(router)

	// Ensure the database connection is closed after the test
	t.Cleanup(func() {
		if testDB != nil {
			testDB.Close()
		}
	})

	return router, testDB, ga
}

func replaceProvider(provider LLMProvider) LLMProvider {
	oldProvider := CurrentProvider
	CurrentProvider = provider
	return oldProvider
}

// Dummy oauth2.Token for testing
type oauth2Token struct {
	AccessToken  string
	TokenType    string
	RefreshToken string
	Expiry       time.Time
}

func (t *oauth2Token) Valid() bool {
	return t != nil && t.AccessToken != "" && t.Expiry.After(time.Now())
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
