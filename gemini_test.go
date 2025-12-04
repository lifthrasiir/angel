package main

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

	// Test 5: Verify MaxTokens method uses models
	t.Run("MaxTokensFromModels", func(t *testing.T) {
		maxTokens := registry.geminiProvider.MaxTokens("gemini-2.5-flash")
		expected := 1048576 // From models.go MaxTokens logic
		if maxTokens != expected {
			t.Errorf("Expected maxTokens %d, got %d", expected, maxTokens)
		}

		// Test non-existent model (should return default)
		defaultTokens := registry.geminiProvider.MaxTokens("non-existent-model")
		if defaultTokens != 0 {
			t.Errorf("Expected default maxTokens %d, got %d", 0, defaultTokens)
		}
	})
}
