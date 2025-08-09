package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"
)

var thoughtPattern = regexp.MustCompile(`^\*\*(.*?)\*\*\n+(.*)\n*$`) // Moved from chat.go

// Helper function to stream Gemini API response
func streamGeminiResponse(db *sql.DB, initialState InitialState, sseW *sseWriter, lastUserMessageID int, modelToUse string) error {
	var agentResponseText string
	var lastUsageMetadata *UsageMetadata
	var finalTotalTokenCount *int
	currentHistory := convertFrontendMessagesToContent(db, initialState.History)

	// Track the ID of the last message added to the database
	lastAddedMessageID := lastUserMessageID
	var parentMessageID *int // Declare parentMessageID here

	// Create a cancellable context for the Gemini API call
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // Ensure context is cancelled when function exits

	// Register the call with the call manager
	if err := startCall(initialState.SessionId, cancel); err != nil {
		log.Printf("streamGeminiResponse: Failed to start call for session %s: %v", initialState.SessionId, err)
		broadcastToSession(initialState.SessionId, EventError, err.Error())
		return err
	}
	defer removeCall(initialState.SessionId) // Ensure call is removed from manager when function exits

	// Send initial state as a single SSE event (Event type 0: active call, broadcasting will start)
	initialStateJSON, err := json.Marshal(initialState) // initialState struct를 JSON으로 마샬링
	if err != nil {
		log.Printf("streamGeminiResponse: Failed to marshal initial state: %v", err)
		return err
	}
	sseW.sendServerEvent(EventInitialState, string(initialStateJSON))

	// Initialize modelMessageID to negative. It's used for the current streaming model message.
	modelMessageID := -1

	provider, ok := CurrentProviders[modelToUse]
	if !ok {
		return fmt.Errorf("unsupported model: %s", modelToUse)
	}

	for {
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
			// Send error to frontend
			broadcastToSession(initialState.SessionId, EventError, "user canceled request")
			return ctx.Err() // Return the context error
		default:
			// Continue with the Gemini API call
		}

		seq, closer, err := provider.SendMessageStream(ctx, SessionParams{
			Contents:       currentHistory,
			ModelName:      modelToUse,
			SystemPrompt:   initialState.SystemPrompt,
			ThinkingConfig: &ThinkingConfig{IncludeThoughts: true},
		})
		if err != nil {
			failCall(initialState.SessionId, err) // Mark the call as failed
			// Save a model_error message to the database
			errorMessage := fmt.Sprintf("Gemini API call failed: %v", err)
			if errors.Is(err, context.Canceled) {
				errorMessage = "user canceled request"
			}
			// If a model message was already created, update it with the error
			if modelMessageID >= 0 {
				if err := UpdateMessageContent(db, modelMessageID, errorMessage); err != nil {
					log.Printf("Failed to update initial model message with error: %v", err)
				}
			} else { // If no model message was created yet, add a new error message
				if _, err := AddMessageToSession(ctx, db, Message{
					SessionID:       initialState.SessionId,
					BranchID:        initialState.PrimaryBranchID,
					ParentMessageID: nil,
					ChosenNextID:    nil,
					Role:            "model",
					Text:            errorMessage,
					Type:            "model_error",
					Attachments:     nil,
					CumulTokenCount: nil,
					Model:           modelToUse,
				}); err != nil {
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
					broadcastToSession(initialState.SessionId, EventCumulTokenCount, fmt.Sprintf("%d\n%d", lastUserMessageID, lastUsageMetadata.PromptTokenCount))
				}
			}
			select {
			case <-ctx.Done():
				// Context was canceled, send a message to the frontend
				broadcastToSession(initialState.SessionId, EventError, "Request canceled by user")
				if modelMessageID >= 0 && agentResponseText != "" { // Only update if a model message was created
					if err := UpdateMessageContent(db, modelMessageID, agentResponseText); err != nil {
						log.Printf("Failed to update model message with cancelled status: %v", err)
					}
				}
				return ctx.Err() // Return the context error
			default:
				// Continue processing the response
			}
			if len(caResp.Response.Candidates) == 0 {
				continue
			}
			if len(caResp.Response.Candidates[0].Content.Parts) == 0 {
				continue
			}
			for _, part := range caResp.Response.Candidates[0].Content.Parts {
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
					if lastAddedMessageID != 0 {
						parentMessageID = &lastAddedMessageID
					} else {
						parentMessageID = nil
					}
					messageID, err := AddMessageToSession(ctx, db, Message{
						SessionID:       initialState.SessionId,
						BranchID:        initialState.PrimaryBranchID,
						ParentMessageID: parentMessageID,
						ChosenNextID:    nil,
						Role:            "model",
						Text:            string(fcJson),
						Type:            "function_call",
						Attachments:     nil,
						CumulTokenCount: nil,
						Model:           modelToUse,
					})
					if err != nil {
						log.Printf("Failed to save function call message: %v", err)
						return fmt.Errorf("failed to save function call message: %w", err)
					}
					// Update chosen_next_id of the parent message
					if parentMessageID != nil {
						if err := UpdateMessageChosenNextID(db, *parentMessageID, &messageID); err != nil {
							log.Printf("Failed to update chosen_next_id for message %d: %v", *parentMessageID, err)
						}
					}
					lastAddedMessageID = messageID
					argsJson, _ := json.Marshal(fc.Args)
					formattedData := fmt.Sprintf("%d\n%s\n%s", messageID, fc.Name, string(argsJson))
					broadcastToSession(initialState.SessionId, EventFunctionCall, formattedData)

					// Add to current history and functionCalls for later execution
					currentHistory = append(currentHistory, Content{Role: "model", Parts: []Part{{FunctionCall: &fc}}})
					hasFunctionCall = true

					functionResponseValue, err := callFunction(fc)
					if err != nil {
						log.Printf("Error executing function %s: %v", fc.Name, err)
						functionResponseValue = map[string]interface{}{"error": err.Error()}
					}

					responseJson, err := json.Marshal(functionResponseValue)
					if err != nil {
						log.Printf("Failed to marshal function response for frontend: %v", err)
						responseJson = []byte(fmt.Sprintf(`{"error": "%v"}`, err))
					}

					fr := FunctionResponse{Name: fc.Name, Response: functionResponseValue}
					frJson, _ := json.Marshal(fr)
					var promptTokens *int
					if lastUsageMetadata != nil && lastUsageMetadata.PromptTokenCount > 0 {
						t := lastUsageMetadata.PromptTokenCount
						promptTokens = &t
					}
					if lastAddedMessageID != 0 {
						parentMessageID = &lastAddedMessageID
					} else {
						parentMessageID = nil
					}
					messageID, err = AddMessageToSession(ctx, db, Message{
						SessionID:       initialState.SessionId,
						BranchID:        initialState.PrimaryBranchID,
						ParentMessageID: parentMessageID,
						ChosenNextID:    nil,
						Role:            "user",
						Text:            string(frJson),
						Type:            "function_response",
						Attachments:     nil,
						CumulTokenCount: promptTokens,
						Model:           modelToUse,
					})
					if err != nil {
						log.Printf("Failed to save function response message: %v", err)
						return fmt.Errorf("failed to save function response message: %w", err)
					}
					// Update chosen_next_id of the parent message
					if parentMessageID != nil {
						if err := UpdateMessageChosenNextID(db, *parentMessageID, &messageID); err != nil {
							log.Printf("Failed to update chosen_next_id for message %d: %v", *parentMessageID, err)
						}
					}
					lastAddedMessageID = messageID
					formattedData = fmt.Sprintf("%d\n%s\n%s", messageID, fc.Name, string(responseJson))
					broadcastToSession(initialState.SessionId, EventFunctionReply, formattedData)
					currentHistory = append(currentHistory, Content{Role: "user", Parts: []Part{{FunctionResponse: &fr}}})

					continue // Continue processing other parts in the same caResp
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

					// Save thought message to DB
					if lastAddedMessageID != 0 {
						parentMessageID = &lastAddedMessageID
					} else {
						parentMessageID = nil
					}
					messageID, err := AddMessageToSession(ctx, db, Message{
						SessionID:       initialState.SessionId,
						BranchID:        initialState.PrimaryBranchID,
						ParentMessageID: parentMessageID,
						ChosenNextID:    nil,
						Role:            "thought",
						Text:            thoughtText,
						Type:            "thought",
						Attachments:     nil,
						CumulTokenCount: nil,
						Model:           modelToUse,
					})
					if err != nil {
						log.Printf("Failed to save thought message: %v", err)
					}
					// Update chosen_next_id of the parent message
					if parentMessageID != nil {
						if err := UpdateMessageChosenNextID(db, *parentMessageID, &messageID); err != nil {
							log.Printf("Failed to update chosen_next_id for message %d: %v", *parentMessageID, err)
						}
					}
					lastAddedMessageID = messageID
					// Broadcast thought immediately
					broadcastToSession(initialState.SessionId, EventThought, fmt.Sprintf("%d\n%s", messageID, thoughtText))
				} else {
					if modelMessageID < 0 {
						// Initialize agentResponseText for the new model message
						agentResponseText = "" // Initialize to empty string
						// Set parentMessageID for the new model message
						if lastAddedMessageID != 0 {
							parentMessageID = &lastAddedMessageID
						} else {
							parentMessageID = nil
						}
						// Add the initial model message to DB with empty text
						modelMessageID, err = AddMessageToSession(ctx, db, Message{
							SessionID:       initialState.SessionId,
							BranchID:        initialState.PrimaryBranchID,
							ParentMessageID: parentMessageID,
							ChosenNextID:    nil,
							Role:            "model",
							Text:            "",
							Type:            "text",
							Attachments:     nil,
							CumulTokenCount: nil,
							Model:           modelToUse,
						})
						if err != nil {
							log.Printf("Failed to add new model message to DB: %v", err)
							return fmt.Errorf("failed to add new model message to DB: %w", err)
						}
						// Update chosen_next_id of the parent message (if any)
						if parentMessageID != nil {
							if err := UpdateMessageChosenNextID(db, *parentMessageID, &modelMessageID); err != nil {
								log.Printf("Failed to update chosen_next_id for message %d: %v", *parentMessageID, err)
							}
						}
					}

					agentResponseText += part.Text // Accumulate text for DB update

					// Update the message content in DB immediately for all parts
					if err := UpdateMessageContent(db, modelMessageID, agentResponseText); err != nil {
						log.Printf("Failed to update model message content: %v", err)
					}
					lastAddedMessageID = modelMessageID // Update lastAddedMessageID for every part

					// Send only the current text chunk to the frontend
					broadcastToSession(initialState.SessionId, EventModelMessage, fmt.Sprintf("%d\n%s", modelMessageID, part.Text))
				}
			}
		}

		// Check if context was cancelled after stream ended
		select {
		case <-ctx.Done():
			// Stream was cancelled, send error and return
			failCall(initialState.SessionId, ctx.Err())
			// Finalize the current model message as-is without adding cancellation text
			if modelMessageID >= 0 && agentResponseText != "" {
				if err := UpdateMessageContent(db, modelMessageID, agentResponseText); err != nil {
					log.Printf("Failed to update final model message: %v", err)
				}
			}

			// Add a separate error message to the database
			errorMessageID, err := AddMessageToSession(ctx, db, Message{
				SessionID:       initialState.SessionId,
				BranchID:        initialState.PrimaryBranchID,
				ParentMessageID: &lastAddedMessageID,
				ChosenNextID:    nil,
				Role:            "model",
				Text:            "user canceled request",
				Type:            "error",
				Attachments:     nil,
				CumulTokenCount: nil,
				Model:           modelToUse,
			})
			if err != nil {
				log.Printf("Failed to add error message to DB: %v", err)
			} else {
				// Update chosen_next_id of the parent message to point to the error message
				if lastAddedMessageID != 0 {
					if err := UpdateMessageChosenNextID(db, lastAddedMessageID, &errorMessageID); err != nil {
						log.Printf("Failed to update chosen_next_id for cancelled message %d: %v", lastAddedMessageID, err)
					}
				}
			}

			broadcastToSession(initialState.SessionId, EventError, "user canceled request")
			return ctx.Err()
		default:
			// Continue with normal processing
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
			log.Printf("Failed to update final agent response: %v", err)
			return fmt.Errorf("failed to update final agent response: %w", err)
		}
	}

	// Need to wait for inferAndSetSessionName so that the connection remains intact.
	var inferWg sync.WaitGroup
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
	nameSystemPrompt, nameInputPrompt := GetSessionNameInferencePrompts(userMessage, "")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	provider, ok := CurrentProviders[modelToUse]
	if !ok {
		log.Printf("inferAndSetSessionName: Unsupported model: %s", modelToUse)
		return
	}

	llmInferredName, err := provider.GenerateContentOneShot(ctx, SessionParams{
		Contents: []Content{
			{
				Role:  "user",
				Parts: []Part{{Text: nameInputPrompt}},
			},
		},
		ModelName:      modelToUse,
		SystemPrompt:   nameSystemPrompt,
		ThinkingConfig: &ThinkingConfig{IncludeThoughts: false},
	})
	if err != nil {
		log.Printf("Failed to infer session name for %s: %v", sessionId, err)
		return // inferredName remains empty
	}

	llmInferredName = strings.TrimSpace(llmInferredName)
	if len(llmInferredName) > 100 || strings.Contains(llmInferredName, "\n") {
		log.Printf("Inferred name for session %s is invalid (too long or multi-line): %s", sessionId, llmInferredName)
		return // inferredName remains empty
	}

	inferredName = llmInferredName // Set inferredName only if successful

	if err := UpdateSessionName(db, sessionId, inferredName); err != nil {
		log.Printf("Failed to update session name for %s: %v", sessionId, err)
		// If DB update fails, inferredName is still the valid one, but DB might not reflect it.
		// We still send the inferredName to frontend, as it's the best we have.
		return
	}
	log.Printf("inferAndSetSessionName: Finished for session %s. Inferred name: %s", sessionId, inferredName)
}
