package llm

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fvbommel/sortorder"

	"github.com/lifthrasiir/angel/internal/database"
	. "github.com/lifthrasiir/angel/internal/llm/spec"
	. "github.com/lifthrasiir/angel/internal/types"
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

// Model represents a resolved model with its properties
type Model struct {
	Name               string
	ModelName          string          // Internal model name for the provider
	ProviderModels     []ProviderModel // Parsed provider-model specifications
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

type Models struct {
	specRegistry *SpecRegistry
	displayOrder []string

	// Provider management
	// Map of provider type -> LLMProvider
	// For OpenAI-compatible providers, the provider type is the endpoint URL
	llmProviders map[string]LLMProvider

	// Map of external model name -> ModelProvider
	// This is built from the spec registry and registered providers
	modelProviders map[string]ModelProvider

	// Legacy fields for compatibility with openai.go
	// providers is an alias for modelProviders
	// builtinModels is derived from specRegistry.ModelSpecs
	providers     map[string]ModelProvider
	builtinModels map[string]*Model

	openAIEndpoints map[string]*OpenAIEndpoint // config hash -> endpoint

	// Thread safety
	mutex sync.RWMutex
}

// LoadModels loads and parses the models.json file using the spec package
func LoadModels(data []byte) (*Models, error) {
	specRegistry, err := LoadSpecs(data)
	if err != nil {
		return nil, ModelsError{
			Type:    ErrTypeParse,
			Message: fmt.Sprintf("Failed to load model specs: %v", err),
			Context: map[string]interface{}{},
		}
	}

	registry := &Models{
		specRegistry:    specRegistry,
		displayOrder:    specRegistry.DisplayOrder,
		llmProviders:    make(map[string]LLMProvider),
		modelProviders:  make(map[string]ModelProvider),
		providers:       make(map[string]ModelProvider), // Alias for modelProviders
		builtinModels:   make(map[string]*Model),        // Derived from specRegistry
		openAIEndpoints: make(map[string]*OpenAIEndpoint),
	}

	// Build builtinModels from specRegistry
	for name, spec := range specRegistry.ModelSpecs {
		registry.builtinModels[name] = &Model{
			Name:               spec.Name,
			ModelName:          spec.ModelName,
			ProviderModels:     spec.ProviderModels,
			GenParams:          spec.GenParams,
			IgnoreSystemPrompt: spec.IgnoreSystemPrompt,
			ThoughtEnabled:     spec.ThoughtEnabled,
			ToolSupported:      spec.ToolSupported,
			ResponseModalities: spec.ResponseModalities,
			MaxTokens:          spec.MaxTokens,
			InheritanceChain:   spec.InheritanceChain,
		}
	}

	// Validation phase
	if err := registry.validate(); err != nil {
		return nil, err
	}

	return registry, nil
}

// validate performs comprehensive validation of the loaded models
func (r *Models) validate() error {
	var errors []ModelsError

	// Validate displayOrder references
	if errs := r.specRegistry.ValidateDisplayOrder(); len(errs) > 0 {
		for _, e := range errs {
			errors = append(errors, ModelsError{
				Type:    ErrTypeValidation,
				Message: e.Error(),
				Context: map[string]interface{}{"displayOrder": true},
			})
		}
	}

	// Validate inheritance chains
	if errs := r.specRegistry.ValidateInheritanceChains(); len(errs) > 0 {
		for _, e := range errs {
			errors = append(errors, ModelsError{
				Type:    ErrTypeValidation,
				Message: e.Error(),
			})
		}
	}

	// Validate aliases
	if errs := r.specRegistry.ValidateAliases(); len(errs) > 0 {
		for _, e := range errs {
			errors = append(errors, ModelsError{
				Type:    ErrTypeValidation,
				Message: e.Error(),
			})
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("validation failed with %d errors", len(errors))
	}

	return nil
}

// SetLLMProvider registers an LLMProvider for a given provider type
// The provider type can be:
// - "geminicli" for Gemini Code Assist API
// - "antigravity" for custom Gemini endpoint
// - An endpoint URL for OpenAI-compatible providers
// - "" (empty string) for wildcard providers that match any provider
func (r *Models) SetLLMProvider(providerType string, provider LLMProvider) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.llmProviders[providerType] = provider

	// Rebuild model providers that depend on this provider type
	r.rebuildModelProvidersUnsafe()
}

// rebuildModelProvidersUnsafe rebuilds the model providers map (internal use, no mutex)
func (r *Models) rebuildModelProvidersUnsafe() {
	// Clear existing model providers for spec-based models
	for name := range r.modelProviders {
		if r.specRegistry.GetModelSpec(name) != nil {
			delete(r.modelProviders, name)
		}
	}

	// Rebuild model providers from specs
	for _, modelSpec := range r.specRegistry.ModelSpecs {
		r.buildModelProviderForSpecUnsafe(modelSpec.Name)

		// Build model providers for variants
		for variantName := range modelSpec.Variants {
			variantModelName := modelSpec.Name + variantName
			r.buildModelProviderForSpecUnsafe(variantModelName)
		}
	}

	// Sync providers (legacy compatibility)
	r.providers = r.modelProviders
}

// buildModelProviderForSpecUnsafe builds a ModelProvider for a given external model name (internal use, no mutex)
func (r *Models) buildModelProviderForSpecUnsafe(externalName string) {
	tuples := r.specRegistry.GetProviderTuples(externalName)
	if len(tuples) == 0 {
		return
	}

	// Get the ModelSpec for this external name
	modelSpec := r.specRegistry.GetModelSpec(externalName)

	var providers []ModelProvider
	for _, tuple := range tuples {
		// Find LLMProvider for this provider type
		llmProvider, exists := r.llmProviders[tuple.ProviderType]
		if !exists {
			// Try wildcard provider
			llmProvider, exists = r.llmProviders[""]
			if !exists {
				continue // No provider available for this type
			}
		}

		// Create ModelProvider with the internal model name and spec
		providers = append(providers, newModelProviderWithSpec(llmProvider, tuple.ModelName, modelSpec))
	}

	if len(providers) > 0 {
		if len(providers) == 1 {
			r.modelProviders[externalName] = providers[0]
		} else {
			r.modelProviders[externalName] = NewModelProviderChain(providers, externalName)
		}
	}
}

// GetModel retrieves a model spec by name or alias
func (r *Models) GetModel(name string) *Model {
	spec := r.specRegistry.GetModelSpec(name)
	if spec == nil {
		return nil
	}

	// Build Model from ModelSpec
	model := &Model{
		Name:               spec.Name,
		ModelName:          spec.ModelName,
		ProviderModels:     spec.ProviderModels,
		GenParams:          spec.GenParams,
		IgnoreSystemPrompt: spec.IgnoreSystemPrompt,
		ThoughtEnabled:     spec.ThoughtEnabled,
		ToolSupported:      spec.ToolSupported,
		ResponseModalities: spec.ResponseModalities,
		MaxTokens:          spec.MaxTokens,
		InheritanceChain:   spec.InheritanceChain,
	}

	// Resolve fallback
	if spec.Fallback != "" {
		if fallbackSpec := r.specRegistry.GetModelSpec(spec.Fallback); fallbackSpec != nil {
			fallbackModel := &Model{
				Name:               fallbackSpec.Name,
				ModelName:          fallbackSpec.ModelName,
				ProviderModels:     fallbackSpec.ProviderModels,
				GenParams:          fallbackSpec.GenParams,
				IgnoreSystemPrompt: fallbackSpec.IgnoreSystemPrompt,
				ThoughtEnabled:     fallbackSpec.ThoughtEnabled,
				ToolSupported:      fallbackSpec.ToolSupported,
				ResponseModalities: fallbackSpec.ResponseModalities,
				MaxTokens:          fallbackSpec.MaxTokens,
				InheritanceChain:   fallbackSpec.InheritanceChain,
			}
			model.Fallback = fallbackModel
		}
	}

	// Resolve subagents
	if len(spec.Subagents) > 0 {
		model.Subagents = make(map[string]*Model)
		for task, subagentRef := range spec.Subagents {
			if subagentSpec := r.specRegistry.GetModelSpec(subagentRef); subagentSpec != nil {
				subagentModel := &Model{
					Name:               subagentSpec.Name,
					ModelName:          subagentSpec.ModelName,
					ProviderModels:     subagentSpec.ProviderModels,
					GenParams:          subagentSpec.GenParams,
					IgnoreSystemPrompt: subagentSpec.IgnoreSystemPrompt,
					ThoughtEnabled:     subagentSpec.ThoughtEnabled,
					ToolSupported:      subagentSpec.ToolSupported,
					ResponseModalities: subagentSpec.ResponseModalities,
					MaxTokens:          subagentSpec.MaxTokens,
					InheritanceChain:   subagentSpec.InheritanceChain,
				}
				model.Subagents[task] = subagentModel
			}
		}
	}

	return model
}

// InitializeOpenAIEndpoints sets up OpenAI providers from database configs
func (r *Models) InitializeOpenAIEndpoints(db *database.Database) error {
	configs, err := database.GetOpenAIConfigs(db)
	if err != nil {
		return fmt.Errorf("failed to get OpenAI configs: %w", err)
	}

	r.mutex.Lock()
	defer r.mutex.Unlock()

	// Clear existing OpenAI endpoints
	for _, endpoint := range r.openAIEndpoints {
		// Remove provider
		delete(r.llmProviders, endpoint.config.Endpoint)
	}
	r.openAIEndpoints = make(map[string]*OpenAIEndpoint)

	// Create new endpoints
	for _, config := range configs {
		if !config.Enabled {
			continue
		}

		if err := r.createOpenAIEndpointUnsafe(&config); err != nil {
			// Log error but continue with other configs
			fmt.Printf("Failed to create OpenAI endpoint for %s: %v\n", config.Name, err)
			continue
		}
	}

	// Rebuild model providers after updating endpoints
	r.rebuildModelProvidersUnsafe()

	return nil
}

// createOpenAIEndpointUnsafe creates a new OpenAI endpoint without mutex (internal use)
func (r *Models) createOpenAIEndpointUnsafe(config *OpenAIConfig) error {
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

	// Register LLMProvider with endpoint URL as provider type
	r.llmProviders[config.Endpoint] = client

	return nil
}

// UpdateOpenAIEndpoints updates OpenAI providers when configs change
func (r *Models) UpdateOpenAIEndpoints(db *database.Database) error {
	configs, err := database.GetOpenAIConfigs(db)
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

		if _, exists := r.openAIEndpoints[hash]; exists {
			// Update existing endpoint config reference
			r.openAIEndpoints[hash].config = &config
		} else {
			// Create new endpoint
			if err := r.createOpenAIEndpointUnsafe(&config); err != nil {
				fmt.Printf("Failed to create OpenAI endpoint for %s: %v\n", config.Name, err)
				continue
			}
		}
	}

	// Remove unused endpoints
	for hash, endpoint := range r.openAIEndpoints {
		if !neededHashes[hash] {
			// Remove provider
			delete(r.llmProviders, endpoint.config.Endpoint)
			delete(r.openAIEndpoints, hash)
		}
	}

	// Rebuild model providers after updating endpoints
	r.rebuildModelProvidersUnsafe()

	return nil
}

// GetProvider returns the ModelProvider for a given external model name
func (r *Models) GetProvider(modelName string) ModelProvider {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	return r.modelProviders[modelName]
}

// GetModelProvider returns a ModelProvider wrapper for a given model name
func (r *Models) GetModelProvider(modelName string) (ModelProvider, error) {
	provider := r.GetProvider(modelName)
	if provider == nil {
		return nil, fmt.Errorf("unsupported model: %s", modelName)
	}
	return provider, nil
}

// Clear removes all providers and resets the registry
func (r *Models) Clear() {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.llmProviders = make(map[string]LLMProvider)
	r.modelProviders = make(map[string]ModelProvider)
	r.openAIEndpoints = make(map[string]*OpenAIEndpoint)
}

// IsEmpty returns true if no providers are registered
func (r *Models) IsEmpty() bool {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	return len(r.llmProviders) == 0
}

// GetAllModels returns all non-subagent models in display order
func (r *Models) GetAllModels() []*Model {
	var models []*Model
	seen := make(map[string]bool)

	// First add models in displayOrder
	for _, name := range r.displayOrder {
		if model := r.GetModel(name); model != nil {
			models = append(models, model)
			seen[name] = true
		}
	}

	// Then add any remaining models from modelProviders
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	var otherModels []*Model
	for name := range r.modelProviders {
		if !seen[name] && !strings.HasPrefix(name, "$") {
			provider := r.modelProviders[name]
			maxTokens := provider.MaxTokens()
			otherModels = append(otherModels, &Model{Name: name, MaxTokens: maxTokens})
			seen[name] = true
		}
	}

	// Sort other models by name in natural ascending order
	sort.Slice(otherModels, func(i, j int) bool {
		return sortorder.NaturalLess(otherModels[i].Name, otherModels[j].Name)
	})

	return append(models, otherModels...)
}

// Validate performs validation and returns detailed errors
func (r *Models) Validate() []ModelsError {
	var errors []ModelsError

	// Validate displayOrder references
	if errs := r.specRegistry.ValidateDisplayOrder(); len(errs) > 0 {
		for _, e := range errs {
			errors = append(errors, ModelsError{
				Type:    ErrTypeValidation,
				Message: e.Error(),
				Context: map[string]interface{}{"displayOrder": true},
			})
		}
	}

	// Validate inheritance chains
	if errs := r.specRegistry.ValidateInheritanceChains(); len(errs) > 0 {
		for _, e := range errs {
			errors = append(errors, ModelsError{
				Type:    ErrTypeValidation,
				Message: e.Error(),
			})
		}
	}

	// Validate aliases
	if errs := r.specRegistry.ValidateAliases(); len(errs) > 0 {
		for _, e := range errs {
			errors = append(errors, ModelsError{
				Type:    ErrTypeValidation,
				Message: e.Error(),
				Context: map[string]interface{}{"alias": e.Error()},
			})
		}
	}

	return errors
}

// ModelGenerationParams returns the generation parameters for a given model name
func (r *Models) ModelGenerationParams(modelName string) (GenerationParams, error) {
	spec := r.specRegistry.GetModelSpec(modelName)
	if spec == nil {
		return GenerationParams{}, ModelsError{
			Type:    ErrTypeNotFound,
			Message: fmt.Sprintf("Model not found: %s", modelName),
			Model:   modelName,
		}
	}

	return spec.GenParams, nil
}

// ResolveSubagent resolves a subagent for a given model and task, returning the ModelProvider
func (r *Models) ResolveSubagent(modelName string, task string) (ModelProvider, error) {
	spec := r.specRegistry.GetModelSpec(modelName)
	if spec == nil {
		return nil, ModelsError{
			Type:    ErrTypeNotFound,
			Message: fmt.Sprintf("Model not found: %s", modelName),
			Model:   modelName,
		}
	}

	// Check if model spec has the requested subagent
	subagentRef, exists := spec.Subagents[task]
	if !exists {
		return nil, ModelsError{
			Type:    ErrTypeNotFound,
			Message: fmt.Sprintf("Subagent not found for task '%s' in model '%s'", task, modelName),
			Model:   modelName,
			Context: map[string]interface{}{"task": task},
		}
	}

	// Handle relative paths (starting with /)
	// If subagentRef starts with "/", it's a relative path from the current model
	resolvedRef := subagentRef
	if strings.HasPrefix(subagentRef, "/") {
		resolvedRef = modelName + subagentRef
	}

	// Get provider for the subagent model
	r.mutex.RLock()
	provider := r.modelProviders[resolvedRef]
	r.mutex.RUnlock()

	if provider == nil {
		return nil, ModelsError{
			Type:    ErrTypeResolution,
			Message: fmt.Sprintf("No provider available for subagent model '%s' (resolved from '%s')", resolvedRef, subagentRef),
			Model:   resolvedRef,
		}
	}

	return provider, nil
}

// GetKnownProviders returns the list of known provider::model specifications
func (r *Models) GetKnownProviders() []string {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	var providers []string
	for provider := range r.specRegistry.KnownProviders {
		providers = append(providers, provider)
	}
	return providers
}
