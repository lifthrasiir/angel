package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"
)

var thoughtPattern = regexp.MustCompile(`^\*\*(.*?)\*\*\n+(.*)\n*$`)

// Helper function to stream LLM response
func streamLLMResponse(
	db *sql.DB,
	initialState InitialState,
	sseW *sseWriter,
	lastUserMessageID int,
	modelToUse string,
	generation int,
	inferSessionName bool,
	callStartTime time.Time,
	fullHistoryForLLM []FrontendMessage,
) error {
	var agentResponseText string
	var lastUsageMetadata *UsageMetadata
	currentHistory := convertFrontendMessagesToContent(db, fullHistoryForLLM)

	// Track the ID of the last message added to the database
	lastAddedMessageID := lastUserMessageID

	// Create a cancellable context for the LLM call
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()                          // Ensure context is cancelled when function exits
	ctx = context.WithValue(ctx, dbKey, db) // Required by tools

	// Register the call with the call manager
	if err := startCall(initialState.SessionId, cancel); err != nil {
		log.Printf("streamLLMResponse: Failed to start call for session %s: %v", initialState.SessionId, err)
		broadcastToSession(initialState.SessionId, EventError, err.Error())
		return err
	}
	defer removeCall(initialState.SessionId) // Ensure call is removed from manager when function exits

	// Calculate elapsed time since the call started
	initialState.CallElapsedTimeSeconds = time.Since(callStartTime).Seconds()

	// Initialize modelMessageID to negative. It's used for the current streaming model message.
	modelMessageID := -1

	provider, ok := CurrentProviders[modelToUse]
	if !ok {
		return fmt.Errorf("unsupported model: %s", modelToUse)
	}

	for {
		addMessageToCurrentSession := func(messageType MessageType, text string, attachments []FileAttachment, cumulTokenCount *int, state string, aux string) (messageID int, err error) {
			var parentMessageID *int
			if lastAddedMessageID != 0 {
				parentMessageID = &lastAddedMessageID
			}
			messageID, err = AddMessageToSession(ctx, db, Message{
				SessionID:       initialState.SessionId,
				BranchID:        initialState.PrimaryBranchID,
				ParentMessageID: parentMessageID,
				ChosenNextID:    nil,
				Text:            text,
				Type:            messageType,
				Attachments:     attachments,
				CumulTokenCount: cumulTokenCount,
				Model:           modelToUse,
				Generation:      generation,
				State:           state,
				Aux:             aux,
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

		if err := checkStreamCancellation(ctx, initialState, db, modelMessageID, agentResponseText, func() {}); err != nil {
			return err
		}

		seq, closer, err := provider.SendMessageStream(ctx, SessionParams{
			Contents:        currentHistory,
			ModelName:       modelToUse,
			SystemPrompt:    initialState.SystemPrompt,
			IncludeThoughts: true,
		})
		if err != nil {
			failCall(initialState.SessionId, err) // Mark the call as failed
			// Save a model_error message to the database
			errorMessage := fmt.Sprintf("LLM call failed: %v", err)
			if errors.Is(err, context.Canceled) {
				errorMessage = "user canceled request"
			}
			// If a model message was already created, update it with the error
			if modelMessageID >= 0 {
				if err := UpdateMessageContent(db, modelMessageID, errorMessage); err != nil {
					log.Printf("Failed to update initial model message with error: %v", err)
				}
			} else { // If no model message was created yet, add a new error message
				if _, err := addMessageToCurrentSession(TypeModelError, errorMessage, nil, nil, "", ""); err != nil {
					log.Printf("Failed to add model error message to DB: %v", err)
				}
			}
			// Send error to frontend
			broadcastToSession(initialState.SessionId, EventError, errorMessage)
			return fmt.Errorf("CodeAssist API call failed: %w", err)
		}
		defer closer.Close() // This closes the server-initiated API request.

		hasFunctionCall := false

		for caResp := range seq {
			// Log UsageMetadata if available
			if caResp.Response.UsageMetadata != nil {
				lastUsageMetadata = caResp.Response.UsageMetadata

				// Update last user message's cumul_token_count with PromptTokenCount
				if lastUsageMetadata.PromptTokenCount > 0 && lastUserMessageID != 0 {
					if err := UpdateMessageTokens(db, lastUserMessageID, lastUsageMetadata.PromptTokenCount); err != nil {
						log.Printf("Failed to update cumul_token_count for user message %d: %v", lastUserMessageID, err)
					}
					broadcastToSession(
						initialState.SessionId,
						EventCumulTokenCount,
						fmt.Sprintf("%d\n%d", lastUserMessageID, lastUsageMetadata.PromptTokenCount))
				}
			}

			if err := checkStreamCancellation(ctx, initialState, db, modelMessageID, agentResponseText, func() {}); err != nil {
				return err
			}

			if len(caResp.Response.Candidates) == 0 {
				continue
			}
			if len(caResp.Response.Candidates[0].Content.Parts) == 0 {
				continue
			}
			for _, part := range caResp.Response.Candidates[0].Content.Parts {
				// ThoughtSignature should be retained in order to correctly reconstruct the original Parts
				state := part.ThoughtSignature

				// Check if a non-text part interrupts the current text stream
				if (part.FunctionCall != nil || part.Thought) && modelMessageID >= 0 {
					// Finalize the current model message before processing the non-text part
					if err := UpdateMessageContent(db, modelMessageID, agentResponseText); err != nil {
						log.Printf("Failed to finalize model message before interruption: %v", err)
					}
					agentResponseText = "" // Reset for the next text block
					modelMessageID = -1    // Reset ID, a new one will be created for next text block
				}

				if part.FunctionCall != nil {
					// Immediately broadcast function call
					fc := *part.FunctionCall
					fcJson, _ := json.Marshal(fc)
					messageID, err := addMessageToCurrentSession(TypeFunctionCall, string(fcJson), nil, nil, state, "")
					if err != nil {
						return logAndErrorf(err, "Failed to save function call message")
					}
					argsJson, _ := json.Marshal(fc.Args)
					formattedData := fmt.Sprintf("%d\n%s\n%s", messageID, fc.Name, string(argsJson))
					broadcastToSession(initialState.SessionId, EventFunctionCall, formattedData)

					// Add to current history and functionCalls for later execution
					currentHistory = append(currentHistory, Content{Role: RoleModel, Parts: []Part{{FunctionCall: &fc}}})
					hasFunctionCall = true

					toolResults, err := CallToolFunction(ctx, fc, ToolHandlerParams{
						ModelName: modelToUse,
						SessionId: initialState.SessionId,
						BranchId:  initialState.PrimaryBranchID,
					})
					if err != nil {
						log.Printf("Error executing function %s: %v", fc.Name, err)

						var pendingConfirmation *PendingConfirmation
						if errors.As(err, &pendingConfirmation) {
							// Handle PendingConfirmation error
							confirmationDataBytes, marshalErr := json.Marshal(pendingConfirmation.Data)
							if marshalErr != nil {
								broadcastToSession(initialState.SessionId, EventError, fmt.Sprintf("Failed to process confirmation: %v", marshalErr))
								return logAndErrorf(marshalErr, "Failed to marshal pending confirmation data")
							}
							confirmationData := string(confirmationDataBytes)

							// Update branch pending_confirmation
							if err := UpdateBranchPendingConfirmation(db, initialState.PrimaryBranchID, confirmationData); err != nil {
								broadcastToSession(initialState.SessionId, EventError, fmt.Sprintf("Failed to update confirmation status: %v", err))
								return logAndErrorf(err, "Failed to update branch pending_confirmation")
							}

							// Send P event to frontend
							broadcastToSession(initialState.SessionId, EventPendingConfirmation, confirmationData)

							// Stop streaming
							return fmt.Errorf("user confirmation pending")
						}

						toolResults.Value = map[string]interface{}{"error": err.Error()}
					}

					fr := FunctionResponse{Name: fc.Name, Response: toolResults.Value}
					frJson, _ := json.Marshal(fr)
					var promptTokens *int
					if lastUsageMetadata != nil && lastUsageMetadata.PromptTokenCount > 0 {
						t := lastUsageMetadata.PromptTokenCount
						promptTokens = &t
					}
					messageID, err = addMessageToCurrentSession(TypeFunctionResponse, string(frJson), toolResults.Attachments, promptTokens, state, "")
					if err != nil {
						return logAndErrorf(err, "Failed to save function response message")
					}
					payload := FunctionResponsePayload{
						Response:    toolResults.Value,
						Attachments: toolResults.Attachments,
					}
					payloadJson, err := json.Marshal(payload)
					if err != nil {
						log.Printf("Failed to marshal EventFunctionResponse payload: %v", err)
						payloadJson = []byte("{}") // Send empty object on error
					}

					formattedData = fmt.Sprintf("%d\n%s\n%s", messageID, fc.Name, string(payloadJson))
					broadcastToSession(initialState.SessionId, EventFunctionResponse, formattedData)

					// Create the initial FunctionResponse part
					frPart := Part{FunctionResponse: &fr}
					partsForContent := []Part{frPart}

					// Add parts for attachments
					for _, attachment := range toolResults.Attachments {
						// Retrieve blob data from DB using hash
						dbFromContext, err := getDbFromContext(ctx)
						if err != nil {
							log.Printf("Failed to get DB from context for GetBlob: %v", err)
							continue // Skip this attachment
						}
						blobData, err := GetBlob(dbFromContext, attachment.Hash)
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

					currentHistory = append(currentHistory, Content{Role: RoleUser, Parts: partsForContent})

					continue // Continue processing other parts in the same caResp
				} else if part.ExecutableCode != nil {
					// Convert ExecutableCode to FunctionCall with special name
					executableCode := part.ExecutableCode
					argsBytes, err := json.Marshal(executableCode)
					if err != nil {
						log.Printf("Error marshaling ExecutableCode to JSON: %v", err)
						continue
					}
					var argsMap map[string]interface{}
					if err := json.Unmarshal(argsBytes, &argsMap); err != nil {
						log.Printf("Error unmarshaling ExecutableCode JSON to map: %v", err)
						continue
					}
					fc := FunctionCall{
						Name: GeminiCodeExecutionToolName,
						Args: argsMap,
					}
					fcJson, _ := json.Marshal(fc)
					messageID, err := addMessageToCurrentSession(TypeFunctionCall, string(fcJson), nil, nil, state, "")
					if err != nil {
						return logAndErrorf(err, "Failed to save executable code message")
					}
					formattedData := fmt.Sprintf("%d\n%s\n%s", messageID, fc.Name, string(argsBytes))
					broadcastToSession(initialState.SessionId, EventFunctionCall, formattedData)

					currentHistory = append(currentHistory, Content{Role: RoleModel, Parts: []Part{{FunctionCall: &fc}}})
					continue
				} else if part.CodeExecutionResult != nil {
					// Convert CodeExecutionResult to FunctionResponse with special name
					codeExecutionResult := part.CodeExecutionResult
					fr := FunctionResponse{
						Name:     GeminiCodeExecutionToolName,
						Response: codeExecutionResult,
					}
					frJson, _ := json.Marshal(fr)
					var promptTokens *int
					if lastUsageMetadata != nil && lastUsageMetadata.PromptTokenCount > 0 {
						t := lastUsageMetadata.PromptTokenCount
						promptTokens = &t
					}
					messageID, err := addMessageToCurrentSession(TypeFunctionResponse, string(frJson), nil, promptTokens, state, "")
					if err != nil {
						return logAndErrorf(err, "Failed to save code execution result message")
					}
					payload := FunctionResponsePayload{
						Response: map[string]interface{}{
							"outcome": codeExecutionResult.Outcome,
							"output":  codeExecutionResult.Output,
						},
					}
					payloadJson, err := json.Marshal(payload)
					if err != nil {
						log.Printf("Failed to marshal EventFunctionResponse payload for code execution result: %v", err)
						payloadJson = []byte("{}") // Send empty object on error
					}
					formattedData := fmt.Sprintf("%d\n%s\n%s", messageID, fr.Name, string(payloadJson))
					broadcastToSession(initialState.SessionId, EventFunctionResponse, formattedData)

					currentHistory = append(currentHistory, Content{Role: RoleUser, Parts: []Part{{FunctionResponse: &fr}}})
					continue
				}

				if part.Text == "" {
					continue
				}

				// part.Thought determines whether part.Text is a thought or a model text
				if part.Thought {
					var thoughtText string
					matches := thoughtPattern.FindStringSubmatch(part.Text)
					if len(matches) > 2 {
						thoughtText = fmt.Sprintf("%s\n%s", matches[1], matches[2])
					} else {
						thoughtText = fmt.Sprintf("Thinking...\n%s", part.Text)
					}

					messageID, err := addMessageToCurrentSession(TypeThought, thoughtText, nil, nil, state, "")
					if err != nil {
						log.Printf("Failed to save thought message: %v", err)
					}
					// Broadcast thought immediately
					broadcastToSession(initialState.SessionId, EventThought, fmt.Sprintf("%d\n%s", messageID, thoughtText))
				} else {
					if modelMessageID < 0 {
						if state != "" {
							// Parts with ThoughtSignature should not be concatenated unlike normal Text parts.
							// However practically ThoughtSignature only appears after a run of Thought parts,
							// so if we record the length of the first Text part bearing ThoughtSignature,
							// we are able to recover the original signature-bearing Part (plus remaining concatenated parts).
							state = fmt.Sprintf("%d,%s", len(part.Text), state)
						}
						// Initialize agentResponseText for the new model message
						agentResponseText = ""
						// Add the initial model message to DB with empty text
						modelMessageID, err = addMessageToCurrentSession(TypeModelText, "", nil, nil, state, "")
						if err != nil {
							return logAndErrorf(err, "Failed to add new model message to DB")
						}
					}

					agentResponseText += part.Text // Accumulate text for DB update

					// Update the message content in DB immediately for all parts
					if err := UpdateMessageContent(db, modelMessageID, agentResponseText); err != nil {
						log.Printf("Failed to update model message content: %v", err)
					}

					// Send only the current text chunk to the frontend
					broadcastToSession(initialState.SessionId, EventModelMessage, fmt.Sprintf("%d\n%s", modelMessageID, part.Text))
				}
			}
		}

		addCancelErrorMessage := func() {
			// Add a separate error message to the database
			if _, err := addMessageToCurrentSession(TypeModelError, "user canceled request", nil, nil, "", ""); err != nil {
				log.Printf("Failed to add error message to DB: %v", err)
			}
		}

		// Check if context was cancelled after stream ended
		if err := checkStreamCancellation(ctx, initialState, db, modelMessageID, agentResponseText, addCancelErrorMessage); err != nil {
			return err
		}

		// Model has generated all messages and nothing to ask
		if !hasFunctionCall {
			break
		}
	}

	broadcastToSession(initialState.SessionId, EventComplete, "")

	// Small delay to allow all clients to receive EventComplete before removeCall is executed
	time.Sleep(50 * time.Millisecond)

	// Finalize the last model message if any text was streamed
	if modelMessageID >= 0 {
		if err := UpdateMessageContent(db, modelMessageID, agentResponseText); err != nil {
			return logAndErrorf(err, "Failed to update final agent response")
		}
	}

	// Need to wait for inferAndSetSessionName so that the connection remains intact.
	var inferWg sync.WaitGroup

	// Only infer session name if inferSessionName is true and the name is still empty
	if inferSessionName {
		currentSession, err := GetSession(db, initialState.SessionId)
		if err != nil {
			log.Printf("streamLLMResponse: Failed to get session %s for initial name check: %v", initialState.SessionId, err)
			// If GetSession fails, assume name might be missing and attempt inference.
		}

		if currentSession.Name == "" { // Only infer if the session name is currently empty
			inferWg.Add(1)
			// Infer session name after streaming is complete
			go func() {
				addSseWriter(initialState.SessionId, sseW) // Increases the reference count and prevents it from being closed early
				defer func() {
					inferWg.Done()
					removeSseWriter(initialState.SessionId, sseW)
				}()

				userMsg := ""
				if len(initialState.History) > 0 && len(initialState.History[0].Parts) > 0 && initialState.History[0].Parts[0].Text != "" {
					userMsg = initialState.History[0].Parts[0].Text
				}
				inferAndSetSessionName(db, initialState.SessionId, userMsg, sseW, modelToUse)
			}()
		}
	}

	var finalTotalTokenCount *int
	if lastUsageMetadata != nil && lastUsageMetadata.TotalTokenCount > 0 {
		t := lastUsageMetadata.TotalTokenCount
		finalTotalTokenCount = &t
	}

	// Update the final message with token count if available
	// This applies to the last model message that was streamed
	if modelMessageID >= 0 && finalTotalTokenCount != nil {
		if err := UpdateMessageTokens(db, modelMessageID, *finalTotalTokenCount); err != nil {
			log.Printf("Failed to update final message tokens: %v", err)
		}
		broadcastToSession(initialState.SessionId, EventCumulTokenCount, fmt.Sprintf("%d\n%d", modelMessageID, *finalTotalTokenCount))
	}

	completeCall(initialState.SessionId) // Mark the call as completed
	inferWg.Wait()
	return nil
}

func checkStreamCancellation(
	ctx context.Context,
	initialState InitialState,
	db *sql.DB,
	modelMessageID int,
	agentResponseText string,
	cancelCallback func(),
) error {
	select {
	case <-ctx.Done():
		// API call was cancelled (either by client disconnect or explicit cancel)
		// Mark the call as cancelled in the manager
		failCall(initialState.SessionId, ctx.Err())
		// Update the message in DB with current accumulated text as-is
		if modelMessageID >= 0 && agentResponseText != "" { // Only update if a model message was created and has content
			if err := UpdateMessageContent(db, modelMessageID, agentResponseText); err != nil {
				log.Printf("Failed to update model message: %v", err)
			}
		}

		cancelCallback()

		// Send error to frontend
		broadcastToSession(initialState.SessionId, EventError, "user canceled request")
		return ctx.Err()
	default:
		// Continue with the LLM call
		return nil
	}
}

// inferAndSetSessionName infers the session name using LLM and updates it in the DB.
func inferAndSetSessionName(db *sql.DB, sessionId string, userMessage string, sseW *sseWriter, modelToUse string) {
	log.Printf("inferAndSetSessionName: Starting for session %s", sessionId)

	var inferredName string // Initialize to empty string

	defer func() {
		// This defer will execute at the end of the function, ensuring 'N' is sent.
		// If inferredName is still empty, it means inference failed or was skipped.
		sseW.sendServerEvent(EventSessionName, fmt.Sprintf("%s\n%s", sessionId, inferredName))
	}()

	if db == nil {
		log.Printf("inferAndSetSessionName: Database connection is nil for session %s", sessionId)
		return
	}

	session, err := GetSession(db, sessionId)
	if err != nil {
		log.Printf("Failed to get session %s for name inference: %v", sessionId, err)
		return // inferredName remains empty
	}
	if session.Name != "" { // If name is not empty, user has set it, do not infer
		inferredName = session.Name // Use existing name if already set
		return
	}

	// Check for (NAME: session name) comment ONLY FOR angel-eval model
	if modelToUse == "angel-eval" {
		nameCommentPattern := regexp.MustCompile(`\(NAME:\s*(.*?)\)`)
		matches := nameCommentPattern.FindStringSubmatch(userMessage)
		if len(matches) > 1 {
			extractedName := strings.TrimSpace(matches[1])
			if extractedName != "" {
				inferredName = extractedName
				if err := UpdateSessionName(db, sessionId, inferredName); err != nil {
					log.Printf("Failed to update session name from comment for %s: %v", sessionId, err)
				}
				log.Printf("inferAndSetSessionName: Inferred name from comment for session %s: %s", sessionId, inferredName)
				return // Name inferred from comment, no need for LLM
			}
		}
		// If angel-eval model and no comment found, skip LLM inference
		log.Printf("inferAndSetSessionName: Skipping LLM name inference for angel-eval model as no comment was found.")
		return // inferredName remains empty if no comment was found
	}

	// Existing LLM inference logic (only for non-angel-eval models)
	nameSystemPrompt := executePromptTemplate("session-name-prompt.md", nil)
	nameInputPrompt := executePromptTemplate("session-name-input.md", map[string]any{
		"UserMessage":  userMessage,
		"AgentMessage": "",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	provider, sessionNameGenParams := CurrentProviders[modelToUse].SubagentProviderAndParams(SubagentSessionNameTask)
	if provider == nil {
		log.Printf("inferAndSetSessionName: Unsupported model for session name inference: %s", modelToUse)
		return
	}

	oneShotResult, err := provider.GenerateContentOneShot(ctx, SessionParams{
		Contents: []Content{
			{
				Role:  RoleUser,
				Parts: []Part{{Text: nameInputPrompt}},
			},
		},
		ModelName:        modelToUse,
		SystemPrompt:     nameSystemPrompt,
		IncludeThoughts:  false,
		GenerationParams: &sessionNameGenParams,
	})
	if err != nil {
		log.Printf("Failed to infer session name for %s: %v", sessionId, err)
		return // inferredName remains empty
	}

	llmInferredNameText := strings.TrimSpace(oneShotResult.Text)
	if len(llmInferredNameText) > 100 || strings.Contains(llmInferredNameText, "\n") {
		log.Printf("Inferred name for session %s is invalid (too long or multi-line): %s", sessionId, llmInferredNameText)
		return // inferredName remains empty
	}

	inferredName = llmInferredNameText // Set inferredName only if successful

	if err := UpdateSessionName(db, sessionId, inferredName); err != nil {
		log.Printf("Failed to update session name for %s: %v", sessionId, err)
		// If DB update fails, inferredName is still the valid one, but DB might not reflect it.
		// We still send the inferredName to frontend, as it's the best we have.
		return
	}
	log.Printf("inferAndSetSessionName: Finished for session %s. Inferred name: %s", sessionId, inferredName)
}

// logAndErrorf logs an error and returns a new error that wraps the original.
func logAndErrorf(err error, format string, a ...interface{}) error {
	log.Printf(format+": %v", append(a, err)...)
	return fmt.Errorf(format+": %w", append(a, err)...)
}
