package llm

import (
	"os"
	"strings"
	"testing"
)

func TestGeminiSubagentProvider(t *testing.T) {
	// Load actual models.json file
	modelsFile, err := os.ReadFile("../../../models.json")
	if err != nil {
		t.Fatalf("Failed to read models.json: %v", err)
	}

	// Create Models from actual models.json
	models, err := LoadModels(modelsFile)
	if err != nil {
		t.Fatalf("Failed to load models: %v", err)
	}

	// Create CodeAssistProvider with models map
	geminiModels := make(map[string]*Model)
	for _, model := range models.builtinModels {
		if models.isGeminiModelUnsafe(model) {
			geminiModels[model.Name] = model
		}
	}

	models.ResetGeminiProvider()

	// Test 1: session_name task for gemini-2.5-flash
	t.Run("SessionNameTask", func(t *testing.T) {
		modelProvider, err := models.ResolveSubagent("gemini-2.5-flash", "session_name")
		if err != nil {
			t.Fatalf("Failed to resolve subagent: %v", err)
		}

		// Should return gemini-2.5-flash/subagent, which modelName *is* gemini-2.5-flash-lite
		if modelProvider.Name() != "gemini-2.5-flash/subagent" {
			t.Errorf("Expected 'gemini-2.5-flash/subagent', got '%s'", modelProvider.Name())
		}
	})

	// Test 2: image_generation task for gemini-2.5-flash
	t.Run("ImageGenerationTask", func(t *testing.T) {
		modelProvider, err := models.ResolveSubagent("gemini-2.5-flash", "image_generation")
		if err != nil {
			t.Fatalf("Failed to resolve subagent: %v", err)
		}

		// Should return some image model (currently gemini-2.5-flash-image, but may change)
		if !strings.Contains(modelProvider.Name(), "-image") {
			t.Errorf("Expected some image model, got '%s'", modelProvider.Name())
		}
	})

	// Test 3: Non-existent task
	t.Run("NonExistentTask", func(t *testing.T) {
		modelProvider, err := models.ResolveSubagent("gemini-2.5-flash", "non_existent_task")
		if err == nil {
			t.Fatalf("Expected error for non-existent task, got none")
		}

		// Should return nil when task not found
		if modelProvider != nil {
			t.Errorf("Expected nil for non-existent task, got non-nil model provider")
		}
	})

	// Test 4: Non-existent model
	t.Run("NonExistentModel", func(t *testing.T) {
		modelProvider, err := models.ResolveSubagent("non-existent-model", "session_name")
		if err == nil {
			t.Fatalf("Expected error for non-existent model, got none")
		}

		// Should return nil when model not found
		if modelProvider != nil {
			t.Errorf("Expected nil for non-existent model, got non-nil model provider")
		}
	})

	// Test 5: claude-sonnet-4.5 model lookup
	t.Run("ClaudeSonnet45Lookup", func(t *testing.T) {
		// Verify claude-sonnet-4.5 model is loaded in the registry
		model, exists := models.GetModel("claude-sonnet-4.5")
		if !exists {
			t.Fatalf("claude-sonnet-4.5 model not found in registry")
		}

		// Verify the model has the correct properties
		if model.ModelName != "claude-sonnet-4-5" {
			t.Errorf("Expected modelName 'claude-sonnet-4-5', got '%s'", model.ModelName)
		}

		// Verify the model has a provider
		provider := models.GetProvider("claude-sonnet-4.5")
		if provider == nil {
			t.Fatalf("No provider found for claude-sonnet-4.5")
		}

		// Verify the provider returns the correct model name
		if provider.Name() != "claude-sonnet-4.5" {
			t.Errorf("Expected provider name 'claude-sonnet-4.5', got '%s'", provider.Name())
		}

		// Test GetModelProvider method (which internally calls GetProvider)
		modelProvider, err := models.GetModelProvider("claude-sonnet-4.5")
		if err != nil {
			t.Fatalf("GetModelProvider failed for claude-sonnet-4.5: %v", err)
		}

		if modelProvider.Name() != "claude-sonnet-4.5" {
			t.Errorf("Expected model provider name 'claude-sonnet-4.5', got '%s'", modelProvider.Name())
		}
	})

	// Test 6: claude-sonnet-4.5 subagent lookup
	t.Run("ClaudeSonnet45Subagent", func(t *testing.T) {
		// Test subagent resolution for claude-sonnet-4.5
		modelProvider, err := models.ResolveSubagent("claude-sonnet-4.5", "")
		if err != nil {
			t.Fatalf("Failed to resolve subagent for claude-sonnet-4.5: %v", err)
		}

		// Should return claude-sonnet-4.5/subagent
		if modelProvider.Name() != "claude-sonnet-4.5/subagent" {
			t.Errorf("Expected 'claude-sonnet-4.5/subagent', got '%s'", modelProvider.Name())
		}
	})

	// Test 7: Verify MaxTokens method uses models
	t.Run("MaxTokensFromModels", func(t *testing.T) {
		// Get the provider for gemini-2.5-flash
		provider := models.GetProvider("gemini-2.5-flash")
		if provider == nil {
			t.Fatalf("No provider found for gemini-2.5-flash")
		}

		maxTokens := provider.MaxTokens()
		expected := 1048576 // From models.go MaxTokens logic
		if maxTokens != expected {
			t.Errorf("Expected maxTokens %d, got %d", expected, maxTokens)
		}

		// Test claude-sonnet-4.5 max tokens
		claudeProvider := models.GetProvider("claude-sonnet-4.5")
		if claudeProvider == nil {
			t.Fatalf("No provider found for claude-sonnet-4.5")
		}

		claudeTokens := claudeProvider.MaxTokens()
		expectedClaudeTokens := 200000 // From models.json
		if claudeTokens != expectedClaudeTokens {
			t.Errorf("Expected claude-sonnet-4.5 maxTokens %d, got %d", expectedClaudeTokens, claudeTokens)
		}
	})
}
