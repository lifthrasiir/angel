package spec

import (
	"encoding/json"
	"testing"
)

// Minimal models.json for testing
var minimalModelsJSON = []byte(`{
	"genParams": {
		"@chat": {
			"temperature": 0.7,
			"topK": 64,
			"topP": 0.95
		},
		"@chat-thinking-low": {
			"extends": "@chat",
			"thinking": "low"
		}
	},
	"models": {
		"$base": {
			"genParams": "@chat"
		},
		"gemini-3-flash": {
			"extends": "$base",
			"providers": [
				"geminicli::gemini-3-flash-preview",
				"antigravity::gemini-3-flash",
				"::gemini-3-flash",
				"::gemini-3-flash-preview"
			],
			"maxTokens": 1048576
		},
		"gemini-3-pro": {
			"extends": "$base",
			"providers": [
				"geminicli::gemini-3-pro-preview",
				"+low antigravity::gemini-3-pro-low",
				"+high antigravity::gemini-3-pro-high",
				"::gemini-3-pro"
			],
			"maxTokens": 1048576,
			"variants": {
				"+low": {
					"genParams": "@chat-thinking-low"
				},
				"+high": {
					"genParams": "@chat"
				}
			}
		},
		"gemini-2.5-flash": {
			"providers": [],
			"maxTokens": 1048576
		},
		"gpt-oss": {
			"extends": "$base",
			"providers": [
				":120b +medium antigravity::gpt-oss-120b-medium"
			],
			"maxTokens": 131072,
			"variants": {
				"+medium": {
					"thoughtEnabled": true
				}
			}
		},
		"alias-target": {
			"providers": ["geminicli::test-model"],
			"maxTokens": 1000
		},
		"alias-test": "alias-target"
	},
	"knownProviders": [
		"geminicli::gemini-3-flash-preview",
		"antigravity::gemini-3-flash",
		"antigravity::gemini-3-pro-low",
		"antigravity::gemini-3-pro-high",
		"antigravity::gpt-oss-120b-medium"
	],
	"displayOrder": [
		"gemini-3-flash",
		"gemini-3-pro+low",
		"gemini-3-pro+high",
		"gpt-oss:120b+medium"
	]
}`)

func TestLoadSpecs(t *testing.T) {
	registry, err := LoadSpecs(minimalModelsJSON)
	if err != nil {
		t.Fatalf("Failed to load specs: %v", err)
	}

	// Check basic structure
	if registry.Config == nil {
		t.Error("Config is nil")
	}

	if len(registry.ModelSpecs) == 0 {
		t.Error("ModelSpecs is empty")
	}

	if len(registry.KnownProviders) == 0 {
		t.Error("KnownProviders is empty")
	}
}

func TestParseProviderModel(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected ProviderModel
		hasError bool
	}{
		{
			name:  "simple wildcard",
			input: "::gemini-3-flash",
			expected: ProviderModel{
				Provider: "",
				Model:    "gemini-3-flash",
			},
		},
		{
			name:  "specific provider",
			input: "geminicli::gemini-3-flash-preview",
			expected: ProviderModel{
				Provider: "geminicli",
				Model:    "gemini-3-flash-preview",
			},
		},
		{
			name:  "with variant",
			input: "+low antigravity::gemini-3-pro-low",
			expected: ProviderModel{
				Provider: "antigravity",
				Model:    "gemini-3-pro-low",
				Variant:  "+low",
			},
		},
		{
			name:  "with submodel",
			input: ":120b antigravity::gpt-oss-120b",
			expected: ProviderModel{
				Provider: "antigravity",
				Model:    "gpt-oss-120b",
				Submodel: ":120b",
			},
		},
		{
			name:  "with variant and submodel",
			input: "+low :120b antigravity::model-name",
			expected: ProviderModel{
				Provider: "antigravity",
				Model:    "model-name",
				Variant:  "+low",
				Submodel: ":120b",
			},
		},
		{
			name:     "invalid format - missing space after variant",
			input:    "+lowantigravity::model",
			hasError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseProviderModel(tt.input)
			if tt.hasError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if result != tt.expected {
				t.Errorf("Expected %+v, got %+v", tt.expected, result)
			}
		})
	}
}

func TestProviderModelMatches(t *testing.T) {
	tests := []struct {
		name      string
		pm        ProviderModel
		provider  string
		modelName string
		variant   string
		submodel  string
		expected  bool
	}{
		{
			name:      "wildcard matches any provider",
			pm:        ProviderModel{Provider: "", Model: "gemini-3-flash"},
			provider:  "geminicli",
			modelName: "gemini-3-flash",
			expected:  true,
		},
		{
			name:      "specific provider matches only itself",
			pm:        ProviderModel{Provider: "geminicli", Model: "gemini-3-flash"},
			provider:  "geminicli",
			modelName: "gemini-3-flash",
			expected:  true,
		},
		{
			name:      "specific provider does not match different provider",
			pm:        ProviderModel{Provider: "geminicli", Model: "gemini-3-flash"},
			provider:  "antigravity",
			modelName: "gemini-3-flash",
			expected:  false,
		},
		{
			name:      "variant must match",
			pm:        ProviderModel{Provider: "", Model: "gemini-3-pro", Variant: "+low"},
			modelName: "gemini-3-pro",
			variant:   "+low",
			expected:  true,
		},
		{
			name:      "variant mismatch",
			pm:        ProviderModel{Provider: "", Model: "gemini-3-pro", Variant: "+low"},
			modelName: "gemini-3-pro",
			variant:   "+high",
			expected:  false,
		},
		{
			name:      "submodel must match",
			pm:        ProviderModel{Provider: "", Model: "gpt-oss", Submodel: ":120b"},
			modelName: "gpt-oss",
			submodel:  ":120b",
			expected:  true,
		},
		{
			name:      "submodel mismatch",
			pm:        ProviderModel{Provider: "", Model: "gpt-oss", Submodel: ":120b"},
			modelName: "gpt-oss",
			submodel:  ":70b",
			expected:  false,
		},
		{
			name:      "model name must match",
			pm:        ProviderModel{Provider: "", Model: "gemini-3-flash"},
			modelName: "gemini-3-pro",
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.pm.Matches(tt.provider, tt.modelName, tt.variant, tt.submodel)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestParseExternalModelName(t *testing.T) {
	tests := []struct {
		name             string
		input            string
		expectedBaseName string
		expectedVariant  string
		expectedSubmodel string
	}{
		{
			name:             "simple model name",
			input:            "gemini-3-flash",
			expectedBaseName: "gemini-3-flash",
		},
		{
			name:             "model with variant",
			input:            "gemini-3-pro+low",
			expectedBaseName: "gemini-3-pro",
			expectedVariant:  "+low",
		},
		{
			name:             "model with submodel",
			input:            "gpt-oss:120b",
			expectedBaseName: "gpt-oss",
			expectedSubmodel: ":120b",
		},
		{
			name:             "model with both",
			input:            "gpt-oss:120b+medium",
			expectedBaseName: "gpt-oss",
			expectedVariant:  "+medium",
			expectedSubmodel: ":120b",
		},
		{
			name:             "complex path",
			input:            "path/to/gpt-oss:120b+medium/extra",
			expectedBaseName: "path/to/gpt-oss/extra",
			expectedVariant:  "+medium",
			expectedSubmodel: ":120b",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseName, variant, submodel := ParseExternalModelName(tt.input)
			if baseName != tt.expectedBaseName {
				t.Errorf("Expected baseName %q, got %q", tt.expectedBaseName, baseName)
			}
			if variant != tt.expectedVariant {
				t.Errorf("Expected variant %q, got %q", tt.expectedVariant, variant)
			}
			if submodel != tt.expectedSubmodel {
				t.Errorf("Expected submodel %q, got %q", tt.expectedSubmodel, submodel)
			}
		})
	}
}

func TestGetProviderTuples(t *testing.T) {
	registry, err := LoadSpecs(minimalModelsJSON)
	if err != nil {
		t.Fatalf("Failed to load specs: %v", err)
	}

	tests := []struct {
		name           string
		externalName   string
		expectedTuples []ProviderTuple
	}{
		{
			name:         "simple model",
			externalName: "gemini-3-flash",
			expectedTuples: []ProviderTuple{
				{"geminicli", "gemini-3-flash-preview"},
				{"antigravity", "gemini-3-flash"},
				{"", "gemini-3-flash"},
				{"", "gemini-3-flash-preview"},
			},
		},
		{
			name:         "model with variant",
			externalName: "gemini-3-pro+low",
			expectedTuples: []ProviderTuple{
				{"antigravity", "gemini-3-pro-low"},
			},
		},
		{
			name:         "model with high variant",
			externalName: "gemini-3-pro+high",
			expectedTuples: []ProviderTuple{
				{"antigravity", "gemini-3-pro-high"},
			},
		},
		{
			name:         "model with submodel and variant",
			externalName: "gpt-oss:120b+medium",
			expectedTuples: []ProviderTuple{
				{"antigravity", "gpt-oss-120b-medium"},
			},
		},
		{
			name:         "model with empty providers auto-maps",
			externalName: "gemini-2.5-flash",
			expectedTuples: []ProviderTuple{
				{"", "gemini-2.5-flash"},
			},
		},
		{
			name:         "alias resolution",
			externalName: "alias-test",
			expectedTuples: []ProviderTuple{
				{"geminicli", "test-model"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tuples := registry.GetProviderTuples(tt.externalName)

			if len(tuples) != len(tt.expectedTuples) {
				t.Errorf("Expected %d tuples, got %d", len(tt.expectedTuples), len(tuples))
				t.Logf("Got: %+v", tuples)
				return
			}

			for i, tuple := range tuples {
				if tuple.ProviderType != tt.expectedTuples[i].ProviderType {
					t.Errorf("Tuple %d: expected provider %q, got %q", i, tt.expectedTuples[i].ProviderType, tuple.ProviderType)
				}
				if tuple.ModelName != tt.expectedTuples[i].ModelName {
					t.Errorf("Tuple %d: expected model %q, got %q", i, tt.expectedTuples[i].ModelName, tuple.ModelName)
				}
			}
		})
	}
}

func TestGetModelSpec(t *testing.T) {
	registry, err := LoadSpecs(minimalModelsJSON)
	if err != nil {
		t.Fatalf("Failed to load specs: %v", err)
	}

	// Test basic lookup
	spec := registry.GetModelSpec("gemini-3-flash")
	if spec == nil {
		t.Fatal("Expected spec to exist, got nil")
	}

	if spec.Name != "gemini-3-flash" {
		t.Errorf("Expected name %q, got %q", "gemini-3-flash", spec.Name)
	}

	if spec.MaxTokens != 1048576 {
		t.Errorf("Expected maxTokens 1048576, got %d", spec.MaxTokens)
	}

	// Test variant lookup
	spec = registry.GetModelSpec("gemini-3-pro+low")
	if spec == nil {
		t.Fatal("Expected spec to exist for variant, got nil")
	}

	// Test alias resolution
	spec = registry.GetModelSpec("alias-test")
	if spec == nil {
		t.Fatal("Expected alias to resolve, got nil")
	}

	// Test non-existent
	spec = registry.GetModelSpec("non-existent")
	if spec != nil {
		t.Error("Expected nil for non-existent model, got spec")
	}
}

func TestValidation(t *testing.T) {
	registry, err := LoadSpecs(minimalModelsJSON)
	if err != nil {
		t.Fatalf("Failed to load specs: %v", err)
	}

	// Validate displayOrder
	errors := registry.ValidateDisplayOrder()
	if len(errors) > 0 {
		t.Errorf("ValidateDisplayOrder returned errors: %v", errors)
	}

	// Validate inheritance chains
	errors = registry.ValidateInheritanceChains()
	if len(errors) > 0 {
		t.Errorf("ValidateInheritanceChains returned errors: %v", errors)
	}

	// Validate aliases
	errors = registry.ValidateAliases()
	if len(errors) > 0 {
		t.Errorf("ValidateAliases returned errors: %v", errors)
	}
}

func TestLoadSpecsWithInvalidJSON(t *testing.T) {
	invalidJSON := []byte(`{invalid json}`)

	_, err := LoadSpecs(invalidJSON)
	if err == nil {
		t.Error("Expected error for invalid JSON, got nil")
	}
}

func TestLoadSpecsWithUnknownModelInDisplayOrder(t *testing.T) {
	invalidJSON := []byte(`{
		"models": {
			"gemini-3-flash": {
				"providers": ["geminicli::gemini-3-flash-preview"]
			}
		},
		"displayOrder": [
			"gemini-3-flash",
			"non-existent-model"
		]
	}`)

	registry, err := LoadSpecs(invalidJSON)
	if err != nil {
		t.Fatalf("Failed to load specs: %v", err)
	}

	errors := registry.ValidateDisplayOrder()
	if len(errors) == 0 {
		t.Error("Expected validation error for unknown model in displayOrder, got none")
	}
}

func TestGenerationParamsUnmarshal(t *testing.T) {
	tests := []struct {
		name     string
		json     string
		expected GenerationParams
	}{
		{
			name: "all fields",
			json: `{"temperature": 0.7, "topK": 64, "topP": 0.95}`,
			expected: GenerationParams{
				Temperature: 0.7,
				TopK:        64,
				TopP:        0.95,
			},
		},
		{
			name: "with thinking",
			json: `{"temperature": 0.5, "thinking": "low"}`,
			expected: GenerationParams{
				Temperature: 0.5,
				Thinking:    "low",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gp GenerationParams
			err := json.Unmarshal([]byte(tt.json), &gp)
			if err != nil {
				t.Fatalf("Failed to unmarshal: %v", err)
			}

			if gp.Temperature != tt.expected.Temperature {
				t.Errorf("Expected Temperature %f, got %f", tt.expected.Temperature, gp.Temperature)
			}
			if gp.TopK != tt.expected.TopK {
				t.Errorf("Expected TopK %d, got %d", tt.expected.TopK, gp.TopK)
			}
			if gp.TopP != tt.expected.TopP {
				t.Errorf("Expected TopP %f, got %f", tt.expected.TopP, gp.TopP)
			}
			if gp.Thinking != tt.expected.Thinking {
				t.Errorf("Expected Thinking %v, got %v", tt.expected.Thinking, gp.Thinking)
			}
		})
	}
}
