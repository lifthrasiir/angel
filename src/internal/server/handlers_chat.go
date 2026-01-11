package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"

	. "github.com/lifthrasiir/angel/gemini"
	"github.com/lifthrasiir/angel/internal/chat"
	"github.com/lifthrasiir/angel/internal/database"
	"github.com/lifthrasiir/angel/internal/env"
	. "github.com/lifthrasiir/angel/internal/types"
)

// New session and message handler
func newSessionAndMessageHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)
	models := getModels(w, r)
	ga := getGeminiAuth(w, r)
	tools := getTools(w, r)

	var requestBody struct {
		Message      string           `json:"message"`
		SystemPrompt string           `json:"systemPrompt"`
		Attachments  []FileAttachment `json:"attachments"`
		WorkspaceID  string           `json:"workspaceId"`
		Model        string           `json:"model"`
		FetchLimit   int              `json:"fetchLimit"`
		InitialRoots []string         `json:"initialRoots"`
	}

	if !decodeJSONRequest(r, w, &requestBody, "newSessionAndMessage") {
		return
	}

	sessionId := database.GenerateID()
	ew := newSseWriter(r.Context(), sessionId, w)
	if ew == nil {
		return
	}

	if err := chat.NewSessionAndMessage(
		r.Context(), db, models, ga, tools,
		ew, requestBody.Message, requestBody.SystemPrompt, requestBody.Attachments,
		sessionId, requestBody.WorkspaceID, requestBody.Model, requestBody.FetchLimit, requestBody.InitialRoots,
	); err != nil {
		if ew.HeadersSent() {
			log.Printf("Failed to create new session and message: %v", err)
		} else {
			sendInternalServerError(w, r, err, "Failed to create new session and message")
		}
	}
}

// New temporary session and message handler
func newTempSessionAndMessageHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)
	models := getModels(w, r)
	ga := getGeminiAuth(w, r)
	tools := getTools(w, r)

	var requestBody struct {
		Message      string           `json:"message"`
		SystemPrompt string           `json:"systemPrompt"`
		Attachments  []FileAttachment `json:"attachments"`
		Model        string           `json:"model"`
		FetchLimit   int              `json:"fetchLimit"`
		InitialRoots []string         `json:"initialRoots"`
		WorkspaceID  string           `json:"workspaceId"`
	}

	if !decodeJSONRequest(r, w, &requestBody, "newTempSessionAndMessage") {
		return
	}

	// Generate temporary session ID (starts with dot)
	sessionId := "." + database.GenerateID()
	ew := newSseWriter(r.Context(), sessionId, w)
	if ew == nil {
		return
	}

	if err := chat.NewSessionAndMessage(
		r.Context(), db, models, ga, tools,
		ew, requestBody.Message, requestBody.SystemPrompt, requestBody.Attachments,
		sessionId, requestBody.WorkspaceID, requestBody.Model, requestBody.FetchLimit, requestBody.InitialRoots,
	); err != nil {
		if ew.HeadersSent() {
			log.Printf("Failed to create new temporary session and message: %v", err)
		} else {
			sendInternalServerError(w, r, err, "Failed to create new temporary session and message")
		}
	}
}

// Chat message handler
func chatMessageHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)
	models := getModels(w, r)
	ga := getGeminiAuth(w, r)
	tools := getTools(w, r)

	vars := mux.Vars(r)
	sessionId := vars["sessionId"]
	if sessionId == "" {
		sendBadRequestError(w, r, "Session ID is required")
		return
	}

	sdb, err := db.WithSession(sessionId)
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to access session database")
		return
	}
	defer sdb.Close()

	var requestBody struct {
		Message     string           `json:"message"`
		Attachments []FileAttachment `json:"attachments"`
		Model       string           `json:"model"`
		FetchLimit  int              `json:"fetchLimit"`
	}

	if !decodeJSONRequest(r, w, &requestBody, "chatMessage") {
		return
	}

	ew := newSseWriter(r.Context(), sessionId, w)
	if ew == nil {
		return
	}

	if err := chat.NewChatMessage(
		r.Context(), sdb, models, ga, tools,
		ew, requestBody.Message, requestBody.Attachments, requestBody.Model, requestBody.FetchLimit,
	); err != nil {
		if ew.HeadersSent() {
			log.Printf("Failed to process chat message: %v", err)
		} else {
			sendInternalServerError(w, r, err, "Failed to process chat message")
		}
	}
}

// New endpoint to load chat session history
func loadChatSessionHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)

	vars := mux.Vars(r)
	sessionId := vars["sessionId"]
	if sessionId == "" {
		sendBadRequestError(w, r, "Session ID is required")
		return
	}

	// Parse pagination parameters
	beforeMessageIDStr := r.URL.Query().Get("beforeMessageId")
	fetchLimitStr := r.URL.Query().Get("fetchLimit")

	beforeMessageID := 0 // Default to 0, meaning fetch from the latest
	if beforeMessageIDStr != "" {
		parsedID, err := strconv.Atoi(beforeMessageIDStr)
		if err != nil {
			sendBadRequestError(w, r, "Invalid beforeMessageId parameter")
			return
		}
		beforeMessageID = parsedID
	}

	fetchLimit := math.MaxInt // Default fetch limit
	if fetchLimitStr != "" {
		parsedLimit, err := strconv.Atoi(fetchLimitStr)
		if err != nil {
			sendBadRequestError(w, r, "Invalid fetchLimit parameter")
			return
		}
		fetchLimit = parsedLimit
	}

	// Optionally initialize EventWriter
	var ew EventWriter
	if r.Header.Get("Accept") == "text/event-stream" {
		sseW := newSseWriter(r.Context(), sessionId, w)
		if sseW == nil {
			return
		}
		ew = sseW // Avoid nil interface
	}

	sdb, err := db.WithSession(sessionId)
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to access session database")
		return
	}
	defer sdb.Close()

	initialState, err := chat.LoadChatSession(r.Context(), sdb, ew, beforeMessageID, fetchLimit)
	if err != nil {
		if ew != nil && ew.HeadersSent() {
			log.Printf("Failed to load chat session: %v", err)
		} else {
			sendInternalServerError(w, r, err, "Failed to load chat session")
		}
		return
	}

	if ew == nil {
		// Original JSON response for non-SSE requests
		sendJSONResponse(w, initialState)
	}
}

func listSessionsByWorkspaceHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)

	workspaceID := r.URL.Query().Get("workspaceId")

	wsWithSessions, err := database.GetWorkspaceAndSessions(db, workspaceID)
	if err != nil {
		sendInternalServerError(w, r, err, fmt.Sprintf("Failed to retrieve sessions for workspace %s", workspaceID))
		return
	}

	sendJSONResponse(w, wsWithSessions)
}

// calculateNewSessionEnvChangedHandler calculates EnvChanged for a new session.
// It expects newRoots as a JSON string in the query parameter.
func calculateNewSessionEnvChangedHandler(w http.ResponseWriter, r *http.Request) {
	// No authentication needed for this endpoint as it's for pre-session calculation
	// and doesn't modify any session state.

	newRootsJSON := r.URL.Query().Get("newRoots")
	if newRootsJSON == "" {
		sendBadRequestError(w, r, "newRoots query parameter is required")
		return
	}

	var newRoots []string
	if err := json.Unmarshal([]byte(newRootsJSON), &newRoots); err != nil {
		sendBadRequestError(w, r, "Invalid newRoots JSON")
		return
	}

	// oldRoots is always empty for a new session's initial environment calculation
	oldRoots := []string{}

	rootsChanged, err := env.CalculateRootsChanged(oldRoots, newRoots)
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to calculate environment changes")
		return
	}

	envChanged := env.EnvChanged{Roots: &rootsChanged}
	sendJSONResponse(w, envChanged)
}

// New endpoint to delete a chat session
func deleteSessionHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)
	config := getEnvConfig(w, r)

	vars := mux.Vars(r)
	sessionId := vars["sessionId"]
	if sessionId == "" {
		sendBadRequestError(w, r, "Session ID is required")
		return
	}

	sandboxBaseDir := config.SessionDir()
	if err := database.DeleteSession(db, sessionId, sandboxBaseDir); err != nil {
		sendInternalServerError(w, r, err, fmt.Sprintf("Failed to delete session %s", sessionId))
		return
	}

	sendJSONResponse(w, map[string]string{"status": "success", "message": "Session deleted successfully"})
}

// createBranchHandler creates a new branch from a given parent message.
func createBranchHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)
	models := getModels(w, r)
	ga := getGeminiAuth(w, r)
	tools := getTools(w, r)

	vars := mux.Vars(r)
	sessionId := vars["sessionId"]
	if sessionId == "" {
		sendBadRequestError(w, r, "Session ID is required")
		return
	}

	var requestBody struct {
		UpdatedMessageID int    `json:"updatedMessageId"`
		NewMessageText   string `json:"newMessageText"`
	}

	if !decodeJSONRequest(r, w, &requestBody, "createBranchHandler") {
		return
	}

	// Check if this is a retry request
	isRetry := r.URL.Query().Get("retry") == "1"

	ew := newSseWriter(r.Context(), sessionId, w)
	if ew == nil {
		return
	}

	sdb, err := db.WithSession(sessionId)
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to access session database")
		return
	}
	defer sdb.Close()

	if isRetry && requestBody.NewMessageText == "" {
		err = chat.RetryBranch(r.Context(), sdb, models, ga, tools, ew, requestBody.UpdatedMessageID)
	} else {
		err = chat.CreateBranch(r.Context(), sdb, models, ga, tools, ew, requestBody.UpdatedMessageID, requestBody.NewMessageText)
	}
	if err != nil {
		if ew.HeadersSent() {
			log.Printf("Failed to create branch: %v", err)
		} else {
			sendInternalServerError(w, r, err, "Failed to create branch")
		}
	}
}

// updateMessageHandler updates the content of a specific message.
// Only user and model messages can be updated; other message types return an error.
func updateMessageHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)

	vars := mux.Vars(r)
	sessionId := vars["sessionId"]
	messageIdStr := vars["messageId"]
	if sessionId == "" || messageIdStr == "" {
		sendBadRequestError(w, r, "Session ID and Message ID are required")
		return
	}

	messageId, err := strconv.Atoi(messageIdStr)
	if err != nil {
		sendBadRequestError(w, r, "Invalid message ID")
		return
	}

	var requestBody struct {
		Text string `json:"text"`
	}

	if !decodeJSONRequest(r, w, &requestBody, "updateMessageHandler") {
		return
	}

	if requestBody.Text == "" {
		sendBadRequestError(w, r, "Message text is required")
		return
	}

	sdb, err := db.WithSession(sessionId)
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to access session database")
		return
	}
	defer sdb.Close()

	// Get message type to validate
	msgType, _, _, err := database.GetMessageDetails(sdb, messageId)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			sendNotFoundError(w, r, "Message not found")
			return
		}
		sendInternalServerError(w, r, err, "Failed to get message details")
		return
	}

	// Only allow updating user and model messages
	if msgType != TypeUserText && msgType != TypeModelText {
		sendBadRequestError(w, r, "Only user and model messages can be updated")
		return
	}

	// Update message content with aux
	if err := database.UpdateMessageContentWithAux(sdb, messageId, requestBody.Text); err != nil {
		sendInternalServerError(w, r, err, "Failed to update message")
		return
	}

	sendJSONResponse(w, map[string]interface{}{
		"status":    "success",
		"messageId": messageId,
		"text":      requestBody.Text,
	})
}

// switchBranchHandler switches the primary branch of a session.
func switchBranchHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)

	vars := mux.Vars(r)
	sessionId := vars["sessionId"]
	if sessionId == "" {
		sendBadRequestError(w, r, "Session ID is required")
		return
	}

	var requestBody struct {
		NewPrimaryBranchID string `json:"newPrimaryBranchId"`
	}

	if !decodeJSONRequest(r, w, &requestBody, "switchBranchHandler") {
		return
	}

	sdb, err := db.WithSession(sessionId)
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to access session database")
		return
	}
	defer sdb.Close()

	if err := chat.SwitchBranch(sdb, requestBody.NewPrimaryBranchID); err != nil {
		sendInternalServerError(w, r, err, "Failed to switch primary branch")
		return
	}

	sendJSONResponse(w, map[string]string{
		"status":          "success",
		"primaryBranchId": requestBody.NewPrimaryBranchID,
	})
}

// confirmBranchHandler handles the confirmation of a pending action on a branch.
func confirmBranchHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)
	models := getModels(w, r)
	ga := getGeminiAuth(w, r)
	tools := getTools(w, r)

	vars := mux.Vars(r)
	sessionId := vars["sessionId"]
	branchId := vars["branchId"]
	if sessionId == "" || branchId == "" {
		sendBadRequestError(w, r, "Session ID and Branch ID are required")
		return
	}

	var requestBody struct {
		Approved     bool                   `json:"approved"`
		ModifiedData map[string]interface{} `json:"modifiedData"` // Optional: tool arguments if modified
	}

	if !decodeJSONRequest(r, w, &requestBody, "confirmBranchHandler") {
		return
	}

	ew := newSseWriter(r.Context(), sessionId, w)
	if ew == nil {
		return
	}

	sdb, err := db.WithSession(sessionId)
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to access session database")
		return
	}
	defer sdb.Close()

	if err := chat.ConfirmBranch(
		r.Context(), sdb, models, ga, tools,
		ew, branchId, requestBody.Approved, requestBody.ModifiedData,
	); err != nil {
		if ew.HeadersSent() {
			log.Printf("Failed to confirm branch: %v", err)
		} else {
			sendInternalServerError(w, r, err, "Failed to confirm branch")
		}
		return
	}
}

// retryErrorBranchHandler handles retry-error requests for a branch by removing error messages and resuming streaming.
func retryErrorBranchHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)
	models := getModels(w, r)
	ga := getGeminiAuth(w, r)
	tools := getTools(w, r)

	vars := mux.Vars(r)
	sessionId := vars["sessionId"]
	branchId := vars["branchId"]

	if sessionId == "" || branchId == "" {
		sendBadRequestError(w, r, "Session ID and Branch ID are required")
		return
	}

	ew := newSseWriter(r.Context(), sessionId, w)
	if ew == nil {
		return
	}

	sdb, err := db.WithSession(sessionId)
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to access session database")
		return
	}
	defer sdb.Close()

	if err := chat.RetryErrorBranch(r.Context(), sdb, models, ga, tools, ew, branchId); err != nil {
		if ew.HeadersSent() {
			log.Printf("Failed to retry error branch: %v", err)
		} else {
			sendInternalServerError(w, r, err, "Failed to retry error branch")
		}
		return
	}
}

// commandHandler handles POST requests for /api/chat/{sessionId}/command
func commandHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)

	vars := mux.Vars(r)
	sessionID := vars["sessionId"]
	if sessionID == "" {
		sendBadRequestError(w, r, "Session ID is required")
		return
	}

	var requestBody struct {
		Command string `json:"command"`
	}

	if !decodeJSONRequest(r, w, &requestBody, "commandHandler") {
		return
	}

	if requestBody.Command == "" {
		sendBadRequestError(w, r, "Command is required")
		return
	}

	sdb, err := db.WithSession(sessionID)
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to access session database")
		return
	}
	defer sdb.Close()

	// Execute the command
	var commandMessageID int
	switch requestBody.Command {
	case "clear", "clearblobs":
		commandMessageID, err = chat.ExecuteClearCommand(r.Context(), sdb, requestBody.Command)
	case "new-user-message", "new-model-message":
		commandMessageID, err = chat.ExecuteNewMessageCommand(r.Context(), sdb, requestBody.Command)
	default:
		sendBadRequestError(w, r, fmt.Sprintf("Unknown command: %s", requestBody.Command))
		return
	}

	if err != nil {
		sendInternalServerError(w, r, err, fmt.Sprintf("Failed to execute command: %s", requestBody.Command))
		return
	}

	sendJSONResponse(w, map[string]interface{}{
		"status":           "success",
		"message":          fmt.Sprintf("Command %s executed successfully", requestBody.Command),
		"commandMessageId": commandMessageID,
	})
}

func compressSessionHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)
	models := getModels(w, r)

	vars := mux.Vars(r)
	sessionID := vars["sessionId"]
	if sessionID == "" {
		sendBadRequestError(w, r, "Session ID is required")
		return
	}

	sdb, err := db.WithSession(sessionID)
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to access session database")
		return
	}
	defer sdb.Close()

	result, err := chat.CompressSession(r.Context(), sdb, models, DefaultGeminiModel)
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to compress session")
		return
	}

	sendJSONResponse(w, map[string]interface{}{
		"status":                  "success",
		"message":                 "Chat history compressed successfully",
		"originalTokenCount":      result.OriginalTokenCount,
		"newTokenCount":           result.NewTokenCount,
		"compressionMessageId":    result.CompressionMsgID,
		"compressedUpToMessageId": result.CompressedUpToMessageID,
		"extractedSummary":        result.ExtractedSummary,
	})
}

// extractSessionHandler extracts messages from a specific branch up to a given message and creates a new session.
func extractSessionHandler(w http.ResponseWriter, r *http.Request) {
	db := getDb(w, r)

	vars := mux.Vars(r)
	sessionId := vars["sessionId"]
	if sessionId == "" {
		sendBadRequestError(w, r, "Session ID is required")
		return
	}

	var requestBody struct {
		MessageID string `json:"messageId"`
	}

	if !decodeJSONRequest(r, w, &requestBody, "extractSessionHandler") {
		return
	}

	if requestBody.MessageID == "" {
		sendBadRequestError(w, r, "Message ID is required")
		return
	}

	// Parse message ID
	targetMessageID, err := strconv.Atoi(requestBody.MessageID)
	if err != nil {
		sendBadRequestError(w, r, "Invalid message ID")
		return
	}

	sdb, err := db.WithSession(sessionId)
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to access session database")
		return
	}
	defer sdb.Close()

	newSessionId, newSessionName, err := chat.ExtractSession(r.Context(), sdb, targetMessageID)
	if err != nil {
		sendInternalServerError(w, r, err, "Failed to extract session")
		return
	}

	// Return the new session link
	response := map[string]string{
		"status":      "success",
		"sessionId":   newSessionId,
		"sessionName": newSessionName,
		"link":        fmt.Sprintf("/%s", newSessionId),
		"message":     "Session extracted successfully",
	}

	sendJSONResponse(w, response)
}
