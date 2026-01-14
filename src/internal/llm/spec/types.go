package spec

import "encoding/json"

// ModelsConfig represents the top-level models.json structure
type ModelsConfig struct {
	GenParams      map[string]interface{} `json:"genParams"`
	Models         map[string]interface{} `json:"models"`
	KnownProviders []string               `json:"knownProviders"`
	DisplayOrder   []string               `json:"displayOrder"`
}

// RawModel represents a model definition from models.json
type RawModel struct {
	Extends            string                 `json:"extends,omitempty"`
	Providers          []string               `json:"providers,omitempty"`
	ModelName          string                 `json:"modelName,omitempty"`
	GenParams          interface{}            `json:"genParams,omitempty"`
	Fallback           string                 `json:"fallback,omitempty"`
	Subagents          map[string]string      `json:"subagents,omitempty"`
	IgnoreSystemPrompt *bool                  `json:"ignoreSystemPrompt,omitempty"`
	ThoughtEnabled     *bool                  `json:"thoughtEnabled,omitempty"`
	ToolSupported      *bool                  `json:"toolSupported,omitempty"`
	ResponseModalities []string               `json:"responseModalities,omitempty"`
	MaxTokens          *int                   `json:"maxTokens,omitempty"`
	Variants           map[string]interface{} `json:"variants,omitempty"`
}

// GenerationParams represents LLM generation parameters
type GenerationParams struct {
	Temperature float32     `json:"temperature,omitempty"`
	TopK        int32       `json:"topK,omitempty"`
	TopP        float32     `json:"topP,omitempty"`
	Thinking    interface{} `json:"thinking,omitempty"`
}

// ProviderModel represents a parsed provider::model specification
type ProviderModel struct {
	Provider string // e.g., "geminicli", "" for wildcard
	Model    string // e.g., "gemini-3-flash-preview"
	Variant  string // e.g., "+low", "+high", or ""
	Submodel string // e.g., ":120b", or ""
}

// ModelSpec represents a fully parsed model specification (without provider instances)
type ModelSpec struct {
	Name               string
	ProviderModels     []ProviderModel // Parsed provider::model specifications
	ModelName          string
	GenParams          GenerationParams
	Fallback           string
	Subagents          map[string]string
	IgnoreSystemPrompt bool
	ThoughtEnabled     bool
	ToolSupported      bool
	ResponseModalities []string
	MaxTokens          int
	InheritanceChain   []string                     // For debugging/validation
	Variants           map[string]*VariantModelSpec // Variant definitions (+low, +high, etc.)
}

// VariantModelSpec represents a variant of a model (e.g., +low, +high)
type VariantModelSpec struct {
	Name           string
	GenParams      GenerationParams
	ThoughtEnabled *bool
	ProviderModels []ProviderModel // Provider-model mappings for this variant
}

// ProviderTuple represents a (providerType, modelName) tuple
type ProviderTuple struct {
	ProviderType string // e.g., "geminicli", "antigravity", "" for wildcard
	ModelName    string // e.g., "gemini-3-flash-preview"
}

// SpecRegistry holds all parsed model specifications
type SpecRegistry struct {
	Config         *ModelsConfig
	ModelSpecs     map[string]*ModelSpec
	Aliases        map[string]string
	DisplayOrder   []string
	KnownProviders map[string]bool // Known provider::model specifications
}

// UnmarshalJSON implements custom JSON unmarshaling for GenerationParams
func (gp *GenerationParams) UnmarshalJSON(data []byte) error {
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	// Helper to parse fields
	getFloat := func(key string) float32 {
		if v, ok := raw[key]; ok {
			switch val := v.(type) {
			case float64:
				return float32(val)
			case float32:
				return val
			}
		}
		return 0
	}

	getInt := func(key string) int32 {
		if v, ok := raw[key]; ok {
			switch val := v.(type) {
			case float64:
				return int32(val)
			case float32:
				return int32(val)
			case int:
				return int32(val)
			case int32:
				return val
			}
		}
		return 0
	}

	gp.Temperature = getFloat("temperature")
	gp.TopK = getInt("topK")
	gp.TopP = getFloat("topP")
	if thinking, ok := raw["thinking"]; ok {
		gp.Thinking = thinking
	}

	return nil
}
