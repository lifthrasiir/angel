package spec

import (
	"fmt"
)

// GetProviderTuples returns an ordered list of (providerType, modelName) tuples for a given external model name
func (sr *SpecRegistry) GetProviderTuples(externalName string) []ProviderTuple {
	baseName, variant, submodel := ParseExternalModelName(externalName)

	// Get the model spec
	modelSpec, exists := sr.ModelSpecs[baseName]
	if !exists {
		// Try alias
		if aliasTarget, isAlias := sr.Aliases[baseName]; isAlias {
			modelSpec = sr.ModelSpecs[aliasTarget]
		}
		if modelSpec == nil {
			return []ProviderTuple{}
		}
	}

	// If no ProviderModels, return empty
	if len(modelSpec.ProviderModels) == 0 {
		return []ProviderTuple{}
	}

	var matches []ProviderTuple

	// Check each provider-model specification in order
	// We only filter by variant/submodel since we've already selected the ModelSpec by baseName
	for _, pm := range modelSpec.ProviderModels {
		// Check if this provider-model matches our variant/submodel requirements
		if variantMatches(pm.Variant, variant) && submodelMatches(pm.Submodel, submodel) {
			matches = append(matches, ProviderTuple{
				ProviderType: pm.Provider,
				ModelName:    pm.Model,
			})
		}
	}

	return matches
}

// variantMatches checks if the provider-model's variant matches the requested variant
func variantMatches(pmVariant, requestedVariant string) bool {
	// If no variant requested, only match provider-models with no variant
	if requestedVariant == "" {
		return pmVariant == ""
	}
	// If variant requested, it must match exactly
	return pmVariant == requestedVariant
}

// submodelMatches checks if the provider-model's submodel matches the requested submodel
func submodelMatches(pmSubmodel, requestedSubmodel string) bool {
	// If no submodel requested, only match provider-models with no submodel
	if requestedSubmodel == "" {
		return pmSubmodel == ""
	}
	// If submodel requested, it must match exactly
	return pmSubmodel == requestedSubmodel
}

// GetModelSpec returns the model spec for a given external model name
func (sr *SpecRegistry) GetModelSpec(externalName string) *ModelSpec {
	baseName, _, _ := ParseExternalModelName(externalName)

	// Check direct access
	if modelSpec, exists := sr.ModelSpecs[baseName]; exists {
		return modelSpec
	}

	// Try alias
	if aliasTarget, isAlias := sr.Aliases[baseName]; isAlias {
		return sr.ModelSpecs[aliasTarget]
	}

	return nil
}

// ValidateDisplayOrder validates that all models in displayOrder exist
func (sr *SpecRegistry) ValidateDisplayOrder() []error {
	var errors []error

	for _, modelName := range sr.DisplayOrder {
		if sr.GetModelSpec(modelName) == nil {
			errors = append(errors, fmt.Errorf("model in displayOrder not found: %s", modelName))
		}
	}

	return errors
}

// ValidateInheritanceChains validates that all models have non-empty inheritance chains
func (sr *SpecRegistry) ValidateInheritanceChains() []error {
	var errors []error

	for name, modelSpec := range sr.ModelSpecs {
		if len(modelSpec.InheritanceChain) == 0 {
			errors = append(errors, fmt.Errorf("model has empty inheritance chain: %s", name))
		}
	}

	return errors
}

// ValidateAliases validates that all aliases point to existing models
func (sr *SpecRegistry) ValidateAliases() []error {
	var errors []error

	for alias, target := range sr.Aliases {
		if sr.ModelSpecs[target] == nil {
			// Check if target is also an alias
			if _, targetExists := sr.Aliases[target]; !targetExists {
				errors = append(errors, fmt.Errorf("alias target not found: %s -> %s", alias, target))
			}
		}
	}

	return errors
}
