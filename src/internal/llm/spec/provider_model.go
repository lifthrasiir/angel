package spec

import (
	"fmt"
	"regexp"
	"strings"
)

var tagRegex = regexp.MustCompile(`^([+:][^\s]+)\s+`)
var providerModelRegex = regexp.MustCompile(`^(?:(.+?)::)?(.+)$`)

// ParseProviderModel parses a provider-model string like "geminicli::gemini-3-flash-preview" or "+low antigravity::gemini-3-pro-low"
func ParseProviderModel(s string) (ProviderModel, error) {
	var variant, submodel string

	// Extract tags from the beginning (can be in any order)
	for {
		match := tagRegex.FindStringSubmatch(s)
		if match == nil {
			break
		}

		tag := match[1]
		if strings.HasPrefix(tag, "+") {
			variant = tag
		} else if strings.HasPrefix(tag, ":") {
			submodel = tag
		}

		// Remove the tag from the string
		s = s[len(match[0]):]
	}

	// Parse the remaining as provider::model
	matches := providerModelRegex.FindStringSubmatch(s)
	if matches == nil {
		return ProviderModel{}, fmt.Errorf("invalid provider-model format: %s", s)
	}

	provider := matches[1] // Optional provider (before ::)
	model := matches[2]    // Model name (required)

	// Validate: provider should not start with + or : (that would indicate a malformed tag)
	if provider != "" && (strings.HasPrefix(provider, "+") || strings.HasPrefix(provider, ":")) {
		return ProviderModel{}, fmt.Errorf("invalid provider-model format: %s (tag without space before provider)", s)
	}

	// Handle wildcard provider case (::model)
	// If provider is empty and model starts with "::", strip the "::" prefix
	if provider == "" && strings.HasPrefix(model, "::") {
		model = strings.TrimPrefix(model, "::")
	}

	return ProviderModel{
		Provider: provider,
		Model:    model,
		Variant:  variant,
		Submodel: submodel,
	}, nil
}

// String returns the string representation of the provider-model
func (pm ProviderModel) String() string {
	var parts []string
	if pm.Variant != "" {
		parts = append(parts, pm.Variant)
	}
	if pm.Submodel != "" {
		parts = append(parts, pm.Submodel)
	}
	if pm.Provider != "" {
		parts = append(parts, pm.Provider+"::"+pm.Model)
	} else {
		// For wildcard providers, don't include "::" in String representation
		if len(parts) == 0 {
			return "::" + pm.Model
		}
		parts = append(parts, "::"+pm.Model)
	}
	return strings.Join(parts, " ")
}

// IsWildcard returns true if this is a wildcard provider (::model)
func (pm ProviderModel) IsWildcard() bool {
	return pm.Provider == ""
}

// Matches checks if this provider-model matches the given provider and model name
func (pm ProviderModel) Matches(providerName, modelName string, variant, submodel string) bool {
	// Check variant match
	if variant != "" && pm.Variant != variant {
		return false
	}
	if variant == "" && pm.Variant != "" {
		return false
	}

	// Check submodel match
	if submodel != "" && pm.Submodel != submodel {
		return false
	}
	if submodel == "" && pm.Submodel != "" {
		return false
	}

	// Check provider match (wildcard matches any)
	if !pm.IsWildcard() && pm.Provider != providerName {
		return false
	}

	// Check model name match
	if pm.Model != modelName {
		return false
	}

	return true
}

// ParseExternalModelName parses an external model name (with variant/submodel) and returns its components
func ParseExternalModelName(externalName string) (baseName string, variant string, submodel string) {
	// Extract variant (+tag)
	if strings.Contains(externalName, "+") {
		plusIdx := strings.Index(externalName, "+")
		nextSlash := strings.Index(externalName[plusIdx:], "/")
		nextSpace := strings.Index(externalName[plusIdx:], " ")

		endIdx := len(externalName)
		if nextSlash != -1 && nextSlash > 0 {
			endIdx = min(endIdx, plusIdx+nextSlash)
		}
		if nextSpace != -1 && nextSpace > 0 {
			endIdx = min(endIdx, plusIdx+nextSpace)
		}

		variant = externalName[plusIdx:endIdx]
		externalName = externalName[:plusIdx] + externalName[endIdx:]
		externalName = strings.TrimSuffix(externalName, " ")
	}

	// Extract submodel (:tag)
	// Need to be careful not to confuse with :: in provider names
	if strings.Contains(externalName, ":") {
		// Find : that's not part of ::
		colonIdx := strings.Index(externalName, ":")
		if !strings.Contains(externalName[colonIdx:], "::") {
			// This is a submodel
			nextSlash := strings.Index(externalName[colonIdx:], "/")
			nextSpace := strings.Index(externalName[colonIdx:], " ")

			endIdx := len(externalName)
			if nextSlash != -1 && nextSlash > 0 {
				endIdx = min(endIdx, colonIdx+nextSlash)
			}
			if nextSpace != -1 && nextSpace > 0 {
				endIdx = min(endIdx, colonIdx+nextSpace)
			}

			submodel = externalName[colonIdx:endIdx]
			externalName = externalName[:colonIdx] + externalName[endIdx:]
			externalName = strings.TrimSuffix(externalName, " ")
		}
	}

	baseName = externalName
	return baseName, variant, submodel
}
