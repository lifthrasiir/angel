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
func streamGeminiResponse(db *sql.DB, initialState InitialState, sseW *sseWriter, lastUserMessageID int, wg *sync.WaitGroup) error {
	var agentResponseText string
	var lastUsageMetadata *UsageMetadata
	currentHistory := convertFrontendMessagesToContent(initialState.History)

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
	log.Printf("streamGeminiResponse: Sent initial state for session %s.", initialState.SessionId)

	// Goroutine to monitor client disconnection
	go func() {
		select {
		case <-ctx.Done():
			// Gemini API call context was cancelled (e.g., by explicit cancel request)
			// No need to do anything here, the main goroutine will handle it.
		}
	}()

	for {
		select {
		case <-ctx.Done():
			// API call was cancelled (either by client disconnect or explicit cancel)
			// Mark the call as cancelled in the manager
			failCall(initialState.SessionId, ctx.Err())
			return ctx.Err() // Return the context error
		default:
			// Continue with the Gemini API call
		}

		seq, closer, err := CurrentProvider.SendMessageStream(ctx, SessionParams{Contents: currentHistory, ModelName: DefaultGeminiModel, SystemPrompt: initialState.SystemPrompt, ThinkingConfig: &ThinkingConfig{IncludeThoughts: true}})
		if err != nil {
			failCall(initialState.SessionId, err) // Mark the call as failed
			// Save a model_error message to the database
			errorMessage := fmt.Sprintf("Gemini API call failed: %v", err)
			if errors.Is(err, context.Canceled) {
				errorMessage = "Request canceled by user"
			}
			if _, err := AddMessageToSession(db, initialState.SessionId, "model", errorMessage, "model_error", nil, nil); err != nil {
				log.Printf("Failed to save model_error message for API call failure: %v", err)
			}
			return fmt.Errorf("CodeAssist API call failed: %w", err)
		}
		defer closer.Close()

		var functionCalls []FunctionCall
		var modelResponseParts []Part

		for caResp := range seq {
			// Log UsageMetadata if available
			if caResp.Response.UsageMetadata != nil {
				lastUsageMetadata = caResp.Response.UsageMetadata
				log.Printf("UsageMetadata: PromptTokenCount=%d, CandidatesTokenCount=%d, TotalTokenCount=%d, ToolUsePromptTokenCount=%d, ThoughtsTokenCount=%d",
					lastUsageMetadata.PromptTokenCount,
					lastUsageMetadata.CandidatesTokenCount,
					lastUsageMetadata.TotalTokenCount,
					lastUsageMetadata.ToolUsePromptTokenCount,
					lastUsageMetadata.ThoughtsTokenCount)

				// Update last user message's cumul_token_count with PromptTokenCount
				if lastUsageMetadata.PromptTokenCount > 0 && lastUserMessageID != 0 {
					if err := UpdateMessageTokens(db, lastUserMessageID, lastUsageMetadata.PromptTokenCount); err != nil {
						log.Printf("Failed to update cumul_token_count for user message %d: %v", lastUserMessageID, err)
					}
				}
			}
			select {
			case <-ctx.Done():
				// Context was canceled, send a message to the frontend
				broadcastToSession(initialState.SessionId, EventError, "Request canceled by user")
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
				if part.FunctionCall != nil {
					functionCalls = append(functionCalls, *part.FunctionCall)
					argsJson, _ := json.Marshal(part.FunctionCall.Args)
					broadcastToSession(initialState.SessionId, EventFunctionCall, fmt.Sprintf("%s\n%s", part.FunctionCall.Name, string(argsJson)))
					continue
				}

				if part.Thought {
					var thoughtText string
					matches := thoughtPattern.FindStringSubmatch(part.Text)
					if len(matches) > 2 {
						thoughtText = fmt.Sprintf("%s\n%s", matches[1], matches[2])
					} else {
						thoughtText = fmt.Sprintf("Thinking...\n%s", part.Text)
					}

					broadcastToSession(initialState.SessionId, EventThought, thoughtText)
					if _, err := AddMessageToSession(db, initialState.SessionId, "thought", thoughtText, "thought", nil, nil); err != nil {
						log.Printf("Failed to save thought message: %v", err)
					}
					continue
				}

				if part.Text != "" {
					broadcastToSession(initialState.SessionId, EventModelMessage, part.Text)
					agentResponseText += part.Text
					modelResponseParts = append(modelResponseParts, part)
				}
			}
			if len(functionCalls) > 0 {
				break
			}
		}

		if len(functionCalls) > 0 {
			for _, fc := range functionCalls {
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
				broadcastToSession(initialState.SessionId, EventFunctionReply, string(responseJson))

				fcJson, _ := json.Marshal(fc)
				var totalTokens *int
				if lastUsageMetadata != nil && lastUsageMetadata.TotalTokenCount > 0 {
					t := lastUsageMetadata.TotalTokenCount
					totalTokens = &t
				}
				if _, err := AddMessageToSession(db, initialState.SessionId, "model", string(fcJson), "function_call", nil, totalTokens); err != nil {
					log.Printf("Failed to save function call: %v", err)
				}
				currentHistory = append(currentHistory, Content{Role: "model", Parts: []Part{{FunctionCall: &fc}}})

				fr := FunctionResponse{Name: fc.Name, Response: functionResponseValue}
				frJson, _ := json.Marshal(fr)
				var promptTokens *int
				if lastUsageMetadata != nil && lastUsageMetadata.PromptTokenCount > 0 {
					t := lastUsageMetadata.PromptTokenCount
					promptTokens = &t
				}
				if _, err := AddMessageToSession(db, initialState.SessionId, "user", string(frJson), "function_response", nil, promptTokens); err != nil {
					log.Printf("Failed to save function response: %v", err)
				}
				currentHistory = append(currentHistory, Content{Role: "user", Parts: []Part{{FunctionResponse: &fr}}})
			}
		} else {
			break
		}
	}

	broadcastToSession(initialState.SessionId, EventComplete, "")

	// Before saving the final agent response, delete any empty model messages
	if err := DeleteLastEmptyModelMessage(db, initialState.SessionId); err != nil {
		log.Printf("Failed to save last empty model message: %v", err)
	}

	// Infer session name after streaming is complete
	if wg != nil {
		wg.Add(1)
	}
	go func() {
		defer func() {
			if wg != nil {
				wg.Done()
			}
		}()
		inferAndSetSessionName(db, initialState.SessionId, initialState.History[0].Parts[0].Text, sseW, wg)
	}()

	var finalTotalTokenCount *int
	if lastUsageMetadata != nil && lastUsageMetadata.TotalTokenCount > 0 {
		t := lastUsageMetadata.TotalTokenCount
		finalTotalTokenCount = &t
	}

	if _, err := AddMessageToSession(db, initialState.SessionId, "model", agentResponseText, "text", nil, finalTotalTokenCount); err != nil {
		failCall(initialState.SessionId, err) // Mark the call as failed
		return fmt.Errorf("failed to save agent response: %w", err)
	}

	completeCall(initialState.SessionId) // Mark the call as completed
	return nil
}

// inferAndSetSessionName infers the session name using LLM and updates it in the DB.
func inferAndSetSessionName(db *sql.DB, sessionId string, userMessage string, sseW *sseWriter, wg *sync.WaitGroup) {
	if wg != nil {
		wg.Add(1)
		defer wg.Done()
	}

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

	nameSystemPrompt, nameInputPrompt := GetSessionNameInferencePrompts(userMessage, "")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	llmInferredName, err := CurrentProvider.GenerateContentOneShot(ctx, SessionParams{
		Contents: []Content{
			{
				Role:  "user",
				Parts: []Part{{Text: nameInputPrompt}},
			},
		}, ModelName: DefaultGeminiModel, SystemPrompt: nameSystemPrompt, ThinkingConfig: &ThinkingConfig{IncludeThoughts: false}})
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
}
