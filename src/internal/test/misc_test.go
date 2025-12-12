package test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	. "github.com/lifthrasiir/angel/gemini"
	"github.com/lifthrasiir/angel/internal/database"
	"github.com/lifthrasiir/angel/internal/llm"
	"github.com/lifthrasiir/angel/internal/server"
	. "github.com/lifthrasiir/angel/internal/types"
)

// TestCountTokensHandler tests the countTokensHandler function
func TestCountTokensHandler(t *testing.T) {
	router, _, models := setupTest(t)

	// Mock the CountTokens method of CurrentProvider
	provider := models.GetProvider(DefaultGeminiModel)
	mockLLMProvider := provider.(*llm.MockLLMProvider)
	mockLLMProvider.CountTokensFunc = func(ctx context.Context, modelName string, contents []Content) (*CaCountTokenResponse, error) {
		// Simulate token counting based on input text length
		totalTokens := len(contents[0].Parts[0].Text) / 2 // Example: 2 chars per token
		return &CaCountTokenResponse{TotalTokens: totalTokens}, nil
	}

	// Test case 1: Successful token count
	t.Run("Success", func(t *testing.T) {
		payload := []byte(`{"text": "This is a test string for token counting."}`)
		rr := testRequest(t, router, "POST", "/api/countTokens", payload, http.StatusOK)

		var response map[string]int
		err := json.Unmarshal(rr.Body.Bytes(), &response)
		if err != nil {
			t.Fatalf("could not unmarshal response: %v", err)
		}

		expectedTokens := len("This is a test string for token counting.") / 2
		if response["totalTokens"] != expectedTokens {
			t.Errorf("expected %d tokens, got %d", expectedTokens, response["totalTokens"])
		}
	})

	// Test case 2: Invalid JSON payload
	t.Run("Invalid JSON", func(t *testing.T) {
		payload := []byte(`{"text": "This is a test string for token counting."`) // Malformed JSON
		testRequest(t, router, "POST", "/api/countTokens", payload, http.StatusBadRequest)
	})

	// Test case 3: Authentication failure
	t.Run("Authentication Failure", func(t *testing.T) {
		// Temporarily set CurrentProvider to nil to simulate uninitialized client
		models.SetGeminiProvider(&llm.MockLLMProvider{
			CountTokensFunc: func(ctx context.Context, modelName string, contents []Content) (*CaCountTokenResponse, error) {
				return nil, &APIError{StatusCode: http.StatusUnauthorized, Message: "Authentication failed"}
			},
		})

		payload := []byte(`{"text": "Some text"}`)
		testRequest(t, router, "POST", "/api/countTokens", payload, http.StatusUnauthorized)
	})
}

// TestHandleEvaluatePrompt tests the handleEvaluatePrompt function
func TestHandleEvaluatePrompt(t *testing.T) {
	router, _, _ := setupTest(t)

	// Test case 1: Successful template evaluation
	t.Run("Success", func(t *testing.T) {
		payload := []byte(`{"template": "{{.Builtin.SystemPromptForCoding}}"}`)
		rr := testRequest(t, router, "POST", "/api/evaluatePrompt", payload, http.StatusOK)

		var response map[string]string
		err := json.Unmarshal(rr.Body.Bytes(), &response)
		if err != nil {
			t.Fatalf("could not unmarshal response: %v", err)
		}

		const expectedPrefix = "You are an interactive agent specializing in software engineering tasks."
		if !strings.HasPrefix(response["evaluatedPrompt"], expectedPrefix) {
			t.Errorf("expected prefix %q, got %q", expectedPrefix, response["evaluatedPrompt"])
		}
	})

	// Test case 2: Invalid JSON payload
	t.Run("Invalid JSON", func(t *testing.T) {
		payload := []byte(`{"template": "Hello, {{.Name}}!"`) // Malformed JSON
		testRequest(t, router, "POST", "/api/evaluatePrompt", payload, http.StatusBadRequest)
	})
}

// TestGetMCPConfigsHandler tests the getMCPConfigsHandler function
func TestGetMCPConfigsHandler(t *testing.T) {
	router, testDB, _ := setupTest(t)

	// Prepare some MCP configs in the DB
	database.SaveMCPServerConfig(testDB, MCPServerConfig{Name: "mcp1", ConfigJSON: json.RawMessage(`{}`), Enabled: true})
	database.SaveMCPServerConfig(testDB, MCPServerConfig{Name: "mcp2", ConfigJSON: json.RawMessage(`{}`), Enabled: false})

	rr := testRequest(t, router, "GET", "/api/mcp/configs", nil, http.StatusOK)

	var configs []server.FrontendMCPConfig
	err := json.Unmarshal(rr.Body.Bytes(), &configs)
	if err != nil {
		t.Fatalf("could not unmarshal response: %v", err)
	}

	if len(configs) != 2 {
		t.Errorf("expected 2 configs, got %d", len(configs))
	}

	// Check if the configs are present
	foundMcp1 := false
	foundMcp2 := false
	for _, cfg := range configs {
		if cfg.Name == "mcp1" {
			foundMcp1 = true
			if !cfg.Enabled {
				t.Errorf("mcp1 should be enabled")
			}
		}
		if cfg.Name == "mcp2" {
			foundMcp2 = true
			if cfg.Enabled {
				t.Errorf("mcp2 should be disabled")
			}
		}
	}

	if !foundMcp1 || !foundMcp2 {
		t.Errorf("expected MCP configs not found in response")
	}
}

// TestSaveMCPConfigHandler tests the saveMCPConfigHandler function
func TestSaveMCPConfigHandler(t *testing.T) {
	router, testDB, _ := setupTest(t)

	// Test case 1: Successful creation/update
	t.Run("Success", func(t *testing.T) {
		payload := []byte(`{"name": "new-mcp", "config_json": "{}", "enabled": true}`)
		rr := testRequest(t, router, "POST", "/api/mcp/configs", payload, http.StatusOK)

		var response MCPServerConfig
		err := json.Unmarshal(rr.Body.Bytes(), &response)
		if err != nil {
			t.Fatalf("could not unmarshal response: %v", err)
		}
		if response.Name != "new-mcp" || !response.Enabled {
			t.Errorf("unexpected response: %+v", response)
		}

		// Verify in DB
		var name string
		var enabled bool
		querySingleRow(t, testDB, "SELECT name, enabled FROM mcp_configs WHERE name = ? ", []interface{}{"new-mcp"}, &name, &enabled)
		if name != "new-mcp" || !enabled {
			t.Errorf("MCP config in DB mismatch: name=%v, enabled=%v", name, enabled)
		}
	})

	// Test case 2: Invalid JSON payload
	t.Run("Invalid JSON", func(t *testing.T) {
		payload := []byte(`{"name": "new-mcp", "config_json": "{}", "enabled": true`) // Malformed JSON
		testRequest(t, router, "POST", "/api/mcp/configs", payload, http.StatusBadRequest)
	})
}

// TestDeleteMCPConfigHandler tests the deleteMCPConfigHandler function
func TestDeleteMCPConfigHandler(t *testing.T) {
	router, testDB, _ := setupTest(t)

	// Prepare an MCP config in the DB
	database.SaveMCPServerConfig(testDB, MCPServerConfig{Name: "mcp-to-delete", ConfigJSON: json.RawMessage(`{}`), Enabled: true})

	// Test case 1: Successful deletion
	t.Run("Success", func(t *testing.T) {
		rr := testRequest(t, router, "DELETE", "/api/mcp/configs/mcp-to-delete", nil, http.StatusOK)

		var response map[string]string
		err := json.Unmarshal(rr.Body.Bytes(), &response)
		if err != nil {
			t.Fatalf("could not unmarshal response: %v", err)
		}
		if response["status"] != "success" {
			t.Errorf("expected status 'success', got %v", response["status"])
		}

		// Verify in DB
		var count int
		querySingleRow(t, testDB, "SELECT COUNT(*) FROM mcp_configs WHERE name = ?", []interface{}{"mcp-to-delete"}, &count)
		if count != 0 {
			t.Errorf("MCP config not deleted from DB")
		}
	})

	// Test case 2: Config not found
	t.Run("Not Found", func(t *testing.T) {
		testRequest(t, router, "DELETE", "/api/mcp/configs/non-existent-mcp", nil, http.StatusInternalServerError)
	})

}
