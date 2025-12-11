package chat

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	. "github.com/lifthrasiir/angel/gemini"
	"github.com/lifthrasiir/angel/internal/database"
	"github.com/lifthrasiir/angel/internal/env"
	"github.com/lifthrasiir/angel/internal/llm"
	"github.com/lifthrasiir/angel/internal/prompts"
	"github.com/lifthrasiir/angel/internal/tool"
	. "github.com/lifthrasiir/angel/internal/types"
)

type InitialState struct {
	SessionId              string            `json:"sessionId"`
	History                []FrontendMessage `json:"history"` // May or may not include thoughts
	SystemPrompt           string            `json:"systemPrompt"`
	WorkspaceID            string            `json:"workspaceId"`
	PrimaryBranchID        string            `json:"primaryBranchId"`
	Roots                  []string          `json:"roots"`
	CallElapsedTimeSeconds float64           `json:"callElapsedTimeSeconds,omitempty"`
	PendingConfirmation    string            `json:"pendingConfirmation,omitempty"`
	EnvChanged             *env.EnvChanged   `json:"envChanged,omitempty"`
}

func NewSessionAndMessage(
	ctx context.Context, db *sql.DB, models *llm.Models, ga *llm.GeminiAuth, tools *tool.Tools,
	ew EventWriter, sessionId string, userMessage string, systemPrompt string, attachments []FileAttachment,
	workspaceId string, modelToUse string, fetchLimit int, initialRoots []string,
) error {
	var workspaceName string
	if workspaceId != "" {
		workspace, err := database.GetWorkspace(db, workspaceId)
		if err != nil {
			return fmt.Errorf("failed to get workspace %s: %w", workspaceId, err)
		}
		workspaceName = workspace.Name
	}

	// Evaluate system prompt
	data := prompts.NewPromptData(workspaceName)
	systemPrompt, err := data.EvaluatePrompt(systemPrompt)
	if err != nil {
		return fmt.Errorf("failed to evaluate system prompt: %w", err)
	}

	// Determine the model to use
	if modelToUse == "" {
		modelToUse = DefaultGeminiModel // Default model for new sessions
	}

	// Handle InitialRoots if provided
	if len(initialRoots) > 0 {
		// Set initial roots as generation 0 environment
		err := database.SetInitialSessionEnv(db, sessionId, initialRoots)
		if err != nil {
			return fmt.Errorf("failed to set initial session environment: %w", err)
		}

		// Calculate EnvChanged from empty to initial roots
		rootsChanged, err := env.CalculateRootsChanged([]string{}, initialRoots)
		if err != nil {
			log.Printf("newSessionAndMessage: Failed to calculate roots changed for initial roots: %v", err)
			// Non-fatal, continue without adding env change to prompt
		} else {
			envChanged := env.EnvChanged{Roots: &rootsChanged}
			envChangeContext := prompts.GetEnvChangeContext(envChanged)
			systemPrompt = systemPrompt + "\n" + envChangeContext // Append to system prompt
		}
	}

	// Create session with primary_branch_id (moved after InitialRoots handling)
	primaryBranchID, err := database.CreateSession(db, sessionId, systemPrompt, workspaceId)
	if err != nil {
		return fmt.Errorf("failed to create new session: %w", err)
	}

	// Create a new message chain
	mc, err := database.NewMessageChain(ctx, db, sessionId, primaryBranchID)
	if err != nil {
		return fmt.Errorf("failed to create new message chain: %w", err)
	}

	// Add user message to the chain
	mc.LastMessageModel = modelToUse
	mc.LastMessageGeneration = 0 // New session starts with generation 0
	userMsg, err := mc.Add(ctx, db, Message{Text: userMessage, Type: TypeUserText, Attachments: attachments})
	if err != nil {
		return fmt.Errorf("failed to save user message: %w", err)
	}

	// Update last_updated_at for the new session
	if err := database.UpdateSessionLastUpdated(db, sessionId); err != nil {
		log.Printf("Failed to update last_updated_at for new session %s: %v", sessionId, err)
		// Non-fatal error, continue with response
	}

	// Send acknowledgement for user message ID to frontend
	ew.Send(EventAcknowledge, fmt.Sprintf("%d", userMsg.ID))

	// Add this sseWriter to the active list for broadcasting subsequent events
	ew.Acquire()
	defer ew.Release()

	// Retrieve session history from DB for LLM (full context)
	historyContext, err := database.GetSessionHistoryContext(db, sessionId, primaryBranchID)
	if err != nil {
		return fmt.Errorf("failed to retrieve full session history for LLM: %w", err)
	}

	// Retrieve session history for frontend InitialState (paginated)
	frontendHistory, err := database.GetSessionHistoryPaginated(db, sessionId, primaryBranchID, 0, fetchLimit)
	if err != nil {
		return fmt.Errorf("failed to retrieve paginated session history for frontend: %w", err)
	}

	// Prepare initial state for streaming (similar to loadChatSession)
	currentRoots, _, err := database.GetLatestSessionEnv(db, sessionId) // Generation is guaranteed to be 0
	if err != nil {
		return fmt.Errorf("failed to get latest session environment: %w", err)
	}

	initialState := InitialState{
		SessionId:       sessionId,
		History:         frontendHistory,
		SystemPrompt:    systemPrompt,
		WorkspaceID:     workspaceId,
		PrimaryBranchID: primaryBranchID,
		Roots:           currentRoots,
	}

	// Send initial state as a single SSE event (Event type 0: active call, broadcasting will start)
	initialStateJSON, err := json.Marshal(initialState)
	if err != nil {
		return fmt.Errorf("failed to marshal initial state: %w", err)
	}
	ew.Send(EventInitialState, string(initialStateJSON))

	// Handle streaming response from LLM
	// Pass full history to streamLLMResponse for LLM
	if err := streamLLMResponse(db, models, ga, tools, initialState, ew, mc, true, time.Now(), historyContext); err != nil {
		return fmt.Errorf("error streaming LLM response: %w", err)
	}
	return nil
}

func NewChatMessage(
	ctx context.Context, db *sql.DB, models *llm.Models, ga *llm.GeminiAuth, tools *tool.Tools,
	ew EventWriter, sessionId string, userMessage string, attachments []FileAttachment, modelToUse string, fetchLimit int,
) error {
	session, err := database.GetSession(db, sessionId)
	if err != nil {
		log.Printf("chatMessage: Failed to load session %s: %v", sessionId, err)
		if errors.Is(err, sql.ErrNoRows) ||
			err.Error() == "sql: no rows in result set" ||
			strings.Contains(err.Error(), "no such table") {
			return notFoundError("session not found")
		} else {
			return fmt.Errorf("failed to load session: %w", err)
		}
	}
	systemPrompt := session.SystemPrompt
	primaryBranchID := session.PrimaryBranchID // Get primary branch ID from session

	var envChangedEventPayload string

	// Create a new message chain
	mc, err := database.NewMessageChain(ctx, db, sessionId, primaryBranchID)
	if err != nil {
		return fmt.Errorf("failed to create message chain: %w", err)
	}

	// Override the model to use the one specified in the request
	if modelToUse != "" {
		mc.LastMessageModel = modelToUse
	} else if mc.LastMessageModel == "" {
		mc.LastMessageModel = DefaultGeminiModel
	}

	_, currentGeneration, err := database.GetLatestSessionEnv(db, sessionId)
	if err != nil {
		return fmt.Errorf("failed to get latest session environment for session %s: %w", sessionId, err)
	}

	// Check for environment changes and add system message to chain if needed
	if currentGeneration > mc.LastMessageGeneration {
		// Get old roots from the previous generation
		oldRoots, err := database.GetSessionEnv(db, sessionId, mc.LastMessageGeneration)
		if err != nil {
			log.Printf("chatMessage: Failed to get old session environment for session %s, generation %d: %v", sessionId, mc.LastMessageGeneration, err)
			// Non-fatal, continue with user message
		}

		// Get new roots from the current generation
		newRoots, err := database.GetSessionEnv(db, sessionId, currentGeneration)
		if err != nil {
			log.Printf("chatMessage: Failed to get new session environment for session %s, generation %d: %v", sessionId, currentGeneration, err)
			// Non-fatal, continue with user message
		}

		rootsChanged, err := env.CalculateRootsChanged(oldRoots, newRoots)
		if err != nil {
			log.Printf("chatMessage: Failed to calculate roots changed: %v", err)
			// Non-fatal, continue with user message
		}

		envChanged := env.EnvChanged{Roots: &rootsChanged}

		// Marshal envChanged into JSON
		envChangedJSON, err := json.Marshal(envChanged) // Use = instead of :=
		if err != nil {
			log.Printf("chatMessage: Failed to marshal envChanged for system message: %v", err)
			// Non-fatal, continue with user message
		}

		systemMsg, err := mc.Add(ctx, db, Message{
			Text:            string(envChangedJSON),
			Type:            TypeEnvChanged,
			Attachments:     nil,
			CumulTokenCount: nil,
		})
		if err != nil {
			log.Printf("chatMessage: Failed to add envChanged message to chain: %v", err)
			// Non-fatal, continue with user message
		}

		envChangedEventPayload = fmt.Sprintf("%d\n%s", systemMsg.ID, string(envChangedJSON))
	}

	// Add user message to the chain
	userMsg, err := mc.Add(ctx, db, Message{
		Text:            userMessage,
		Type:            TypeUserText,
		Attachments:     attachments,
		CumulTokenCount: nil,
	})
	if err != nil {
		return fmt.Errorf("failed to save user message: %w", err)
	}

	// Update last_updated_at for the session
	if err := database.UpdateSessionLastUpdated(db, sessionId); err != nil {
		log.Printf("Failed to update last_updated_at for session %s: %v", sessionId, err)
		// Non-fatal error, continue with response
	}

	// Add this sseWriter to the active list for broadcasting subsequent events
	ew.Acquire()
	defer ew.Release()

	if envChangedEventPayload != "" {
		ew.Send(EventGenerationChanged, envChangedEventPayload)
	}

	// Send acknowledgement for user message ID to frontend
	ew.Send(EventAcknowledge, fmt.Sprintf("%d", userMsg.ID))

	// Retrieve session history from DB for LLM (full context)
	fullFrontendHistoryForLLM, err := database.GetSessionHistoryContext(db, sessionId, primaryBranchID)
	if err != nil {
		return fmt.Errorf("failed to retrieve full session history for LLM: %w", err)
	}

	// Retrieve session history for frontend InitialState (paginated)
	frontendHistoryForInitialState, err := database.GetSessionHistoryPaginated(db, sessionId, primaryBranchID, 0, fetchLimit)
	if err != nil {
		return fmt.Errorf("failed to retrieve paginated session history for frontend: %w", err)
	}

	roots, _, err := database.GetLatestSessionEnv(db, sessionId)
	if err != nil {
		return fmt.Errorf("failed to get latest session environment for session %s: %w", sessionId, err)
	}

	// Prepare initial state for streaming (similar to loadChatSession)
	initialState := InitialState{
		SessionId:       sessionId,
		History:         frontendHistoryForInitialState, // Use paginated history for frontend
		SystemPrompt:    systemPrompt,
		WorkspaceID:     session.WorkspaceID,
		PrimaryBranchID: primaryBranchID,
		Roots:           roots,
	}

	// Send initial state as a single SSE event (Event type 0: active call, broadcasting will start)
	initialStateJSON, err := json.Marshal(initialState)
	if err != nil {
		return fmt.Errorf("failed to marshal initial state: %w", err)
	}
	ew.Send(EventInitialState, string(initialStateJSON))

	if err := streamLLMResponse(db, models, ga, tools, initialState, ew, mc, false, time.Now(), fullFrontendHistoryForLLM); err != nil {
		return fmt.Errorf("failed to stream LLM response: %w", err)
	}
	return nil
}

func LoadChatSession(ctx context.Context, db *sql.DB, ew EventWriter, sessionId string, beforeMessageID int, fetchLimit int) (initialState InitialState, err error) {
	// Check if session exists
	exists, err := database.SessionExists(db, sessionId)
	if err != nil {
		err = fmt.Errorf("failed to check session existence: %w", err)
		return
	}
	if !exists {
		err = notFoundError("session not found")
		return
	}

	session, err := database.GetSession(db, sessionId)
	if err != nil {
		err = fmt.Errorf("failed to load session: %w", err)
		return
	}

	// Check if it's an SSE request and initialize ew early
	if ew != nil {
		// Send WorkspaceID hint to frontend as early as possible
		ew.Send(EventWorkspaceHint, session.WorkspaceID)

		ew.Acquire()
		defer ew.Release()
	}

	// Use automatic branch detection to load history with pagination
	history, actualBranchID, err := database.GetSessionHistoryPaginatedWithAutoBranch(db, sessionId, beforeMessageID, fetchLimit)
	if err != nil {
		err = fmt.Errorf("failed to load session history: %w", err)
		return
	}

	// Ensure history is an empty slice if no messages are found, not nil
	if history == nil {
		history = []FrontendMessage{}
	}

	currentRoots, currentGeneration, err := database.GetLatestSessionEnv(db, sessionId)
	if err != nil {
		err = fmt.Errorf("failed to get latest session environment for session %s: %w", sessionId, err)
		return
	}

	// Get the generation of the last message in the history being loaded
	// If history is empty, assume generation 0.
	lastMessageGenerationInHistory := 0
	if len(history) > 0 {
		// Get the actual Message from DB using the ID of the last FrontendMessage
		lastFrontendMessageID, err := strconv.Atoi(history[len(history)-1].ID)
		if err != nil {
			log.Printf("loadChatSession: Failed to parse last message ID: %v", err)
			// Non-fatal, continue with generation 0
		} else {
			lastMessage, err := database.GetMessageByID(db, lastFrontendMessageID)
			if err != nil {
				log.Printf("loadChatSession: Failed to get last message by ID %d: %v", lastFrontendMessageID, err)
				// Non-fatal, continue with generation 0
			} else {
				lastMessageGenerationInHistory = lastMessage.Generation
			}
		}
	}

	var initialStateEnvChanged *env.EnvChanged
	if currentGeneration > lastMessageGenerationInHistory {
		oldRoots, err := database.GetSessionEnv(db, sessionId, lastMessageGenerationInHistory)
		if err != nil {
			log.Printf("loadChatSession: Failed to get old session environment for generation %d: %v", lastMessageGenerationInHistory, err)
			// Non-fatal, continue
		}
		rootsChanged, err := env.CalculateRootsChanged(oldRoots, currentRoots)
		if err != nil {
			log.Printf("loadChatSession: Failed to calculate roots changed for initial state: %v", err)
			// Non-fatal, continue
		}
		initialStateEnvChanged = &env.EnvChanged{Roots: &rootsChanged}
	}

	// Prepare initial state as a single JSON object
	initialState = InitialState{
		SessionId:       sessionId,
		History:         history,
		SystemPrompt:    session.SystemPrompt,
		WorkspaceID:     session.WorkspaceID,
		PrimaryBranchID: actualBranchID,
		Roots:           currentRoots,
		EnvChanged:      initialStateEnvChanged,
	}

	branch, err := database.GetBranch(db, actualBranchID)
	if err != nil {
		err = fmt.Errorf("failed to get branch %s: %w", actualBranchID, err)
		return
	}

	if branch.PendingConfirmation != nil {
		initialState.PendingConfirmation = *branch.PendingConfirmation
	}

	// If it's an SSE request, handle streaming. Otherwise, send regular JSON response.
	if ew != nil {
		if HasActiveCall(sessionId) {
			callStartTime, ok := GetCallStartTime(sessionId)
			if ok {
				initialState.CallElapsedTimeSeconds = time.Since(callStartTime).Seconds()
			}
			initialStateJSON, err2 := json.Marshal(initialState)
			if err2 != nil {
				err = fmt.Errorf("failed to marshal initial state with elapsed time: %w", err2)
				return
			}
			ew.Send(EventInitialState, string(initialStateJSON))

			// Keep the connection open until client disconnects.
			// sseW will get any broadcasted messages over the course.
			<-ctx.Done()
		} else {
			initialStateJSON, err2 := json.Marshal(initialState)
			if err2 != nil {
				err = fmt.Errorf("failed to marshal initial state: %w", err2)
				return
			}

			// If no active call, close the SSE connection after sending initial state
			ew.Send(EventInitialStateNoCall, string(initialStateJSON))
			time.Sleep(10 * time.Millisecond) // Give some time for the event to be processed
			ew.Close()
		}
	}

	return
}
