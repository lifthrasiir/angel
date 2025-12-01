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
	for _, model := range registry.Models {
		if registry.isGeminiModelUnsafe(model) {
			geminiModels[model.Name] = model
		}
	}

	provider := NewCodeAssistProvider(geminiModels, nil)

	// Test 1: session_name task for gemini-2.5-flash
	t.Run("SessionNameTask", func(t *testing.T) {
		returnProvider, returnModelName, params := provider.SubagentProviderAndParams("gemini-2.5-flash", "session_name")

		// Should return gemini-2.5-flash/subagent, which modelName *is* gemini-2.5-flash-lite
		if returnModelName != "gemini-2.5-flash/subagent" {
			t.Errorf("Expected 'gemini-2.5-flash/subagent', got '%s'", returnModelName)
		}

		// Should use @subagent genParams
		if params.Temperature != 0.0 {
			t.Errorf("Expected temperature 0.0, got %f", params.Temperature)
		}
		if params.TopK != -1 {
			t.Errorf("Expected topK -1, got %d", params.TopK)
		}
		if params.TopP != 0.0 {
			t.Errorf("Expected topP 0.0, got %f", params.TopP)
		}

		// Provider should be self
		if returnProvider != provider {
			t.Error("Expected returnProvider to be the same as provider")
		}
	})

	// Test 2: image_generation task for gemini-2.5-flash
	t.Run("ImageGenerationTask", func(t *testing.T) {
		_, returnModelName, params := provider.SubagentProviderAndParams("gemini-2.5-flash", "image_generation")

		// Should return some image model (currently gemini-2.5-flash-image, but may change)
		if !strings.Contains(returnModelName, "-image") {
			t.Errorf("Expected some image model, got '%s'", returnModelName)
		}

		// Should use @subagent genParams (default for subagents)
		if params.Temperature != 0.0 {
			t.Errorf("Expected temperature 0.0, got %f", params.Temperature)
		}
		if params.TopK != -1 {
			t.Errorf("Expected topK -1, got %d", params.TopK)
		}
		if params.TopP != 0.0 {
			t.Errorf("Expected topP 0.0, got %f", params.TopP)
		}
	})

	// Test 3: Non-existent task
	t.Run("NonExistentTask", func(t *testing.T) {
		_, returnModelName, params := provider.SubagentProviderAndParams("gemini-2.5-flash", "non_existent_task")

		// Should return empty values when task not found
		if returnModelName != "" {
			t.Errorf("Expected empty returnModelName for non-existent task, got '%s'", returnModelName)
		}

		if params.Temperature != 0.0 || params.TopK != 0 || params.TopP != 0.0 {
			t.Error("Expected zero params for non-existent task")
		}
	})

	// Test 4: Non-existent model
	t.Run("NonExistentModel", func(t *testing.T) {
		_, returnModelName, params := provider.SubagentProviderAndParams("non-existent-model", "session_name")

		// Should return empty values when model not found
		if returnModelName != "" {
			t.Errorf("Expected empty returnModelName for non-existent model, got '%s'", returnModelName)
		}

		if params.Temperature != 0.0 || params.TopK != 0 || params.TopP != 0.0 {
			t.Error("Expected zero params for non-existent model")
		}
	})

	// Test 5: Verify MaxTokens method uses models
	t.Run("MaxTokensFromModels", func(t *testing.T) {
		maxTokens := provider.MaxTokens("gemini-2.5-flash")
		expected := 1048576 // From models.go MaxTokens logic
		if maxTokens != expected {
			t.Errorf("Expected maxTokens %d, got %d", expected, maxTokens)
		}

		// Test non-existent model (should return default)
		defaultTokens := provider.MaxTokens("non-existent-model")
		if defaultTokens != 1048576 {
			t.Errorf("Expected default maxTokens %d, got %d", 1048576, defaultTokens)
		}
	})

	// Test 6: Verify RelativeDisplayOrder method uses models
	t.Run("RelativeDisplayOrderFromModels", func(t *testing.T) {
		order := provider.RelativeDisplayOrder("gemini-2.5-flash")
		expected := 10 // From models.go RelativeDisplayOrder logic
		if order != expected {
			t.Errorf("Expected display order %d, got %d", expected, order)
		}

		// Test non-existent model (should return 0)
		defaultOrder := provider.RelativeDisplayOrder("non-existent-model")
		if defaultOrder != 0 {
			t.Errorf("Expected default display order 0, got %d", defaultOrder)
		}
	})
}
