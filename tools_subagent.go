package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
)

// SubagentTool handles the subagent tool call, allowing to spawn a new subagent or interact with an existing one.
func SubagentTool(ctx context.Context, args map[string]interface{}, params ToolHandlerParams) (ToolHandlerResults, error) {
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

		lastMessageIDFromDB, _, _, err := GetLastMessageInBranch(db, subsessionID, session.PrimaryBranchID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return ToolHandlerResults{}, fmt.Errorf("failed to get last message in subagent branch %s: %w", session.PrimaryBranchID, err)
		}
		lastAddedMessageID := lastMessageIDFromDB

		addMessageToCurrentSubagentSession := func(messageType MessageType, text string, attachments []FileAttachment, cumulTokenCount *int, generation int) (messageID int, err error) {
			var parentMessageID *int
			if lastAddedMessageID != 0 {
				parentMessageID = &lastAddedMessageID
			}
			messageID, err = AddMessageToSession(ctx, db, Message{
				SessionID:       subsessionID,
				BranchID:        session.PrimaryBranchID,
				ParentMessageID: parentMessageID,
				ChosenNextID:    nil,
				Text:            text,
				Type:            messageType,
				Attachments:     attachments,
				CumulTokenCount: cumulTokenCount,
				Model:           params.ModelName,
				Generation:      generation,
			})
			if err == nil {
				if parentMessageID != nil {
					if err := UpdateMessageChosenNextID(db, *parentMessageID, &messageID); err != nil {
						log.Printf("Failed to update chosen_next_id for message %d: %v", *parentMessageID, err)
					}
				}
				lastAddedMessageID = messageID
			}
			return
		}

		// Initial message for a new subagent session will have generation 0
		_, err = addMessageToCurrentSubagentSession(TypeUserText, text, nil, nil, 0)
		if err != nil {
			return ToolHandlerResults{}, fmt.Errorf("failed to add initial user message to new subagent session: %w", err)
		}

		// Proceed with LLM turn for the new subagent
		return handleSubagentTurn(ctx, db, subsessionID, &session, params, agentID, addMessageToCurrentSubagentSession)

	} else {
		// Interact with an existing subagent
		subsessionID := fmt.Sprintf("%s.%s", params.SessionId, subagentID)

		session, err := GetSession(db, subsessionID)
		if err != nil {
			return ToolHandlerResults{}, fmt.Errorf("subagent session with ID %s not found: %w", subsessionID, err)
		}

		lastMessageIDFromDB, _, _, err := GetLastMessageInBranch(db, subsessionID, session.PrimaryBranchID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return ToolHandlerResults{}, fmt.Errorf("failed to get last message in subagent branch %s: %w", session.PrimaryBranchID, err)
		}
		lastAddedMessageID := lastMessageIDFromDB

		addMessageToCurrentSubagentSession := func(messageType MessageType, text string, attachments []FileAttachment, cumulTokenCount *int, generation int) (messageID int, err error) {
			var parentMessageID *int
			if lastAddedMessageID != 0 {
				parentMessageID = &lastAddedMessageID
			}
			messageID, err = AddMessageToSession(ctx, db, Message{
				SessionID:       subsessionID,
				BranchID:        session.PrimaryBranchID,
				ParentMessageID: parentMessageID,
				ChosenNextID:    nil,
				Text:            text,
				Type:            messageType,
				Attachments:     attachments,
				CumulTokenCount: cumulTokenCount,
				Model:           params.ModelName,
				Generation:      generation,
			})
			if err == nil {
				if parentMessageID != nil {
					if err := UpdateMessageChosenNextID(db, *parentMessageID, &messageID); err != nil {
						log.Printf("Failed to update chosen_next_id for message %d: %v", *parentMessageID, err)
					}
				}
				lastAddedMessageID = messageID
			}
			return
		}

		// For existing subagent sessions, the generation will be determined within handleSubagentTurn
		_, err = addMessageToCurrentSubagentSession(TypeUserText, text, nil, nil, 0) // Pass 0 for now, will be updated
		if err != nil {
			return ToolHandlerResults{}, fmt.Errorf("failed to add user message to subagent session: %w", err)
		}

		return handleSubagentTurn(ctx, db, subsessionID, &session, params, "", addMessageToCurrentSubagentSession)
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
	addMessageToCurrentSubagentSession func(
		messageType MessageType,
		text string,
		attachments []FileAttachment,
		cumulTokenCount *int,
		generation int,
	) (messageID int, err error),
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
		_, err = addMessageToCurrentSubagentSession(TypeEnvChanged, string(envChangedJSON), nil, nil, subSessionGeneration)
		if err != nil {
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
	llmClient := CurrentProviders[params.ModelName]
	if llmClient == nil {
		return ToolHandlerResults{}, fmt.Errorf("LLM client for model %s not found", params.ModelName)
	}

	var fullResponseText strings.Builder

	// Loop to continue LLM calls after tool execution
	for {
		// Configure SessionParams for LLM call
		sessionParams := &SessionParams{
			ModelName:    params.ModelName,
			SystemPrompt: session.SystemPrompt,
			Contents:     currentHistory, // Use updated history
		}

		// Use a context with a timeout for the LLM call
		llmCtx, cancel := context.WithTimeout(ctx, 5*time.Minute) // 5 minute timeout for subagent turn

		// Stream LLM response
		seq, closer, err := llmClient.SendMessageStream(llmCtx, *sessionParams)
		if err != nil {
			cancel() // Ensure context is cancelled on error
			return ToolHandlerResults{}, fmt.Errorf("failed to get streaming response from subagent LLM: %w", err)
		}

		hasFunctionCall := false // Track if a function call occurred in this turn

		for caResp := range seq {
			if len(caResp.Response.Candidates) > 0 {
				candidate := caResp.Response.Candidates[0]
				for _, part := range candidate.Content.Parts {
					if part.Text != "" {
						fullResponseText.WriteString(part.Text)
						// In a real streaming scenario, you would update the DB with partial text here
						// For subagent_turn, we accumulate and save at the end of the outer loop.
					} else if part.FunctionCall != nil {
						hasFunctionCall = true
						toolCall := *part.FunctionCall
						log.Printf("Subagent LLM requested tool call: %s with args: %+v", toolCall.Name, toolCall.Args)

						// Save FunctionCall message to DB
						fcJson, _ := json.Marshal(toolCall)
						_, err := addMessageToCurrentSubagentSession(TypeFunctionCall, string(fcJson), nil, nil, subSessionGeneration)
						if err != nil {
							log.Printf("Warning: Failed to add function call message to subagent session: %v", err)
						}

						// Execute the tool
						toolResult, err := CallToolFunction(ctx, toolCall, ToolHandlerParams{
							ModelName: params.ModelName,
							SessionId: subsessionID,
							BranchId:  session.PrimaryBranchID,
						})
						if err != nil {
							log.Printf("Subagent LLM tool execution failed: %v", err)
							toolResult.Value = map[string]interface{}{"error": err.Error()}
						}

						// Save FunctionResponse message to DB
						frJson, _ := json.Marshal(FunctionResponse{
							Name:     toolCall.Name,
							Response: toolResult.Value,
						})
						_, err = addMessageToCurrentSubagentSession(TypeFunctionResponse, string(frJson), toolResult.Attachments, nil, subSessionGeneration)
						if err != nil {
							log.Printf("Warning: Failed to add function response message to subagent session: %v", err)
						}

						// Create the initial FunctionResponse part
						frPart := Part{FunctionResponse: &FunctionResponse{Name: toolCall.Name, Response: toolResult.Value}}
						partsForContent := []Part{frPart}

						// Add parts for attachments
						for _, attachment := range toolResult.Attachments {
							// Retrieve blob data from DB using hash
							blobData, err := GetBlob(db, attachment.Hash)
							if err != nil {
								log.Printf("Failed to retrieve blob data for hash %s: %v", attachment.Hash, err)
								continue // Skip this attachment
							}

							// Add inlineData part with Base64 encoded blob data
							partsForContent = append(partsForContent, Part{
								InlineData: &InlineData{
									MimeType: attachment.MimeType,
									Data:     base64.StdEncoding.EncodeToString(blobData),
								},
							})
						}

						// Add FunctionCall and FunctionResponse (with attachments) to currentHistory for next LLM turn
						currentHistory = append(currentHistory, Content{Role: RoleModel, Parts: []Part{{FunctionCall: &toolCall}}})
						currentHistory = append(currentHistory, Content{Role: RoleUser, Parts: partsForContent})
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
		_, err = addMessageToCurrentSubagentSession(TypeModelText, fullResponseText.String(), nil, nil, subSessionGeneration)
		if err != nil {
			return ToolHandlerResults{}, fmt.Errorf("failed to add final model response to subagent session: %w", err)
		}
	}

	result := map[string]interface{}{"response_text": fullResponseText.String()}
	if agentID != "" {
		result["subagent_id"] = agentID
	}
	return ToolHandlerResults{Value: result}, nil
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

	// Register tool definition with availableTools map
	availableTools["subagent"] = subagentToolDefinition
}
