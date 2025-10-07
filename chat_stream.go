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
	mc *MessageChain,
	inferSessionName bool,
	callStartTime time.Time,
	fullHistoryForLLM []FrontendMessage,
) error {
	var agentResponseText string
	var lastUsageMetadata *UsageMetadata
	var inlineDataCounter int = 0 // Counter for sequential inlineData filenames
	currentHistory := convertFrontendMessagesToContent(db, fullHistoryForLLM)

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

	provider, ok := CurrentProviders[mc.LastMessageModel]
	if !ok {
		return fmt.Errorf("unsupported model: %s", mc.LastMessageModel)
	}

	for {
		if err := checkStreamCancellation(ctx, initialState, db, modelMessageID, agentResponseText, func() {}); err != nil {
			return err
		}

		seq, closer, err := provider.SendMessageStream(ctx, SessionParams{
			Contents:        currentHistory,
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
				if _, err := mc.Add(ctx, db, Message{Type: TypeModelError, Text: errorMessage}); err != nil {
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
				if lastUsageMetadata.PromptTokenCount > 0 && mc.LastMessageID != 0 {
					if err := UpdateMessageTokens(db, mc.LastMessageID, lastUsageMetadata.PromptTokenCount); err != nil {
						log.Printf("Failed to update cumul_token_count for user message %d: %v", mc.LastMessageID, err)
					}
					broadcastToSession(
						initialState.SessionId,
						EventCumulTokenCount,
						fmt.Sprintf("%d\n%d", mc.LastMessageID, lastUsageMetadata.PromptTokenCount))
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
				if (part.FunctionCall != nil || part.Thought || part.InlineData != nil) && modelMessageID >= 0 {
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
					hasFunctionCall = true

					fcJson, _ := json.Marshal(fc)
					newMessage, err := mc.Add(ctx, db, Message{Type: TypeFunctionCall, Text: string(fcJson), State: state})
					if err != nil {
						return logAndErrorf(err, "Failed to save function call message")
					}

					argsJson, _ := json.Marshal(fc.Args)
					formattedData := fmt.Sprintf("%d\n%s\n%s", newMessage.ID, fc.Name, string(argsJson))
					broadcastToSession(initialState.SessionId, EventFunctionCall, formattedData)

					toolResults, err := CallToolFunction(ctx, fc, ToolHandlerParams{
						ModelName: mc.LastMessageModel,
						SessionId: initialState.SessionId,
						BranchId:  initialState.PrimaryBranchID,
					})
					if err != nil {
						log.Printf("Error executing function %s: %v", fc.Name, err)

						var pendingConfirmation *PendingConfirmation
						if errors.As(err, &pendingConfirmation) {
							return handlePendingConfirmation(db, initialState, pendingConfirmation)
						} else {
							toolResults.Value = map[string]interface{}{"error": err.Error()}
						}
					}

					fr := FunctionResponse{Name: fc.Name, Response: toolResults.Value}

					frJson, _ := json.Marshal(fr)
					var promptTokens *int
					if lastUsageMetadata != nil && lastUsageMetadata.PromptTokenCount > 0 {
						t := lastUsageMetadata.PromptTokenCount
						promptTokens = &t
					}
					newMessage, err = mc.Add(ctx, db, Message{
						Type:            TypeFunctionResponse,
						Text:            string(frJson),
						Attachments:     toolResults.Attachments,
						CumulTokenCount: promptTokens,
						State:           state,
					})
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
					formattedData = fmt.Sprintf("%d\n%s\n%s", newMessage.ID, fc.Name, string(payloadJson))
					broadcastToSession(initialState.SessionId, EventFunctionResponse, formattedData)

					// Add to current history for later execution
					currentHistory = append(currentHistory,
						Content{Role: RoleModel, Parts: []Part{{FunctionCall: &fc}}},
						Content{Role: RoleUser, Parts: appendAttachmentParts(db, toolResults, []Part{{FunctionResponse: &fr}})},
					)
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
					newMessage, err := mc.Add(ctx, db, Message{Type: TypeFunctionCall, Text: string(fcJson), State: state})
					if err != nil {
						return logAndErrorf(err, "Failed to save executable code message")
					}
					formattedData := fmt.Sprintf("%d\n%s\n%s", newMessage.ID, fc.Name, string(argsBytes))
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
					newMessage, err := mc.Add(ctx, db, Message{Type: TypeFunctionResponse, Text: string(frJson), CumulTokenCount: promptTokens, State: state})
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
					formattedData := fmt.Sprintf("%d\n%s\n%s", newMessage.ID, fr.Name, string(payloadJson))
					broadcastToSession(initialState.SessionId, EventFunctionResponse, formattedData)

					currentHistory = append(currentHistory, Content{Role: RoleUser, Parts: []Part{{FunctionResponse: &fr}}})
					continue
				} else if part.InlineData != nil {
					// Handle inlineData part - create a separate message with attachment
					inlineData := part.InlineData

					// Increment counter for this inlineData
					inlineDataCounter++

					// Decode base64 data
					data, err := base64.StdEncoding.DecodeString(inlineData.Data)
					if err != nil {
						log.Printf("Failed to decode inlineData base64: %v", err)
						continue
					}

					// Create attachment from inlineData with a generated filename
					attachment := FileAttachment{
						FileName: generateFilenameFromMimeType(inlineData.MimeType, inlineDataCounter),
						MimeType: inlineData.MimeType,
						Data:     data,
					}

					// Create a message with empty text but with attachment
					newMessage, err := mc.Add(ctx, db, Message{
						Type:        TypeModelText,
						Text:        "", // Empty text for inlineData messages
						State:       state,
						Attachments: []FileAttachment{attachment},
					})
					if err != nil {
						log.Printf("Failed to save inlineData message: %v", err)
						continue
					}

					// Use the attachment from newMessage which should have the hash set
					var attachmentToSend FileAttachment
					if len(newMessage.Attachments) > 0 {
						attachmentToSend = newMessage.Attachments[0]
					} else {
						// Fallback to original attachment (shouldn't happen)
						attachmentToSend = attachment
						log.Printf("Warning: newMessage has no attachments, using original attachment")
					}

					// Create inline data payload with hash keys (data is already stored in blobs table)
					inlineDataPayload := InlineDataPayload{
						MessageId:   fmt.Sprintf("%d", newMessage.ID),
						Attachments: []FileAttachment{attachmentToSend},
					}
					payloadJson, err := json.Marshal(inlineDataPayload)
					if err != nil {
						log.Printf("Failed to marshal inline data payload: %v", err)
						continue
					}

					// Broadcast inline data event with hash keys
					broadcastToSession(initialState.SessionId, EventInlineData, string(payloadJson))

					// Add to current history
					currentHistory = append(currentHistory, Content{
						Role:  RoleModel,
						Parts: []Part{{InlineData: inlineData}},
					})
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

					newMessage, err := mc.Add(ctx, db, Message{Type: TypeThought, Text: thoughtText, State: state})
					if err != nil {
						log.Printf("Failed to save thought message: %v", err)
					}
					// Broadcast thought immediately
					broadcastToSession(initialState.SessionId, EventThought, fmt.Sprintf("%d\n%s", newMessage.ID, thoughtText))
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
						newMessage, err := mc.Add(ctx, db, Message{Type: TypeModelText, Text: "", State: state})
						if err != nil {
							return logAndErrorf(err, "Failed to add new model message to DB")
						}
						modelMessageID = newMessage.ID
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
			if _, err := mc.Add(ctx, db, Message{Type: TypeModelError, Text: "user canceled request"}); err != nil {
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
				inferAndSetSessionName(db, initialState.SessionId, userMsg, sseW, mc.LastMessageModel)
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

func appendAttachmentParts(db *sql.DB, toolResults ToolHandlerResults, partsForContent []Part) []Part {
	for _, attachment := range toolResults.Attachments {
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
	return partsForContent
}

func handlePendingConfirmation(db *sql.DB, initialState InitialState, pendingConfirmation *PendingConfirmation) error {
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

// generateFilenameFromMimeType generates a filename based on MIME type with sequential numbering
func generateFilenameFromMimeType(mimeType string, counter int) string {
	var extension, prefix string

	switch mimeType {
	case "image/png":
		extension = ".png"
		prefix = "generated_image"
	case "image/jpeg":
		extension = ".jpg"
		prefix = "generated_image"
	case "image/gif":
		extension = ".gif"
		prefix = "generated_image"
	case "image/webp":
		extension = ".webp"
		prefix = "generated_image"
	case "image/svg+xml":
		extension = ".svg"
		prefix = "generated_image"
	case "audio/mpeg":
		extension = ".mp3"
		prefix = "generated_audio"
	case "audio/wav":
		extension = ".wav"
		prefix = "generated_audio"
	case "audio/ogg":
		extension = ".ogg"
		prefix = "generated_audio"
	case "video/mp4":
		extension = ".mp4"
		prefix = "generated_video"
	case "video/webm":
		extension = ".webm"
		prefix = "generated_video"
	case "application/pdf":
		extension = ".pdf"
		prefix = "generated_document"
	case "text/plain":
		extension = ".txt"
		prefix = "generated_text"
	case "text/markdown":
		extension = ".md"
		prefix = "generated_text"
	case "application/json":
		extension = ".json"
		prefix = "generated_data"
	case "text/csv":
		extension = ".csv"
		prefix = "generated_data"
	default:
		// For unknown MIME types, generate a generic filename
		extension = ""
		prefix = "generated_file"
	}

	return fmt.Sprintf("%s_%03d%s", prefix, counter, extension)
}

// logAndErrorf logs an error and returns a new error that wraps the original.
func logAndErrorf(err error, format string, a ...interface{}) error {
	log.Printf(format+": %v", append(a, err)...)
	return fmt.Errorf(format+": %w", append(a, err)...)
}
