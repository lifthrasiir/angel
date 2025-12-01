package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	. "github.com/lifthrasiir/angel/gemini"
)

const AngelEvalModelName = "angel-eval"

// Error types for models parsing and validation
type ModelsError struct {
	Type    string                 `json:"type"`
	Message string                 `json:"message"`
	Model   string                 `json:"model,omitempty"`
	Context map[string]interface{} `json:"context,omitempty"`
}

func (e ModelsError) Error() string {
	if e.Model != "" {
		return fmt.Sprintf("[%s] %s (model: %s)", e.Type, e.Message, e.Model)
	}
	return fmt.Sprintf("[%s] %s", e.Type, e.Message)
}

const (
	ErrTypeParse      = "parse_error"
	ErrTypeValidation = "validation_error"
	ErrTypeResolution = "resolution_error"
	ErrTypeNotFound   = "not_found_error"
)

// Raw structures for JSON unmarshaling
type ModelsConfig struct {
	GenParams    map[string]interface{} `json:"genParams"`
	Models       map[string]interface{} `json:"models"`
	DisplayOrder []string               `json:"displayOrder"`
}

type RawModel struct {
	Extends            string            `json:"extends,omitempty"`
	Providers          []string          `json:"providers,omitempty"`
	ModelName          string            `json:"modelName,omitempty"`
	GenParams          interface{}       `json:"genParams,omitempty"`
	Fallback           string            `json:"fallback,omitempty"`
	Subagents          map[string]string `json:"subagents,omitempty"`
	IgnoreSystemPrompt *bool             `json:"ignoreSystemPrompt,omitempty"`
	ThoughtEnabled     *bool             `json:"thoughtEnabled,omitempty"`
	ToolSupported      *bool             `json:"toolSupported,omitempty"`
	ResponseModalities []string          `json:"responseModalities,omitempty"`
	MaxTokens          *int              `json:"maxTokens,omitempty"`
}

// Resolved runtime structures
type GenerationParams struct {
	Temperature float32     `json:"temperature"`
	TopK        int32       `json:"topK"`
	TopP        float32     `json:"topP"`
	Thinking    interface{} `json:"thinking,omitempty"`
}

type Model struct {
	Name               string
	Providers          []string
	ModelName          string
	GenParams          GenerationParams
	Fallback           *Model
	Subagents          map[string]*Model
	IgnoreSystemPrompt bool
	ThoughtEnabled     bool
	ToolSupported      bool
	ResponseModalities []string
	MaxTokens          int
	InheritanceChain   []string // For debugging/validation
}

// OpenAI endpoint with provider and models
type OpenAIEndpoint struct {
	config     *OpenAIConfig
	provider   LLMProvider
	models     []OpenAIModel // Available models from this endpoint
	lastUpdate time.Time
	hash       string // Configuration hash for change detection
}

type ModelsRegistry struct {
	Models       map[string]*Model
	DisplayOrder []string
	Aliases      map[string]string
	rawConfig    *ModelsConfig // Keep for reference resolution

	// Provider management
	providers       map[string]LLMProvider     // model name -> provider
	openAIEndpoints map[string]*OpenAIEndpoint // config hash -> endpoint
	geminiProvider  LLMProvider                // Single Gemini provider

	// Thread safety
	mutex sync.RWMutex
}

// Global registry instance
var GlobalModelsRegistry *ModelsRegistry

// LoadModels loads and parses the models.json file
func LoadModels(data []byte) (*ModelsRegistry, error) {
	var config ModelsConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, ModelsError{
			Type:    ErrTypeParse,
			Message: fmt.Sprintf("Failed to parse models JSON: %v", err),
			Context: map[string]interface{}{},
		}
	}

	registry := &ModelsRegistry{
		Models:          make(map[string]*Model),
		DisplayOrder:    config.DisplayOrder,
		Aliases:         make(map[string]string),
		rawConfig:       &config,
		providers:       make(map[string]LLMProvider),
		openAIEndpoints: make(map[string]*OpenAIEndpoint),
	}

	// First pass: Parse raw models and handle aliases
	if err := registry.parseRawModels(&config); err != nil {
		return nil, err
	}

	// Second pass: Resolve inheritance and references
	if err := registry.resolveModels(); err != nil {
		return nil, err
	}

	// Validation phase
	if err := registry.validate(); err != nil {
		return nil, err
	}

	// Set global registry
	GlobalModelsRegistry = registry

	return registry, nil
}

// parseRawModels converts raw JSON structures to intermediate representation
func (r *ModelsRegistry) parseRawModels(config *ModelsConfig) error {
	// Parse genParams (basic validation)
	for name := range config.GenParams {
		if _, exists := config.GenParams[name]; !exists {
			return ModelsError{
				Type:    ErrTypeValidation,
				Message: fmt.Sprintf("Duplicate genParams name: %s", name),
			}
		}
	}

	// Parse models and aliases
	for name, raw := range config.Models {
		switch v := raw.(type) {
		case string:
			// String alias
			r.Aliases[name] = v
		case map[string]interface{}:
			// Convert to RawModel
			jsonBytes, err := json.Marshal(v)
			if err != nil {
				return ModelsError{
					Type:    ErrTypeParse,
					Message: fmt.Sprintf("Failed to convert model %s: %v", name, err),
					Model:   name,
				}
			}

			var rawModel RawModel
			if err := json.Unmarshal(jsonBytes, &rawModel); err != nil {
				return ModelsError{
					Type:    ErrTypeParse,
					Message: fmt.Sprintf("Failed to parse model %s: %v", name, err),
					Model:   name,
				}
			}

			// Create intermediate model representation
			intermediate := &Model{
				Name:               name,
				Providers:          rawModel.Providers,
				ModelName:          rawModel.ModelName,
				IgnoreSystemPrompt: r.getBoolValue(rawModel.IgnoreSystemPrompt, false),
				ThoughtEnabled:     r.getBoolValue(rawModel.ThoughtEnabled, false),
				ToolSupported:      r.getBoolValue(rawModel.ToolSupported, false),
				ResponseModalities: rawModel.ResponseModalities,
				MaxTokens:          r.getIntValue(rawModel.MaxTokens, 8192),
				InheritanceChain:   []string{name},
			}

			// Store raw model data for resolution
			r.Models[name] = intermediate
		default:
			return ModelsError{
				Type:    ErrTypeParse,
				Message: fmt.Sprintf("Invalid model type for %s: expected object or string", name),
				Model:   name,
			}
		}
	}

	return nil
}

// resolveModels handles inheritance, genParams, subagents, and fallbacks in a single pass
func (r *ModelsRegistry) resolveModels() error {
	// Resolve inheritance chains first
	for name, model := range r.Models {
		if err := r.resolveInheritance(model, name); err != nil {
			return err
		}
	}

	// Now resolve everything from RawModel perspective
	for name, model := range r.Models {
		if err := r.resolveModelFromRaw(name, model); err != nil {
			return err
		}
	}

	return nil
}

// resolveModelFromRaw resolves all model properties from RawModel in a single pass
func (r *ModelsRegistry) resolveModelFromRaw(name string, model *Model) error {
	rawModel := r.getRawModel(name)
	if rawModel == nil {
		return nil // Skip if no raw model (e.g., computed models)
	}

	// Resolve genParams with inheritance fallback
	var genParams GenerationParams
	var err error

	if rawModel.GenParams != nil {
		// Model has explicit genParams
		genParams, err = r.resolveGenParams(rawModel.GenParams)
		if err != nil {
			return ModelsError{
				Type:    ErrTypeResolution,
				Message: fmt.Sprintf("Failed to resolve genParams for %s: %v", name, err),
				Model:   name,
			}
		}
	} else {
		// Model has no explicit genParams, inherit from parent via inheritance chain
		for _, ancestorName := range model.InheritanceChain {
			ancestorRaw := r.getRawModel(ancestorName)
			if ancestorRaw != nil && ancestorRaw.GenParams != nil {
				genParams, err = r.resolveGenParams(ancestorRaw.GenParams)
				if err != nil {
					return ModelsError{
						Type:    ErrTypeResolution,
						Message: fmt.Sprintf("Failed to resolve inherited genParams for %s from %s: %v", name, ancestorName, err),
						Model:   name,
					}
				}
				break // Found genParams, stop searching
			}
		}
	}

	model.GenParams = genParams

	// Resolve fallback
	if rawModel.Fallback != "" {
		fallbackModel, err := r.resolveModelReference(nil, rawModel.Fallback)
		if err != nil {
			return ModelsError{
				Type:    ErrTypeResolution,
				Message: fmt.Sprintf("Failed to resolve fallback '%s' for %s: %v", rawModel.Fallback, name, err),
				Model:   name,
			}
		}
		model.Fallback = fallbackModel
	}

	// Resolve subagents (only for non-abstract models)
	if !strings.HasPrefix(name, "$") {
		subagents := make(map[string]*Model)
		// Walk through inheritance chain to collect subagents
		for _, parentModelName := range model.InheritanceChain {
			parentRaw := r.getRawModel(parentModelName)
			if parentRaw == nil {
				continue
			}
			for task, subagentRef := range parentRaw.Subagents {
				if _, exists := subagents[task]; exists {
					continue // Already resolved from a child model
				}
				subagentModel, err := r.resolveModelReference(model, subagentRef)
				if err != nil {
					return ModelsError{
						Type:    ErrTypeResolution,
						Message: fmt.Sprintf("Failed to resolve subagent '%s' for task '%s' in %s: %v", subagentRef, task, name, err),
						Model:   name,
						Context: map[string]interface{}{"task": task, "subagent": subagentRef},
					}
				}
				subagents[task] = subagentModel
			}
		}
		model.Subagents = subagents
	}

	return nil
}

// getRawModel retrieves the raw model data from config
func (r *ModelsRegistry) getRawModel(name string) *RawModel {
	if r.rawConfig == nil {
		return nil
	}

	raw, exists := r.rawConfig.Models[name]
	if !exists {
		return nil
	}

	// Handle aliases
	if alias, isAlias := raw.(string); isAlias {
		return r.getRawModel(alias)
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

// resolveInheritance resolves the inheritance chain for a model
func (r *ModelsRegistry) resolveInheritance(model *Model, modelName string) error {
	visited := make(map[string]bool)
	return r.resolveInheritanceRecursive(model, modelName, visited)
}

func (r *ModelsRegistry) resolveInheritanceRecursive(model *Model, modelName string, visited map[string]bool) error {
	if visited[modelName] {
		return ModelsError{
			Type:    ErrTypeValidation,
			Message: fmt.Sprintf("Inheritance cycle detected: %s", strings.Join(model.InheritanceChain, " -> ")),
			Model:   modelName,
			Context: map[string]interface{}{"chain": model.InheritanceChain},
		}
	}

	visited[modelName] = true

	rawModel := r.getRawModel(modelName)
	if rawModel == nil {
		return ModelsError{
			Type:    ErrTypeNotFound,
			Message: fmt.Sprintf("Model not found: %s", modelName),
			Model:   modelName,
		}
	}

	// If this model extends another, resolve parent first
	if rawModel.Extends != "" {
		parentModel, err := r.resolveModelReference(nil, rawModel.Extends)
		if err != nil {
			return ModelsError{
				Type:    ErrTypeResolution,
				Message: fmt.Sprintf("Failed to resolve parent '%s' for %s: %v", rawModel.Extends, modelName, err),
				Model:   modelName,
			}
		}

		// Recursively resolve parent inheritance
		if err := r.resolveInheritanceRecursive(parentModel, rawModel.Extends, visited); err != nil {
			return err
		}

		// Merge parent into child (arrays completely replaced)
		r.mergeModel(parentModel, model, modelName)
		// Child should be at the front of inheritance chain, followed by parent's chain
		model.InheritanceChain = append([]string{modelName}, parentModel.InheritanceChain...)
	}

	delete(visited, modelName)
	return nil
}

// getBoolValue gets the boolean value from a tristate pointer
func (r *ModelsRegistry) getBoolValue(ptr *bool, defaultValue bool) bool {
	if ptr == nil {
		return defaultValue
	}
	return *ptr
}

// getIntValue gets the integer value from a tristate pointer
func (r *ModelsRegistry) getIntValue(ptr *int, defaultValue int) int {
	if ptr == nil {
		return defaultValue
	}
	return *ptr
}

// mergeModel merges parent model into child model (child overrides parent)
func (r *ModelsRegistry) mergeModel(parent, child *Model, childName string) {
	// Merge non-array fields (child overrides parent if present)
	if child.Providers == nil {
		child.Providers = parent.Providers
	}

	if child.ModelName == "" {
		child.ModelName = parent.ModelName
	}

	// Get raw models to check if child explicitly set the values
	childRaw := r.getRawModel(childName)

	// Only override if child didn't explicitly set the value (nil)
	if childRaw != nil {
		if childRaw.IgnoreSystemPrompt == nil {
			child.IgnoreSystemPrompt = parent.IgnoreSystemPrompt
		}
		if childRaw.ThoughtEnabled == nil {
			child.ThoughtEnabled = parent.ThoughtEnabled
		}
		if childRaw.ToolSupported == nil {
			child.ToolSupported = parent.ToolSupported
		}
		if childRaw.MaxTokens == nil {
			child.MaxTokens = parent.MaxTokens
		}
	}

	if child.ResponseModalities == nil {
		child.ResponseModalities = parent.ResponseModalities
	}

	// Set fallback to parent's fallback if child doesn't have one
	if child.Fallback == nil {
		child.Fallback = parent.Fallback
	}

	// Note: GenParams are now handled in resolveModelFromRaw
}

// resolveModelReference resolves a model reference with inheritance-aware subagent path resolution
func (r *ModelsRegistry) resolveModelReference(baseModel *Model, name string) (*Model, error) {
	// 1. Check if it's an alias first
	if alias, exists := r.Aliases[name]; exists {
		name = alias
	}

	// 2. Check if direct model exists (after alias resolution)
	if model, exists := r.Models[name]; exists {
		return model, nil
	}

	// 3. Handle any name containing "/" (the last one used if multiple) with inheritance semantics
	sep := strings.LastIndexByte(name, '/')
	if sep == -1 {
		return nil, ModelsError{
			Type:    ErrTypeNotFound,
			Message: fmt.Sprintf("Model reference not found: %s", name),
			Context: map[string]interface{}{"reference": name},
		}
	}
	parentName, subagentPath := name[:sep], name[sep:]

	var parentModel *Model
	if parentName == "" {
		parentModel = baseModel
	} else {
		parentModel = r.Models[parentName]
	}
	if parentModel == nil {
		return nil, ModelsError{
			Type:    ErrTypeValidation,
			Message: fmt.Sprintf("Path with '/' requires parent model context: %s", name),
			Context: map[string]interface{}{"reference": name},
		}
	}

	// Traverse parentModel's inheritance chain and try each ancestor/subagentPath
	for _, ancestorName := range parentModel.InheritanceChain {
		targetName := ancestorName + subagentPath
		if targetModel, exists := r.Models[targetName]; exists {
			return targetModel, nil
		}
	}

	return nil, ModelsError{
		Type:    ErrTypeNotFound,
		Message: fmt.Sprintf("Subagent not found in inheritance chain: %s", name),
		Context: map[string]interface{}{
			"subagentPath":     name,
			"parentModel":      parentModel.Name,
			"inheritanceChain": parentModel.InheritanceChain,
		},
	}
}

// resolveGenParams resolves genParams from reference or inline object
func (r *ModelsRegistry) resolveGenParams(raw interface{}) (GenerationParams, error) {
	switch v := raw.(type) {
	case string:
		if paramSet, exists := r.rawConfig.GenParams[v]; exists {
			return r.convertToGenParams(paramSet)
		} else {
			return GenerationParams{}, ModelsError{
				Type:    ErrTypeNotFound,
				Message: fmt.Sprintf("genParams reference not found: %s", v),
				Context: map[string]interface{}{"reference": v},
			}
		}
	case map[string]interface{}:
		// Inline genParams object
		return r.convertToGenParams(v)
	default:
		return GenerationParams{}, ModelsError{
			Type:    ErrTypeValidation,
			Message: fmt.Sprintf("Invalid genParams type: %T", raw),
		}
	}
}

// convertToGenParams converts raw interface{} to GenerationParams with strict type validation
func (r *ModelsRegistry) convertToGenParams(raw interface{}) (GenerationParams, error) {
	paramMap, ok := raw.(map[string]interface{})
	if !ok {
		return GenerationParams{}, ModelsError{
			Type:    ErrTypeValidation,
			Message: fmt.Sprintf("genParams must be an object, got %T", raw),
		}
	}

	var genParams GenerationParams

	// Direct JSON unmarshal to the GenerationParams struct
	jsonBytes, err := json.Marshal(paramMap)
	if err != nil {
		return GenerationParams{}, ModelsError{
			Type:    ErrTypeParse,
			Message: fmt.Sprintf("Failed to marshal genParams: %v", err),
		}
	}

	if err := json.Unmarshal(jsonBytes, &genParams); err != nil {
		return GenerationParams{}, ModelsError{
			Type:    ErrTypeParse,
			Message: fmt.Sprintf("Failed to unmarshal genParams: %v", err),
		}
	}

	return genParams, nil
}

// validate performs comprehensive validation of the resolved models
func (r *ModelsRegistry) validate() error {
	var errors []ModelsError

	// Validate displayOrder references
	for _, modelName := range r.DisplayOrder {
		if _, exists := r.Models[modelName]; !exists {
			errors = append(errors, ModelsError{
				Type:    ErrTypeValidation,
				Message: fmt.Sprintf("Model in displayOrder not found: %s", modelName),
				Context: map[string]interface{}{"displayOrder": true},
			})
		}
	}

	// Validate that all models have proper inheritance chains resolved
	for name, model := range r.Models {
		if len(model.InheritanceChain) == 0 {
			errors = append(errors, ModelsError{
				Type:    ErrTypeValidation,
				Message: fmt.Sprintf("Model has empty inheritance chain: %s", name),
				Model:   name,
			})
		}
	}

	// Validate aliases point to existing models
	for alias, target := range r.Aliases {
		if _, exists := r.Models[target]; !exists {
			// Check if target is also an alias
			if _, targetExists := r.Aliases[target]; !targetExists {
				errors = append(errors, ModelsError{
					Type:    ErrTypeValidation,
					Message: fmt.Sprintf("Alias target not found: %s -> %s", alias, target),
					Context: map[string]interface{}{"alias": alias, "target": target},
				})
			}
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("validation failed with %d errors", len(errors))
	}

	return nil
}

// GetModel retrieves a model by name or alias
func (r *ModelsRegistry) GetModel(name string) (*Model, bool) {
	// Check if it's an alias
	if alias, exists := r.Aliases[name]; exists {
		name = alias
	}

	model, exists := r.Models[name]
	return model, exists
}

// Hash generates a unique hash for OpenAI config to detect changes
func (config *OpenAIConfig) Hash() string {
	hasher := sha256.New()
	hasher.Write([]byte(config.Endpoint))
	hasher.Write([]byte(config.APIKey))
	hasher.Write([]byte(fmt.Sprintf("%v", config.Enabled)))
	return hex.EncodeToString(hasher.Sum(nil))
}

// InitializeOpenAIEndpoints sets up OpenAI providers from database configs
func (r *ModelsRegistry) InitializeOpenAIEndpoints(db *sql.DB) error {
	configs, err := GetOpenAIConfigs(db)
	if err != nil {
		return fmt.Errorf("failed to get OpenAI configs: %w", err)
	}

	r.mutex.Lock()
	defer r.mutex.Unlock()

	// Clear existing OpenAI endpoints
	for _, endpoint := range r.openAIEndpoints {
		// Remove models from provider mapping
		for _, model := range endpoint.models {
			delete(r.providers, model.ID)
		}
	}
	r.openAIEndpoints = make(map[string]*OpenAIEndpoint)

	// Create new endpoints
	for _, config := range configs {
		if !config.Enabled {
			continue
		}

		if err := r.createOpenAIEndpoint(&config); err != nil {
			// Log error but continue with other configs
			fmt.Printf("Failed to create OpenAI endpoint for %s: %v\n", config.Name, err)
			continue
		}
	}

	return nil
}

// createOpenAIEndpoint creates a new OpenAI endpoint
func (r *ModelsRegistry) createOpenAIEndpoint(config *OpenAIConfig) error {
	hash := config.Hash()
	if _, exists := r.openAIEndpoints[hash]; exists {
		return fmt.Errorf("endpoint with hash %s already exists", hash)
	}

	// Create OpenAI client
	client := NewOpenAIClient(config)

	// Get available models
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	models, err := client.GetModels(ctx)
	if err != nil {
		return fmt.Errorf("failed to get models for %s: %w", config.Name, err)
	}

	// Create endpoint
	endpoint := &OpenAIEndpoint{
		config:     config,
		provider:   client,
		models:     models,
		lastUpdate: time.Now(),
		hash:       hash,
	}

	r.openAIEndpoints[hash] = endpoint

	// Register models with provider
	for _, model := range models {
		r.providers[model.ID] = client
	}

	return nil
}

// UpdateOpenAIEndpoints updates OpenAI providers when configs change
func (r *ModelsRegistry) UpdateOpenAIEndpoints(db *sql.DB) error {
	configs, err := GetOpenAIConfigs(db)
	if err != nil {
		return fmt.Errorf("failed to get OpenAI configs: %w", err)
	}

	r.mutex.Lock()
	defer r.mutex.Unlock()

	// Create maps for efficient lookup
	newConfigs := make(map[string]*OpenAIConfig)
	for i := range configs {
		newConfigs[configs[i].ID] = &configs[i]
	}

	// Track which hashes are still needed
	neededHashes := make(map[string]bool)

	// Process new/updated configs
	for _, config := range configs {
		if !config.Enabled {
			continue
		}

		hash := config.Hash()
		neededHashes[hash] = true

		if endpoint, exists := r.openAIEndpoints[hash]; exists {
			// Update existing endpoint config reference
			endpoint.config = &config
		} else {
			// Create new endpoint
			if err := r.createOpenAIEndpointUnsafe(&config); err != nil {
				fmt.Printf("Failed to create OpenAI endpoint for %s: %v\n", config.Name, err)
				continue
			}
		}
	}

	// Remove unused endpoints
	r.cleanupUnusedEndpointsUnsafe(neededHashes)

	// Update model provider mappings
	r.updateModelProvidersUnsafe()

	return nil
}

// createOpenAIEndpointUnsafe creates endpoint without mutex lock (internal use)
func (r *ModelsRegistry) createOpenAIEndpointUnsafe(config *OpenAIConfig) error {
	hash := config.Hash()

	client := NewOpenAIClient(config)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	models, err := client.GetModels(ctx)
	if err != nil {
		return fmt.Errorf("failed to get models for %s: %w", config.Name, err)
	}

	endpoint := &OpenAIEndpoint{
		config:     config,
		provider:   client,
		models:     models,
		lastUpdate: time.Now(),
		hash:       hash,
	}

	r.openAIEndpoints[hash] = endpoint

	for _, model := range models {
		r.providers[model.ID] = client
	}

	return nil
}

// cleanupUnusedEndpointsUnsafe removes endpoints that are no longer needed (internal use)
func (r *ModelsRegistry) cleanupUnusedEndpointsUnsafe(neededHashes map[string]bool) {
	for hash, endpoint := range r.openAIEndpoints {
		if !neededHashes[hash] {
			// Remove models from provider mapping
			for _, model := range endpoint.models {
				delete(r.providers, model.ID)
			}
			delete(r.openAIEndpoints, hash)
		}
	}
}

// updateModelProvidersUnsafe updates the model to provider mappings (internal use)
func (r *ModelsRegistry) updateModelProvidersUnsafe() {
	// Clear existing non-Gemini providers (but preserve angel-eval)
	for model, provider := range r.providers {
		if provider != r.geminiProvider && model != AngelEvalModelName {
			delete(r.providers, model)
		}
	}

	// Re-add OpenAI models
	for _, endpoint := range r.openAIEndpoints {
		for _, model := range endpoint.models {
			r.providers[model.ID] = endpoint.provider
		}
	}
}

// SetAngelEvalProvider sets the angel-eval provider
func (r *ModelsRegistry) SetAngelEvalProvider(provider LLMProvider) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.providers[AngelEvalModelName] = provider
}

// SetGeminiCodeAssistClient creates and sets the Gemini provider with the given CodeAssistClient
func (r *ModelsRegistry) SetGeminiCodeAssistClient(client *CodeAssistClient) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	// Collect all Gemini models
	geminiModels := make(map[string]*Model)
	for _, model := range r.Models {
		if r.isGeminiModelUnsafe(model) {
			geminiModels[model.Name] = model
		}
	}

	// Create a single provider instance that will be shared by all Gemini models
	r.geminiProvider = NewCodeAssistProvider(geminiModels, client)

	// Register all Gemini models with the provider
	for modelName := range geminiModels {
		r.providers[modelName] = r.geminiProvider
	}
}

// SetGeminiProvider sets a custom LLM provider for Gemini models (for testing)
func (r *ModelsRegistry) SetGeminiProvider(provider LLMProvider) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.geminiProvider = provider

	// Register all Gemini models with the provider
	for _, model := range r.Models {
		if r.isGeminiModelUnsafe(model) {
			r.providers[model.Name] = provider
		}
	}
}

// GetProvider returns the provider for a model name
func (r *ModelsRegistry) GetProvider(modelName string) LLMProvider {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	return r.providers[modelName]
}

// Clear removes all providers and resets the registry
func (r *ModelsRegistry) Clear() {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.providers = make(map[string]LLMProvider)
	r.openAIEndpoints = make(map[string]*OpenAIEndpoint)
	r.geminiProvider = nil
}

// ClearGeminiProviders removes only Gemini-related providers, preserving others
func (r *ModelsRegistry) ClearGeminiProviders() {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	// Preserve angel-eval and other non-Gemini providers
	angelEvalProvider := r.providers[AngelEvalModelName]
	otherProviders := make(map[string]LLMProvider)
	for model, provider := range r.providers {
		// Only preserve non-Gemini providers
		if provider != r.geminiProvider {
			otherProviders[model] = provider
		}
	}

	// Clear and restore non-Gemini providers
	r.providers = otherProviders
	if angelEvalProvider != nil {
		r.providers[AngelEvalModelName] = angelEvalProvider
	}
	// Clear only Gemini-specific endpoints
	r.openAIEndpoints = make(map[string]*OpenAIEndpoint)
	r.geminiProvider = nil
}

// IsEmpty returns true if no providers are registered
func (r *ModelsRegistry) IsEmpty() bool {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	return r.geminiProvider == nil && len(r.providers) == 0
}

// isGeminiModelUnsafe checks if a model is a Gemini model (internal use, no mutex)
func (r *ModelsRegistry) isGeminiModelUnsafe(model *Model) bool {
	// Check if model has Gemini providers
	for _, provider := range model.Providers {
		if provider == "gemini" || provider == "vertexai" {
			return true
		}
	}

	// Also check model name patterns
	return strings.HasPrefix(model.Name, "gemini-") ||
		strings.HasPrefix(model.Name, "vertexai-")
}

// GetAllModels returns all non-subagent models in display order
func (r *ModelsRegistry) GetAllModels() []*Model {
	var models []*Model
	seen := make(map[string]bool)

	// First add models in displayOrder
	for _, name := range r.DisplayOrder {
		if model, exists := r.Models[name]; exists {
			if !seen[name] && !strings.Contains(name, "/") {
				models = append(models, model)
				seen[name] = true
			}
		}
	}

	// Then add any remaining models not in displayOrder
	for name, model := range r.Models {
		if !seen[name] && !strings.Contains(name, "/") {
			models = append(models, model)
			seen[name] = true
		}
	}

	return models
}

// Validate performs validation and returns detailed errors
func (r *ModelsRegistry) Validate() []ModelsError {
	var errors []ModelsError

	// Validate displayOrder references
	for _, modelName := range r.DisplayOrder {
		if _, exists := r.Models[modelName]; !exists {
			errors = append(errors, ModelsError{
				Type:    ErrTypeValidation,
				Message: fmt.Sprintf("Model in displayOrder not found: %s", modelName),
				Context: map[string]interface{}{"displayOrder": true},
			})
		}
	}

	// Validate inheritance chains
	for name, model := range r.Models {
		if len(model.InheritanceChain) == 0 {
			errors = append(errors, ModelsError{
				Type:    ErrTypeValidation,
				Message: fmt.Sprintf("Model has empty inheritance chain: %s", name),
				Model:   name,
			})
		}
	}

	// Validate aliases
	for alias, target := range r.Aliases {
		if _, exists := r.Models[target]; !exists {
			if _, targetExists := r.Aliases[target]; !targetExists {
				errors = append(errors, ModelsError{
					Type:    ErrTypeValidation,
					Message: fmt.Sprintf("Alias target not found: %s -> %s", alias, target),
					Context: map[string]interface{}{"alias": alias, "target": target},
				})
			}
		}
	}

	return errors
}
