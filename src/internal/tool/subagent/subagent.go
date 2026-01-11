package subagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"

	. "github.com/lifthrasiir/angel/gemini"
	"github.com/lifthrasiir/angel/internal/chat"
	"github.com/lifthrasiir/angel/internal/database"
	"github.com/lifthrasiir/angel/internal/env"
	"github.com/lifthrasiir/angel/internal/llm"
	"github.com/lifthrasiir/angel/internal/tool"
	. "github.com/lifthrasiir/angel/internal/types"
)

// SubagentResultCollector implements EventWriter to capture chat events and convert them to tool results
type SubagentResultCollector struct {
	responseText       strings.Builder
	attachments        []FileAttachment
	subagentID         string
	functionCalls      []FunctionCall
	hasError           bool
	errorMessage       string
	firstFinishReason  string
	generationComplete bool
	imageGeneration    bool // Flag for image_generation tasks that need hash output
	lastItemWasHash    bool // Track if the last added item was a hash
}

// addWithSpacing adds content with proper spacing between hash and text
func (src *SubagentResultCollector) addWithSpacing(content string, isHash bool) {
	if src.responseText.Len() == 0 {
		// First item, no spacing needed
		src.responseText.WriteString(content)
	} else {
		// Check if we need to add space based on edge trigger
		if src.lastItemWasHash || isHash {
			src.responseText.WriteString(" ")
		}
		src.responseText.WriteString(content)
	}
	src.lastItemWasHash = isHash
}

// newSubagentResultCollector creates a new result collector for subagent operations
func newSubagentResultCollector(subagentID string, imageGeneration bool) *SubagentResultCollector {
	return &SubagentResultCollector{
		subagentID:      subagentID,
		attachments:     make([]FileAttachment, 0),
		functionCalls:   make([]FunctionCall, 0),
		imageGeneration: imageGeneration,
	}
}

// Send implements EventWriter.Send
func (src *SubagentResultCollector) Send(eventType EventType, payload string) {
	src.processEvent(eventType, payload)
}

// Broadcast implements EventWriter.Broadcast
func (src *SubagentResultCollector) Broadcast(eventType EventType, payload string) {
	src.processEvent(eventType, payload)
}

// Acquire implements EventWriter.Acquire (no-op for subagent)
func (src *SubagentResultCollector) Acquire() {}

// Release implements EventWriter.Release (no-op for subagent)
func (src *SubagentResultCollector) Release() {}

// Close implements EventWriter.Close (no-op for subagent)
func (src *SubagentResultCollector) Close() {}

// HeadersSent implements EventWriter.HeadersSent (always false for subagent)
func (src *SubagentResultCollector) HeadersSent() bool {
	return false
}

// processEvent handles different event types and collects relevant data
func (src *SubagentResultCollector) processEvent(eventType EventType, payload string) {
	switch eventType {
	case EventWorkspaceHint, EventInitialState, EventInitialStateNoCall, EventAcknowledge:
		// Ignore, we are not interested in the previous state
	case EventModelMessage:
		// Parse model message: "messageId\ntext"
		if _, after, found := strings.Cut(payload, "\n"); found {
			src.addWithSpacing(after, false) // Text is not a hash
		}
	case EventFunctionCall:
		// Parse function call: [Function name, arguments JSON]
		if name, argsStr, found := strings.Cut(payload, "\n"); found {
			var args map[string]interface{}
			if argsStr != "" {
				if err := json.Unmarshal([]byte(argsStr), &args); err != nil {
					log.Printf("Failed to parse function call args: %v", err)
					args = make(map[string]interface{})
				}
			}
			fc := FunctionCall{
				Name: name,
				Args: args,
			}
			src.functionCalls = append(src.functionCalls, fc)
		}
	case EventFunctionResponse:
		// Parse function response to extract attachments
		// Format: [Function name, FunctionResponsePayload JSON]
		if _, responseStr, found := strings.Cut(payload, "\n"); found {
			var response FunctionResponsePayload
			if err := json.Unmarshal([]byte(responseStr), &response); err == nil {
				src.attachments = append(src.attachments, response.Attachments...)
			}
		}
	case EventInlineData:
		// Parse inline data: [InlineDataPayload JSON]
		var inlineData InlineDataPayload
		if err := json.Unmarshal([]byte(payload), &inlineData); err == nil {
			// Always add attachments to result
			src.attachments = append(src.attachments, inlineData.Attachments...)

			// For image_generation tasks, also add hash to response text
			if src.imageGeneration {
				for _, attachment := range inlineData.Attachments {
					if attachment.Hash != "" {
						src.addWithSpacing(attachment.Hash, true) // Hash is a hash
					}
				}
			}
		}
	case EventPendingConfirmation:
		// Handle pending confirmation - treat as error for subagent
		src.hasError = true
		src.errorMessage = "Tool execution requires user confirmation"
	case EventError:
		// Handle error events
		src.hasError = true
		src.errorMessage = payload
	case EventComplete:
		// Mark generation as complete
		src.generationComplete = true
	case EventThought, EventGenerationChanged, EventSessionName, EventCumulTokenCount, EventFinish:
		// Thoughts and metadata are ignored for subagent results
	}
}

func (src *SubagentResultCollector) SubagentID() string {
	return src.subagentID
}

func (src *SubagentResultCollector) Response() string {
	return src.responseText.String()
}

func (src *SubagentResultCollector) Attachments() []FileAttachment {
	return src.attachments
}

func (src *SubagentResultCollector) FunctionCalls() []FunctionCall {
	return src.functionCalls
}

func (src *SubagentResultCollector) ErrorMessage() string {
	if src.hasError {
		return src.errorMessage
	} else if src.firstFinishReason != "" && src.firstFinishReason != FinishReasonStop {
		return FinishReasonMessage(src.firstFinishReason)
	} else {
		return ""
	}
}

// SubagentTool handles the subagent tool call, allowing to spawn a new subagent or interact with an existing one.
func SubagentTool(ctx context.Context, args map[string]interface{}, params tool.HandlerParams) (tool.HandlerResults, error) {
	if err := tool.EnsureKnownKeys("subagent", args, "subagent_id", "system_prompt", "text"); err != nil {
		return tool.HandlerResults{}, err
	}

	subagentID, hasSubagentID := args["subagent_id"].(string)
	systemPrompt, hasSystemPrompt := args["system_prompt"].(string)
	text, hasText := args["text"].(string)

	if hasSubagentID && hasSystemPrompt {
		return tool.HandlerResults{}, fmt.Errorf("subagent tool: cannot provide both 'subagent_id' and 'system_prompt'")
	}
	if !hasSubagentID && !hasSystemPrompt {
		return tool.HandlerResults{}, fmt.Errorf("subagent tool: must provide either 'subagent_id' or 'system_prompt'")
	}
	if !hasText {
		return tool.HandlerResults{}, fmt.Errorf("subagent tool: must provide 'text'")
	}

	db, err := database.FromContext(ctx)
	if err != nil {
		return tool.HandlerResults{}, err
	}
	models, err := llm.ModelsFromContext(ctx)
	if err != nil {
		return tool.HandlerResults{}, err
	}
	ga, err := llm.GeminiAuthFromContext(ctx)
	if err != nil {
		return tool.HandlerResults{}, err
	}
	tools, err := tool.FromContext(ctx)
	if err != nil {
		return tool.HandlerResults{}, err
	}
	config, err := env.EnvConfigFromContext(ctx)
	if err != nil {
		return tool.HandlerResults{}, err
	}

	// Check if the current session is already a subagent session
	if IsSubsessionId(params.SessionId) {
		return tool.HandlerResults{}, errors.New("subagent tool cannot be called from a subagent session")
	}

	mainDb, err := db.WithSession(params.SessionId)
	if err != nil {
		return tool.HandlerResults{}, err
	}
	defer mainDb.Close()

	var subsessionID string
	var newAgentID string
	var subDb *database.SessionDatabase

	if hasSystemPrompt {
		// Spawn a new subagent
		newAgentID = database.GenerateID()
		subsessionID = fmt.Sprintf("%s.%s", params.SessionId, newAgentID)
		subDb = mainDb.WithSuffix("." + newAgentID)

		// Create new subagent session with the provided system prompt
		subDb, _, err = database.CreateSession(subDb.Database, subsessionID, systemPrompt, "")
		if err != nil {
			return tool.HandlerResults{}, fmt.Errorf("failed to create new subagent session with ID %s: %w", subsessionID, err)
		}
	} else {
		// Interact with an existing subagent
		subsessionID = fmt.Sprintf("%s.%s", params.SessionId, subagentID)
		subDb = mainDb.WithSuffix("." + subagentID)

		// Verify the subagent session exists
		_, err = database.GetSession(subDb)
		if err != nil {
			return tool.HandlerResults{}, fmt.Errorf("subagent session with ID %s not found: %w", subsessionID, err)
		}
	}

	// Copy environment from main session to subagent session
	err = copyEnvironmentToSubagent(mainDb, subDb)
	if err != nil {
		log.Printf("Failed to copy environment to subagent: %v", err)
		// Non-fatal, continue with subagent execution
	}

	// Resolve the model for general subagent tasks
	subagentModelProvider, err := models.ResolveSubagent(params.ModelName, "")
	if err != nil {
		return tool.HandlerResults{}, fmt.Errorf("failed to resolve subagent: %w", err)
	}

	// Create result collector to capture chat events
	resultCollector := newSubagentResultCollector(newAgentID, false)

	// Use chat package's NewChatMessage to handle the subagent interaction
	// with the resolved subagent model name
	err = chat.NewChatMessage(
		ctx,
		subDb,
		models,
		ga,
		tools,
		config,
		resultCollector, // Custom EventWriter that captures events
		text,
		nil,                        // No attachments for regular subagent
		subagentModelProvider.Name, // Use the resolved subagent model
		0,                          // fetchLimit is not needed for subagent
	)

	if err != nil {
		return tool.HandlerResults{}, fmt.Errorf("subagent execution failed: %w", err)
	}

	result := map[string]interface{}{
		"response_text": resultCollector.Response(),
	}
	if subagentID := resultCollector.SubagentID(); subagentID != "" {
		result["subagent_id"] = subagentID
	}
	if errorMessage := resultCollector.ErrorMessage(); errorMessage != "" {
		result["error"] = errorMessage
	}

	return tool.HandlerResults{
		Value:       result,
		Attachments: resultCollector.Attachments(),
	}, nil
}

// copyEnvironmentToSubagent copies the environment configuration from main session to subagent session
func copyEnvironmentToSubagent(mainDb, subDb *database.SessionDatabase) error {
	// Get main session environment
	mainRoots, _, err := database.GetLatestSessionEnv(mainDb)
	if err != nil {
		return fmt.Errorf("failed to get main session environment: %w", err)
	}

	// Get subagent session environment
	subRoots, _, err := database.GetLatestSessionEnv(subDb)
	if err != nil {
		return fmt.Errorf("failed to get subagent session environment: %w", err)
	}

	// Check if roots have actually changed
	rootsChanged, err := env.CalculateRootsChanged(subRoots, mainRoots)
	if err != nil {
		return fmt.Errorf("failed to calculate roots changed for subagent: %w", err)
	}

	// Only update if roots have actually changed
	if rootsChanged.HasChanges() {
		// Add new subagent session environment in DB
		_, err = database.AddSessionEnv(subDb, mainRoots)
		if err != nil {
			return fmt.Errorf("failed to add new subagent session environment: %w", err)
		}
	}

	return nil
}

// GenerateImageTool handles the generate_image tool call, allowing to generate images using a subagent with image generation capabilities.
func GenerateImageTool(ctx context.Context, args map[string]interface{}, params tool.HandlerParams) (tool.HandlerResults, error) {
	// Accept and discard want_image for backward compatibility
	if err := tool.EnsureKnownKeys("generate_image", args, "text", "input_hashes", "want_image"); err != nil {
		return tool.HandlerResults{}, err
	}

	text, hasText := args["text"].(string)
	inputHashesInterface, hasInputHashes := args["input_hashes"].([]interface{})

	if !hasText {
		return tool.HandlerResults{}, fmt.Errorf("generate_image tool: must provide 'text'")
	}

	// Convert input_hashes from []interface{} to []string
	var inputHashes []string
	if hasInputHashes {
		inputHashes = make([]string, len(inputHashesInterface))
		for i, hash := range inputHashesInterface {
			if hashStr, ok := hash.(string); ok {
				inputHashes[i] = hashStr
			} else {
				return tool.HandlerResults{}, fmt.Errorf("generate_image tool: input_hashes must be strings")
			}
		}
	}

	db, err := database.FromContext(ctx)
	if err != nil {
		return tool.HandlerResults{}, err
	}
	models, err := llm.ModelsFromContext(ctx)
	if err != nil {
		return tool.HandlerResults{}, err
	}
	ga, err := llm.GeminiAuthFromContext(ctx)
	if err != nil {
		return tool.HandlerResults{}, err
	}
	tools, err := tool.FromContext(ctx)
	if err != nil {
		return tool.HandlerResults{}, err
	}
	config, err := env.EnvConfigFromContext(ctx)
	if err != nil {
		return tool.HandlerResults{}, err
	}

	// Create a subsession for image generation
	agentID := database.GenerateID()
	subsessionID := fmt.Sprintf("%s.%s", params.SessionId, agentID)

	mainDb, err := db.WithSession(params.SessionId)
	if err != nil {
		return tool.HandlerResults{}, err
	}
	defer mainDb.Close()
	subDb := mainDb.WithSuffix("." + agentID)

	// Create subsession with system prompt for image generation
	systemPrompt := "Generate images based on the user's request. The output should contain the generated images."
	subDb, _, err = database.CreateSession(subDb.Database, subsessionID, systemPrompt, "")
	if err != nil {
		return tool.HandlerResults{}, fmt.Errorf("failed to create image generation subsession with ID %s: %w", subsessionID, err)
	}

	// Convert input hashes to FileAttachments for the chat message
	var inputAttachments []FileAttachment
	for _, hash := range inputHashes {
		if hash != "" {
			attachment, err := database.GetBlobAsFileAttachment(mainDb, hash)
			if err != nil {
				return tool.HandlerResults{}, fmt.Errorf("failed to create file attachment for hash %s: %w", hash, err)
			}
			inputAttachments = append(inputAttachments, attachment)
		}
	}

	// Copy environment from main session to image generation subagent
	err = copyEnvironmentToSubagent(mainDb, subDb)
	if err != nil {
		log.Printf("Failed to copy environment to image generation subagent: %v", err)
		// Non-fatal, continue with image generation
	}

	// Resolve the model for image generation
	imageModelProvider, err := models.ResolveSubagent(params.ModelName, llm.SubagentImageGenerationTask)
	if err != nil {
		return tool.HandlerResults{}, fmt.Errorf("failed to resolve subagent for image generation: %w", err)
	}

	// Create result collector to capture chat events
	resultCollector := newSubagentResultCollector(agentID, true)

	// Use chat package's NewChatMessage to handle the image generation
	// with the resolved image generation model name
	err = chat.NewChatMessage(
		ctx,
		subDb,
		models,
		ga,
		tools,
		config,
		resultCollector, // Custom EventWriter that captures events
		text,
		inputAttachments,        // Input images as attachments
		imageModelProvider.Name, // Use the resolved image generation model
		0,                       // fetchLimit is not needed for image generation
	)

	if err != nil {
		return tool.HandlerResults{}, fmt.Errorf("image generation failed: %w", err)
	}

	result := map[string]interface{}{
		"response": resultCollector.Response(),
	}
	if errorMessage := resultCollector.ErrorMessage(); errorMessage != "" {
		result["error"] = errorMessage
	}

	return tool.HandlerResults{
		Value:       result,
		Attachments: resultCollector.Attachments(),
	}, nil
}

var subagentTool = tool.Definition{
	Name:        "subagent",
	Description: "Spawns a new subagent session with a given system prompt, or sends a text message to an existing subagent session (identified by its session-local ID) and returns its text response. Exactly one of 'subagent_id' or 'system_prompt' must be provided. 'subagent_id' is returned only when 'system_prompt' is provided.",
	Parameters: &Schema{
		Type: TypeObject,
		Properties: map[string]*Schema{
			"subagent_id": {
				Type:        TypeString,
				Description: "The session-local ID of the subagent session to interact with. Required if 'system_prompt' is not provided.",
			},
			"system_prompt": {
				Type:        TypeString,
				Description: "The system prompt for the new subagent session. Required if 'subagent_id' is not provided.",
			},
			"text": {
				Type:        TypeString,
				Description: "The text message to send to the subagent.",
			},
		},
		Required: []string{"text"},
	},
	Handler: SubagentTool,
}

var generateImageTool = tool.Definition{
	Name:        "generate_image",
	Description: "Generates new images based on a text prompt. It can also be used for general image editing tasks by providing an `input_hashes` and a `text` prompt describing the desired modifications (e.g., 'change background to white', 'apply a sepia filter'). Returns the SHA-512/256 hash(es) of the generated image(s) for internal tracking, and the generated images are always returned as attachments for direct assessment.",
	Parameters: &Schema{
		Type: TypeObject,
		Properties: map[string]*Schema{
			"text": {
				Type:        TypeString,
				Description: "The text prompt for image generation, preferably in English. This prompt should clearly describe the desired image or the modifications to be applied. Do not include image hashes in the text; use the input_hashes parameter instead.",
			},
			"input_hashes": {
				Type:        TypeArray,
				Description: "Array of SHA-512/256 hashes of input images to use for generation or as a base for modifications.",
				Items: &Schema{
					Type: TypeString,
				},
			},
		},
		Required: []string{"text"},
	},
	Handler: GenerateImageTool,
}

var AllTools = []tool.Definition{
	subagentTool,
	generateImageTool,
}
