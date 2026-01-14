package spec

import (
	"encoding/json"
	"fmt"
)

// LoadSpecs parses models.json and returns a SpecRegistry
func LoadSpecs(data []byte) (*SpecRegistry, error) {
	var config ModelsConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse models JSON: %w", err)
	}

	// Build known providers map
	knownProviders := make(map[string]bool)
	for _, providerStr := range config.KnownProviders {
		knownProviders[providerStr] = true
	}

	registry := &SpecRegistry{
		Config:         &config,
		ModelSpecs:     make(map[string]*ModelSpec),
		Aliases:        make(map[string]string),
		DisplayOrder:   config.DisplayOrder,
		KnownProviders: knownProviders,
	}

	// Parse models
	if err := registry.parseModels(&config); err != nil {
		return nil, err
	}

	// Resolve inheritance
	if err := registry.resolveInheritance(); err != nil {
		return nil, err
	}

	return registry, nil
}

// parseModels parses raw models into ModelSpecs
func (sr *SpecRegistry) parseModels(config *ModelsConfig) error {
	for name, raw := range config.Models {
		switch v := raw.(type) {
		case string:
			// String alias
			sr.Aliases[name] = v
		case map[string]interface{}:
			// Convert to RawModel
			jsonBytes, err := json.Marshal(v)
			if err != nil {
				return fmt.Errorf("failed to convert model %s: %w", name, err)
			}

			var rawModel RawModel
			if err := json.Unmarshal(jsonBytes, &rawModel); err != nil {
				return fmt.Errorf("failed to parse model %s: %w", name, err)
			}

			// Parse provider models
			var providerModels []ProviderModel
			if len(rawModel.Providers) > 0 {
				providerModels = make([]ProviderModel, 0, len(rawModel.Providers))
				for _, providerStr := range rawModel.Providers {
					pm, err := ParseProviderModel(providerStr)
					if err != nil {
						return fmt.Errorf("failed to parse provider-model '%s' for %s: %w", providerStr, name, err)
					}
					providerModels = append(providerModels, pm)
				}
			} else if rawModel.Extends == "" {
				// Auto-map to ::name if no providers specified and no extends
				// (models with extends will inherit from parent)
				providerModels = []ProviderModel{
					{Provider: "", Model: name},
				}
			}
			// Note: If providers is empty and extends is set, it will be inherited from parent

			// Parse variants
			variants := make(map[string]*VariantModelSpec)
			if rawModel.Variants != nil {
				for variantName, variantData := range rawModel.Variants {
					// Ensure variant name starts with +
					if len(variantName) == 0 || variantName[0] != '+' {
						return fmt.Errorf("variant name must start with '+': %s", variantName)
					}

					variantMap, ok := variantData.(map[string]interface{})
					if !ok {
						return fmt.Errorf("variant data must be an object: %s", variantName)
					}

					variant := &VariantModelSpec{
						Name: variantName,
					}

					// Parse variant genParams
					if genParamsRaw, ok := variantMap["genParams"]; ok {
						genParams, err := sr.resolveGenParams(genParamsRaw)
						if err != nil {
							return fmt.Errorf("failed to resolve genParams for variant %s: %w", variantName, err)
						}
						variant.GenParams = genParams
					}

					// Parse variant thoughtEnabled
					if thoughtEnabledRaw, ok := variantMap["thoughtEnabled"]; ok {
						if thoughtEnabledBool, ok := thoughtEnabledRaw.(bool); ok {
							variant.ThoughtEnabled = &thoughtEnabledBool
						}
					}

					// Parse variant provider models if present
					if providerModelsRaw, ok := variantMap["providers"]; ok {
						if providerModelsList, ok := providerModelsRaw.([]interface{}); ok {
							for _, pmRaw := range providerModelsList {
								if pmStr, ok := pmRaw.(string); ok {
									pm, err := ParseProviderModel(pmStr)
									if err != nil {
										return fmt.Errorf("failed to parse variant provider-model '%s': %w", pmStr, err)
									}
									variant.ProviderModels = append(variant.ProviderModels, pm)
								}
							}
						}
					}

					variants[variantName] = variant
				}
			}

			// Resolve genParams
			var genParams GenerationParams
			if rawModel.GenParams != nil {
				var err error
				genParams, err = sr.resolveGenParams(rawModel.GenParams)
				if err != nil {
					return fmt.Errorf("failed to resolve genParams for %s: %w", name, err)
				}
			}

			// Create ModelSpec
			// If ModelName is not specified, default to name
			modelName := rawModel.ModelName
			if modelName == "" {
				modelName = name
			}
			spec := &ModelSpec{
				Name:               name,
				ProviderModels:     providerModels,
				ModelName:          modelName,
				GenParams:          genParams,
				Fallback:           rawModel.Fallback,
				Subagents:          rawModel.Subagents,
				IgnoreSystemPrompt: getBoolValue(rawModel.IgnoreSystemPrompt, false),
				ThoughtEnabled:     getBoolValue(rawModel.ThoughtEnabled, false),
				ToolSupported:      getBoolValue(rawModel.ToolSupported, false),
				ResponseModalities: rawModel.ResponseModalities,
				MaxTokens:          getIntValue(rawModel.MaxTokens, 8192),
				InheritanceChain:   []string{name},
				Variants:           variants,
			}

			// Store spec
			sr.ModelSpecs[name] = spec
		default:
			return fmt.Errorf("invalid model type for %s: expected object or string", name)
		}
	}

	return nil
}

// resolveInheritance resolves model inheritance chains
func (sr *SpecRegistry) resolveInheritance() error {
	for name, spec := range sr.ModelSpecs {
		if err := sr.resolveInheritanceRecursive(spec, name, make(map[string]bool)); err != nil {
			return err
		}
	}
	return nil
}

func (sr *SpecRegistry) resolveInheritanceRecursive(spec *ModelSpec, modelName string, visited map[string]bool) error {
	if visited[modelName] {
		return fmt.Errorf("inheritance cycle detected: %s", modelName)
	}

	visited[modelName] = true

	// Get raw model to check extends field
	rawModel := sr.getRawModel(modelName)
	if rawModel == nil {
		return fmt.Errorf("model not found: %s", modelName)
	}

	// If this model extends another, resolve parent first
	if rawModel.Extends != "" {
		parentSpec, exists := sr.ModelSpecs[rawModel.Extends]
		if !exists {
			return fmt.Errorf("parent model not found: %s", rawModel.Extends)
		}

		// Recursively resolve parent inheritance
		if err := sr.resolveInheritanceRecursive(parentSpec, rawModel.Extends, visited); err != nil {
			return err
		}

		// Merge parent into child
		sr.mergeModelSpec(parentSpec, spec, modelName)

		// Update inheritance chain
		spec.InheritanceChain = append([]string{modelName}, parentSpec.InheritanceChain...)
	}

	delete(visited, modelName)
	return nil
}

// mergeModelSpec merges parent spec into child spec
func (sr *SpecRegistry) mergeModelSpec(parent, child *ModelSpec, childName string) {
	// Merge non-array fields (child overrides parent if present)
	if len(child.ProviderModels) == 0 {
		child.ProviderModels = parent.ProviderModels
	}

	if child.ModelName == "" {
		child.ModelName = parent.ModelName
	}

	// Merge GenParams if child didn't explicitly set them
	rawModel := sr.getRawModel(childName)
	if rawModel != nil && rawModel.GenParams == nil {
		// Child didn't specify genParams, use parent's
		child.GenParams = parent.GenParams
	}

	if child.Fallback == "" {
		child.Fallback = parent.Fallback
	}

	if child.Subagents == nil {
		child.Subagents = parent.Subagents
	}

	// Only override if child didn't explicitly set the value (nil)
	// Note: rawModel was already fetched above for GenParams check
	if rawModel != nil {
		if rawModel.IgnoreSystemPrompt == nil {
			child.IgnoreSystemPrompt = parent.IgnoreSystemPrompt
		}
		if rawModel.ThoughtEnabled == nil {
			child.ThoughtEnabled = parent.ThoughtEnabled
		}
		if rawModel.ToolSupported == nil {
			child.ToolSupported = parent.ToolSupported
		}
		if rawModel.MaxTokens == nil {
			child.MaxTokens = parent.MaxTokens
		}
	}

	if child.ResponseModalities == nil {
		child.ResponseModalities = parent.ResponseModalities
	}

	// Merge variants from parent
	if child.Variants == nil {
		child.Variants = make(map[string]*VariantModelSpec)
	}
	for variantName, parentVariant := range parent.Variants {
		if _, exists := child.Variants[variantName]; !exists {
			child.Variants[variantName] = parentVariant
		}
	}
}

// resolveGenParams resolves genParams from reference or inline object
func (sr *SpecRegistry) resolveGenParams(raw interface{}) (GenerationParams, error) {
	switch v := raw.(type) {
	case string:
		if paramSet, exists := sr.Config.GenParams[v]; exists {
			return sr.convertToGenParams(paramSet)
		} else {
			return GenerationParams{}, fmt.Errorf("genParams reference not found: %s", v)
		}
	case map[string]interface{}:
		return sr.convertToGenParams(v)
	default:
		return GenerationParams{}, fmt.Errorf("invalid genParams type: %T", raw)
	}
}

// convertToGenParams converts raw interface{} to GenerationParams
func (sr *SpecRegistry) convertToGenParams(raw interface{}) (GenerationParams, error) {
	paramMap, ok := raw.(map[string]interface{})
	if !ok {
		return GenerationParams{}, fmt.Errorf("genParams must be an object, got %T", raw)
	}

	// Handle extends
	var baseParams GenerationParams
	if extendsRef, ok := paramMap["extends"].(string); ok {
		if baseGenParams, exists := sr.Config.GenParams[extendsRef]; exists {
			var err error
			baseParams, err = sr.convertToGenParams(baseGenParams)
			if err != nil {
				return GenerationParams{}, fmt.Errorf("failed to resolve extended genParams '%s': %w", extendsRef, err)
			}
		} else {
			return GenerationParams{}, fmt.Errorf("extended genParams not found: %s", extendsRef)
		}
	}

	// Create params map without extends key
	paramsMap := make(map[string]interface{})
	for k, v := range paramMap {
		if k != "extends" {
			paramsMap[k] = v
		}
	}

	// Convert to JSON and unmarshal to use GenerationParams logic
	jsonBytes, err := json.Marshal(paramsMap)
	if err != nil {
		return GenerationParams{}, fmt.Errorf("failed to marshal genParams: %w", err)
	}

	var overrideParams GenerationParams
	if err := json.Unmarshal(jsonBytes, &overrideParams); err != nil {
		return GenerationParams{}, fmt.Errorf("failed to unmarshal genParams: %w", err)
	}

	// Merge: overrideParams takes precedence
	result := baseParams
	if overrideParams.Temperature != 0 {
		result.Temperature = overrideParams.Temperature
	}
	if overrideParams.TopK != 0 {
		result.TopK = overrideParams.TopK
	}
	if overrideParams.TopP != 0 {
		result.TopP = overrideParams.TopP
	}
	if overrideParams.Thinking != nil {
		result.Thinking = overrideParams.Thinking
	}

	return result, nil
}

// getRawModel retrieves the raw model definition for parsing
func (sr *SpecRegistry) getRawModel(name string) *RawModel {
	raw, exists := sr.Config.Models[name]
	if !exists {
		return nil
	}

	// Handle aliases
	if alias, isAlias := raw.(string); isAlias {
		return sr.getRawModel(alias)
	}

	// Convert map to RawModel
	if rawMap, isMap := raw.(map[string]interface{}); isMap {
		jsonBytes, _ := json.Marshal(rawMap)
		var rawModel RawModel
		json.Unmarshal(jsonBytes, &rawModel)
		return &rawModel
	}

	return nil
}

// Helper functions
func getBoolValue(ptr *bool, defaultValue bool) bool {
	if ptr == nil {
		return defaultValue
	}
	return *ptr
}

func getIntValue(ptr *int, defaultValue int) int {
	if ptr == nil {
		return defaultValue
	}
	return *ptr
}
