package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
	"unicode"

	. "github.com/lifthrasiir/angel/gemini"
)

// addHashWithSpacing adds a hash to the response with appropriate spacing to avoid
// confusion with adjacent Unicode letters (\pL) or other hashes
func addHashWithSpacing(response *strings.Builder, hash string) {
	currentText := response.String()

	// Check if we need to add space before the hash
	if len(currentText) > 0 {
		lastChar := rune(currentText[len(currentText)-1])
		// Add space before if previous character is a Unicode letter or a digit
		if unicode.IsLetter(lastChar) || unicode.IsDigit(lastChar) {
			response.WriteString(" ")
		}
	}

	// Add the hash
	response.WriteString(hash)
}

// addTextWithSpacing adds text to the response with appropriate spacing to avoid
// confusion with adjacent hashes
func addTextWithSpacing(response *strings.Builder, text string) {
	if text == "" {
		return
	}

	currentText := response.String()

	// Check if we need to add space before the text
	if len(currentText) > 0 {
		lastChar := rune(currentText[len(currentText)-1])
		// Add space before if previous character looks like it could be part of a hash
		if unicode.IsLetter(lastChar) || unicode.IsDigit(lastChar) {
			response.WriteString(" ")
		}
	}

	// Add the text
	response.WriteString(text)
}

// SubagentTool handles the subagent tool call, allowing to spawn a new subagent or interact with an existing one.
func SubagentTool(ctx context.Context, args map[string]interface{}, params ToolHandlerParams) (ToolHandlerResults, error) {
	if err := EnsureKnownKeys("subagent", args, "subagent_id", "system_prompt", "text"); err != nil {
		return ToolHandlerResults{}, err
	}

	subagentID, hasSubagentID := args["subagent_id"].(string)
	systemPrompt, hasSystemPrompt := args["system_prompt"].(string)
	text, hasText := args["text"].(string)

	if hasSubagentID && hasSystemPrompt {
		return ToolHandlerResults{}, fmt.Errorf("subagent tool: cannot provide both 'subagent_id' and 'system_prompt'")
	}
	if !hasSubagentID && !hasSystemPrompt {
		return ToolHandlerResults{}, fmt.Errorf("subagent tool: must provide either 'subagent_id' or 'system_prompt'")
	}
	if !hasText {
		return ToolHandlerResults{}, fmt.Errorf("subagent tool: must provide 'text'")
	}

	db, err := getDbFromContext(ctx)
	if err != nil {
		return ToolHandlerResults{}, err
	}

	// Check if the current session is already a subagent session
	if strings.Contains(params.SessionId, ".") {
		return ToolHandlerResults{}, errors.New("subagent tool cannot be called from a subagent session")
	}

	if hasSystemPrompt {
		// Spawn a new subagent
		agentID := generateID()
		subsessionID := fmt.Sprintf("%s.%s", params.SessionId, agentID)

		_, err = CreateSession(db, subsessionID, systemPrompt, "")
		if err != nil {
			return ToolHandlerResults{}, fmt.Errorf("failed to create new subagent session with ID %s: %w", subsessionID, err)
		}

		// Now send the initial message to the newly spawned subagent
		// This part is similar to the beginning of SubagentTurnTool
		session, err := GetSession(db, subsessionID)
		if err != nil {
			return ToolHandlerResults{}, fmt.Errorf("subagent session with ID %s not found after creation: %w", subsessionID, err)
		}

		mc, err := NewMessageChain(ctx, db, subsessionID, session.PrimaryBranchID)
		if err != nil {
			return ToolHandlerResults{}, fmt.Errorf("failed to create message chain for subagent: %w", err)
		}

		// Initial message for a new subagent session will have generation 0
		mc.LastMessageModel = params.ModelName
		if _, err = mc.Add(ctx, db, Message{Type: TypeUserText, Text: text}); err != nil {
			return ToolHandlerResults{}, fmt.Errorf("failed to add initial user message to new subagent session: %w", err)
		}

		// Proceed with LLM turn for the new subagent
		return handleSubagentTurn(ctx, db, subsessionID, &session, params, agentID, mc)
	} else {
		// Interact with an existing subagent
		subsessionID := fmt.Sprintf("%s.%s", params.SessionId, subagentID)

		session, err := GetSession(db, subsessionID)
		if err != nil {
			return ToolHandlerResults{}, fmt.Errorf("subagent session with ID %s not found: %w", subsessionID, err)
		}

		mc, err := NewMessageChain(ctx, db, subsessionID, session.PrimaryBranchID)
		if err != nil {
			return ToolHandlerResults{}, fmt.Errorf("failed to create message chain for subagent: %w", err)
		}

		// For existing subagent sessions, the generation will be determined within handleSubagentTurn
		mc.LastMessageModel = params.ModelName
		if _, err = mc.Add(ctx, db, Message{Type: TypeUserText, Text: text}); err != nil {
			return ToolHandlerResults{}, fmt.Errorf("failed to add user message to subagent session: %w", err)
		}

		return handleSubagentTurn(ctx, db, subsessionID, &session, params, "", mc)
	}
}

// handleSubagentTurn encapsulates the common logic for interacting with a subagent session.
func handleSubagentTurn(
	ctx context.Context,
	db *sql.DB,
	subsessionID string,
	session *Session,
	params ToolHandlerParams,
	agentID string,
	mc *MessageChain,
) (ToolHandlerResults, error) {
	// Get main session environment
	mainSessionEnvRoots, _, err := GetLatestSessionEnv(db, params.SessionId)
	if err != nil {
		return ToolHandlerResults{}, fmt.Errorf("failed to get main session environment: %w", err)
	}

	// Get subagent session environment
	subSessionEnvRoots, subSessionGeneration, err := GetLatestSessionEnv(db, subsessionID)
	if err != nil {
		return ToolHandlerResults{}, fmt.Errorf("failed to get subagent session environment: %w", err)
	}

	// Check if roots have actually changed
	rootsChanged, err := calculateRootsChanged(subSessionEnvRoots, mainSessionEnvRoots)
	if err != nil {
		return ToolHandlerResults{}, fmt.Errorf("failed to calculate roots changed for subagent: %w", err)
	}

	// Only add env_changed message if roots have actually changed (added or removed)
	if rootsChanged.HasChanges() {
		envChanged := EnvChanged{Roots: &rootsChanged}

		// Marshal envChanged into JSON
		envChangedJSON, err := json.Marshal(envChanged)
		if err != nil {
			return ToolHandlerResults{}, fmt.Errorf("failed to marshal envChanged for subagent: %w", err)
		}

		// Add env_changed message to subagent session with the new generation
		mc.LastMessageGeneration = subSessionGeneration
		if _, err = mc.Add(ctx, db, Message{Type: TypeEnvChanged, Text: string(envChangedJSON)}); err != nil {
			return ToolHandlerResults{}, fmt.Errorf("failed to add env_changed message to subagent session: %w", err)
		}
		// Add new subagent session environment in DB
		_, err = AddSessionEnv(db, subsessionID, mainSessionEnvRoots)
		if err != nil {
			return ToolHandlerResults{}, fmt.Errorf("failed to add new subagent session environment: %w", err)
		}
	}

	// Load conversation context for the subagent (using GetSessionHistoryContext function defined in db_chat.go)
	// GetSessionHistoryContext returns []FrontendMessage.
	frontendMessages, err := GetSessionHistoryContext(db, subsessionID, session.PrimaryBranchID)
	if err != nil {
		return ToolHandlerResults{}, fmt.Errorf("failed to get messages for subagent session %s: %w", subsessionID, err)
	}
	currentHistory := convertFrontendMessagesToContent(db, frontendMessages)

	// Get LLM client for the subagent
	llmClient, subagentModelName, subagentGenParams := CurrentProviders[params.ModelName].SubagentProviderAndParams(params.ModelName, "")
	if llmClient == nil {
		return ToolHandlerResults{}, fmt.Errorf("LLM client for model %s not found", params.ModelName)
	}

	// Update LastMessageModel to use the actual provider's model name
	mc.LastMessageModel = subagentModelName

	var fullResponseText strings.Builder
	var firstFinishReason string

	// Loop to continue LLM calls after tool execution
	for {
		// Configure SessionParams for LLM call
		sessionParams := &SessionParams{
			SystemPrompt:     session.SystemPrompt,
			Contents:         currentHistory, // Use updated history
			GenerationParams: &subagentGenParams,
		}

		// Use a context with a timeout for the LLM call
		llmCtx, cancel := context.WithTimeout(ctx, 5*time.Minute) // 5 minute timeout for subagent turn

		// Stream LLM response
		seq, closer, err := llmClient.SendMessageStream(llmCtx, subagentModelName, *sessionParams)
		if err != nil {
			cancel() // Ensure context is cancelled on error
			return ToolHandlerResults{}, fmt.Errorf("failed to get streaming response from subagent LLM: %w", err)
		}

		hasFunctionCall := false // Track if a function call occurred in this turn

		for caResp := range seq {
			if len(caResp.Candidates) > 0 {
				candidate := caResp.Candidates[0]
				// Capture the first finish reason (like in chat_stream.go)
				if firstFinishReason == "" && candidate.FinishReason != "" {
					firstFinishReason = candidate.FinishReason
				}
				for _, part := range candidate.Content.Parts {
					if part.Text != "" {
						fullResponseText.WriteString(part.Text)
						// In a real streaming scenario, you would update the DB with partial text here
						// For subagent_turn, we accumulate and save at the end of the outer loop.
					} else if part.FunctionCall != nil {
						hasFunctionCall = true
						fc := *part.FunctionCall
						log.Printf("Subagent LLM requested tool call: %s with args: %+v", fc.Name, fc.Args)

						// Save FunctionCall message to DB
						fcJson, _ := json.Marshal(fc)
						if _, err := mc.Add(ctx, db, Message{Type: TypeFunctionCall, Text: string(fcJson)}); err != nil {
							log.Printf("Warning: Failed to add function call message to subagent session: %v", err)
						}

						// Execute the tool
						toolResults, err := CallToolFunction(ctx, fc, ToolHandlerParams{
							ModelName: params.ModelName,
							SessionId: subsessionID,
							BranchId:  session.PrimaryBranchID,
						})
						if err != nil {
							log.Printf("Subagent LLM tool execution failed: %v", err)
							toolResults.Value = map[string]interface{}{"error": err.Error()}
						}

						// Save FunctionResponse message to DB
						frJson, _ := json.Marshal(FunctionResponse{
							Name:     fc.Name,
							Response: toolResults.Value,
						})
						_, err = mc.Add(ctx, db, Message{
							Type:        TypeFunctionResponse,
							Text:        string(frJson),
							Attachments: toolResults.Attachments,
						})
						if err != nil {
							log.Printf("Warning: Failed to add function response message to subagent session: %v", err)
						}

						// Create the initial FunctionResponse part
						fr := FunctionResponse{Name: fc.Name, Response: toolResults.Value}

						// Add FunctionCall and FunctionResponse (with attachments) to currentHistory for next LLM turn
						currentHistory = append(currentHistory,
							Content{Role: RoleModel, Parts: []Part{{FunctionCall: &fc}}},
							Content{Role: RoleUser, Parts: appendAttachmentParts(db, toolResults, []Part{{FunctionResponse: &fr}})},
						)
					}
				}
			}
		}
		cancel()       // Ensure context is cancelled on error
		closer.Close() // Close the stream for the current LLM call

		if !hasFunctionCall {
			// No function call in this turn, break the loop
			break
		}
	}

	// Add the final model response message to the database
	if fullResponseText.Len() > 0 {
		if _, err = mc.Add(ctx, db, Message{Type: TypeModelText, Text: fullResponseText.String()}); err != nil {
			return ToolHandlerResults{}, fmt.Errorf("failed to add final model response to subagent session: %w", err)
		}
	}

	result := map[string]interface{}{"response_text": fullResponseText.String()}
	if agentID != "" {
		result["subagent_id"] = agentID
	}

	// Handle finish reason errors
	if firstFinishReason != "" && firstFinishReason != FinishReasonStop {
		result["error"] = FinishReasonMessage(firstFinishReason)
	}

	return ToolHandlerResults{Value: result}, nil
}

// GenerateImageTool handles the generate_image tool call, allowing to generate images using a subagent with image generation capabilities.
func GenerateImageTool(ctx context.Context, args map[string]interface{}, params ToolHandlerParams) (ToolHandlerResults, error) {
	// Accept and discard want_image for backward compatibility
	if err := EnsureKnownKeys("generate_image", args, "text", "input_hashes", "want_image"); err != nil {
		return ToolHandlerResults{}, err
	}

	text, hasText := args["text"].(string)
	inputHashesInterface, hasInputHashes := args["input_hashes"].([]interface{})

	if !hasText {
		return ToolHandlerResults{}, fmt.Errorf("generate_image tool: must provide 'text'")
	}

	// Convert input_hashes from []interface{} to []string
	var inputHashes []string
	if hasInputHashes {
		inputHashes = make([]string, len(inputHashesInterface))
		for i, hash := range inputHashesInterface {
			if hashStr, ok := hash.(string); ok {
				inputHashes[i] = hashStr
			} else {
				return ToolHandlerResults{}, fmt.Errorf("generate_image tool: input_hashes must be strings")
			}
		}
	}

	db, err := getDbFromContext(ctx)
	if err != nil {
		return ToolHandlerResults{}, err
	}

	// Create a subsession for image generation
	agentID := generateID()
	subsessionID := fmt.Sprintf("%s.%s", params.SessionId, agentID)

	// Create subsession with system prompt for image generation
	systemPrompt := "Generate images based on the user's request. The output should contain the generated images."
	_, err = CreateSession(db, subsessionID, systemPrompt, "")
	if err != nil {
		return ToolHandlerResults{}, fmt.Errorf("failed to create image generation subsession with ID %s: %w", subsessionID, err)
	}

	// Get session for message chain creation
	session, err := GetSession(db, subsessionID)
	if err != nil {
		return ToolHandlerResults{}, fmt.Errorf("image generation subsession with ID %s not found after creation: %w", subsessionID, err)
	}

	// Create message chain for the subsession
	mc, err := NewMessageChain(ctx, db, subsessionID, session.PrimaryBranchID)
	if err != nil {
		return ToolHandlerResults{}, fmt.Errorf("failed to create message chain for image generation subsession: %w", err)
	}

	// Get LLM client for image generation using the new task first to determine the correct model
	llmClient, imageModelName, imageGenParams := CurrentProviders[params.ModelName].SubagentProviderAndParams(params.ModelName, SubagentImageGenerationTask)
	if llmClient == nil {
		return ToolHandlerResults{}, fmt.Errorf("LLM client for image generation with model %s not found", params.ModelName)
	}

	// Set the last message model to use the actual provider's model name
	mc.LastMessageModel = imageModelName

	// Save user message to subsession with input image attachments
	var userMessageAttachments []FileAttachment
	for _, hash := range inputHashes {
		if hash != "" {
			attachment, err := GetBlobAsFileAttachment(db, hash)
			if err != nil {
				return ToolHandlerResults{}, fmt.Errorf("failed to create file attachment for hash %s: %w", hash, err)
			}
			userMessageAttachments = append(userMessageAttachments, attachment)
		}
	}

	if _, err = mc.Add(ctx, db, Message{
		Type:        TypeUserText,
		Text:        text,
		Attachments: userMessageAttachments,
	}); err != nil {
		return ToolHandlerResults{}, fmt.Errorf("failed to add user message to image generation subsession: %w", err)
	}

	// Prepare the content for image generation
	var content []Content

	// Add user text part
	content = append(content, Content{
		Role: RoleUser,
		Parts: []Part{
			{Text: text},
		},
	})

	// Add input images as attachments if provided
	for _, hash := range inputHashes {
		if hash != "" {
			// Retrieve blob data for the hash
			blobData, err := GetBlob(db, hash)
			if err != nil {
				return ToolHandlerResults{}, fmt.Errorf("failed to retrieve blob for hash %s: %w", hash, err)
			}

			// Determine MIME type by detecting content type
			mimeType := http.DetectContentType(blobData)
			content = append(content, Content{
				Role: RoleUser,
				Parts: []Part{
					{
						InlineData: &InlineData{
							MimeType: mimeType,
							Data:     base64.StdEncoding.EncodeToString(blobData),
						},
					},
				},
			})
		}
	}

	// Configure session params for image generation
	sessionParams := &SessionParams{
		SystemPrompt:     "Generate images based on the user's request. The output should contain the generated images.",
		Contents:         content,
		GenerationParams: &imageGenParams,
	}

	// Use a context with timeout for the image generation
	llmCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	// Stream LLM response
	seq, closer, err := llmClient.SendMessageStream(llmCtx, imageModelName, *sessionParams)
	if err != nil {
		return ToolHandlerResults{}, fmt.Errorf("failed to get streaming response from image generation LLM: %w", err)
	}
	defer closer.Close()

	var fullResponseText strings.Builder
	var generatedAttachments []FileAttachment
	var firstFinishReason string
	imageCounter := 1

	// Process the response
	for caResp := range seq {
		if len(caResp.Candidates) > 0 {
			candidate := caResp.Candidates[0]
			// Capture the first finish reason (like in chat_stream.go)
			if firstFinishReason == "" && candidate.FinishReason != "" {
				firstFinishReason = candidate.FinishReason
			}
			for _, part := range candidate.Content.Parts {
				if part.InlineData != nil {
					// Convert generated image data to blob and get hash
					imageData, err := base64.StdEncoding.DecodeString(part.InlineData.Data)
					if err != nil {
						log.Printf("Warning: Failed to decode generated image data: %v", err)
						continue
					}

					hash, err := SaveBlob(ctx, db, imageData)
					if err != nil {
						log.Printf("Warning: Failed to save generated image blob: %v", err)
						continue
					}

					// Add hash to response with proper spacing
					addHashWithSpacing(&fullResponseText, hash)

					// Generate filename with counter for generated images
					filename := generateFilenameFromMimeType(part.InlineData.MimeType, imageCounter)
					imageCounter++

					// Immediately save the image as a separate message to preserve blob
					messageAttachments := []FileAttachment{{
						Hash:     hash,
						MimeType: part.InlineData.MimeType,
						FileName: filename,
					}}

					if _, err = mc.Add(ctx, db, Message{
						Type:        TypeModelText,
						Text:        "",
						Attachments: messageAttachments,
					}); err != nil {
						log.Printf("Warning: Failed to add image response to subsession: %v", err)
					}

					// Add to attachments
					generatedAttachments = append(generatedAttachments, FileAttachment{
						Hash:     hash,
						MimeType: part.InlineData.MimeType,
						FileName: filename,
					})
				} else {
					// Add text to response with proper spacing and immediately save to message chain
					addTextWithSpacing(&fullResponseText, part.Text)
					if _, err = mc.Add(ctx, db, Message{
						Type: TypeModelText,
						Text: part.Text,
					}); err != nil {
						log.Printf("Warning: Failed to add text response to subsession: %v", err)
					}
				}
			}
		}
	}

	// Prepare result
	result := map[string]interface{}{
		"response": fullResponseText.String(),
	}

	// Handle finish reason errors
	if firstFinishReason != "" && firstFinishReason != FinishReasonStop {
		result["error"] = FinishReasonMessage(firstFinishReason)
	}

	// Always return with attachments
	return ToolHandlerResults{Value: result, Attachments: generatedAttachments}, nil
}

func init() {
	// Define tool definitions locally within init function
	subagentToolDefinition := ToolDefinition{
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

	// Define generate_image tool
	generateImageToolDefinition := ToolDefinition{
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

	// Register tool definitions with availableTools map
	availableTools["subagent"] = subagentToolDefinition
	availableTools["generate_image"] = generateImageToolDefinition
}
