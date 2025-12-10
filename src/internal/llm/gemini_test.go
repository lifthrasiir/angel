package llm

import (
	"os"
	"strings"
	"testing"
)

func TestGeminiSubagentProvider(t *testing.T) {
	// Load actual models.json file
	modelsFile, err := os.ReadFile("models.json")
	if err != nil {
		t.Fatalf("Failed to read models.json: %v", err)
	}

	// Create ModelsRegistry from actual models.json
	registry, err := LoadModels(modelsFile)
	if err != nil {
		t.Fatalf("Failed to load models: %v", err)
	}

	// Create CodeAssistProvider with models map
	geminiModels := make(map[string]*Model)
	for _, model := range registry.builtinModels {
		if registry.isGeminiModelUnsafe(model) {
			geminiModels[model.Name] = model
		}
	}

	registry.ResetGeminiProvider()

	// Test 1: session_name task for gemini-2.5-flash
	t.Run("SessionNameTask", func(t *testing.T) {
		modelProvider, err := registry.ResolveSubagent("gemini-2.5-flash", "session_name")
		if err != nil {
			t.Fatalf("Failed to resolve subagent: %v", err)
		}

		// Should return gemini-2.5-flash/subagent, which modelName *is* gemini-2.5-flash-lite
		if modelProvider.Name != "gemini-2.5-flash/subagent" {
			t.Errorf("Expected 'gemini-2.5-flash/subagent', got '%s'", modelProvider.Name)
		}

		// Provider should be self
		if modelProvider.LLMProvider != registry.geminiProvider {
			t.Error("Expected modelProvider.provider to be the same as geminiProvider")
		}
	})

	// Test 2: image_generation task for gemini-2.5-flash
	t.Run("ImageGenerationTask", func(t *testing.T) {
		modelProvider, err := registry.ResolveSubagent("gemini-2.5-flash", "image_generation")
		if err != nil {
			t.Fatalf("Failed to resolve subagent: %v", err)
		}

		// Should return some image model (currently gemini-2.5-flash-image, but may change)
		if !strings.Contains(modelProvider.Name, "-image") {
			t.Errorf("Expected some image model, got '%s'", modelProvider.Name)
		}
	})

	// Test 3: Non-existent task
	t.Run("NonExistentTask", func(t *testing.T) {
		modelProvider, err := registry.ResolveSubagent("gemini-2.5-flash", "non_existent_task")
		if err == nil {
			t.Fatalf("Expected error for non-existent task, got none")
		}

		// Should return empty values when task not found
		if modelProvider.Name != "" {
			t.Errorf("Expected empty model name for non-existent task, got '%s'", modelProvider.Name)
		}
	})

	// Test 4: Non-existent model
	t.Run("NonExistentModel", func(t *testing.T) {
		modelProvider, err := registry.ResolveSubagent("non-existent-model", "session_name")
		if err == nil {
			t.Fatalf("Expected error for non-existent model, got none")
		}

		// Should return empty values when model not found
		if modelProvider.Name != "" {
			t.Errorf("Expected empty model name for non-existent model, got '%s'", modelProvider.Name)
		}
	})

	// Test 5: claude-sonnet-4.5 model lookup
	t.Run("ClaudeSonnet45Lookup", func(t *testing.T) {
		// Verify claude-sonnet-4.5 model is loaded in the registry
		model, exists := registry.GetModel("claude-sonnet-4.5")
		if !exists {
			t.Fatalf("claude-sonnet-4.5 model not found in registry")
		}

		// Verify the model has the correct properties
		if model.ModelName != "claude-sonnet-4-5" {
			t.Errorf("Expected modelName 'claude-sonnet-4-5', got '%s'", model.ModelName)
		}

		// Verify the model is handled by GeminiProvider
		provider := registry.GetProvider("claude-sonnet-4.5")
		if provider == nil {
			t.Fatalf("No provider found for claude-sonnet-4.5")
		}

		if provider != registry.geminiProvider {
			t.Error("claude-sonnet-4.5 should be handled by GeminiProvider")
		}

		// Test GetModelProvider method (which internally calls resolveModel)
		modelProvider, err := registry.GetModelProvider("claude-sonnet-4.5")
		if err != nil {
			t.Fatalf("GetModelProvider failed for claude-sonnet-4.5: %v", err)
		}

		if modelProvider.Name != "claude-sonnet-4.5" {
			t.Errorf("Expected model provider name 'claude-sonnet-4.5', got '%s'", modelProvider.Name)
		}

		// Verify the model provider can resolve the model (implicitly testing resolveModel)
		if modelProvider.LLMProvider == nil {
			t.Fatal("GetModelProvider returned nil LLMProvider for claude-sonnet-4.5")
		}
	})

	// Test 6: claude-sonnet-4.5 subagent lookup
	t.Run("ClaudeSonnet45Subagent", func(t *testing.T) {
		// Test subagent resolution for claude-sonnet-4.5
		modelProvider, err := registry.ResolveSubagent("claude-sonnet-4.5", "")
		if err != nil {
			t.Fatalf("Failed to resolve subagent for claude-sonnet-4.5: %v", err)
		}

		// Should return claude-sonnet-4.5/subagent
		if modelProvider.Name != "claude-sonnet-4.5/subagent" {
			t.Errorf("Expected 'claude-sonnet-4.5/subagent', got '%s'", modelProvider.Name)
		}

		// Provider should be the GeminiProvider
		if modelProvider.LLMProvider != registry.geminiProvider {
			t.Error("Expected subagent provider to be GeminiProvider")
		}
	})

	// Test 7: Verify MaxTokens method uses models
	t.Run("MaxTokensFromModels", func(t *testing.T) {
		maxTokens := registry.geminiProvider.MaxTokens("gemini-2.5-flash")
		expected := 1048576 // From models.go MaxTokens logic
		if maxTokens != expected {
			t.Errorf("Expected maxTokens %d, got %d", expected, maxTokens)
		}

		// Test claude-sonnet-4.5 max tokens
		claudeTokens := registry.geminiProvider.MaxTokens("claude-sonnet-4.5")
		expectedClaudeTokens := 200000 // From models.json
		if claudeTokens != expectedClaudeTokens {
			t.Errorf("Expected claude-sonnet-4.5 maxTokens %d, got %d", expectedClaudeTokens, claudeTokens)
		}

		// Test non-existent model (should return default)
		defaultTokens := registry.geminiProvider.MaxTokens("non-existent-model")
		if defaultTokens != 0 {
			t.Errorf("Expected default maxTokens %d, got %d", 0, defaultTokens)
		}
	})
}
