package main

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// GeminiCallStatus enum defines the status of a Gemini API call.
type GeminiCallStatus string

const (
	GeminiCallStatusRunning   GeminiCallStatus = "running"
	GeminiCallStatusCancelled GeminiCallStatus = "cancelled"
	GeminiCallStatusCompleted GeminiCallStatus = "completed"
	GeminiCallStatusError     GeminiCallStatus = "error"
)

// GeminiCall struct represents an ongoing Gemini API call.
type GeminiCall struct {
	SessionID  string             `json:"sessionId"`
	CancelFunc context.CancelFunc `json:"-"` // context.CancelFunc cannot be marshaled to JSON
	Status     GeminiCallStatus   `json:"status"`
	StartTime  time.Time          `json:"startTime"`
	EndTime    *time.Time         `json:"endTime,omitempty"`
	Error      string             `json:"error,omitempty"`
}

var (
	activeCalls = make(map[string]*GeminiCall)
	callsMutex  sync.Mutex
)

// startGeminiCall registers a new Gemini API call.
func startCall(sessionId string, cancelFunc context.CancelFunc) error {
	callsMutex.Lock()
	defer callsMutex.Unlock()

	if _, ok := activeCalls[sessionId]; ok {
		return fmt.Errorf("a call for session ID %s is already active", sessionId)
	}

	activeCalls[sessionId] = &GeminiCall{
		SessionID:  sessionId,
		CancelFunc: cancelFunc,
		Status:     GeminiCallStatusRunning,
		StartTime:  time.Now(),
	}
	return nil
}

// cancelGeminiCall cancels an active Gemini API call and updates its status.
func cancelCall(sessionId string) error {
	callsMutex.Lock()
	defer callsMutex.Unlock()

	call, ok := activeCalls[sessionId]
	if !ok {
		return fmt.Errorf("no active call found for session ID: %s", sessionId)
	}

	if call.Status == GeminiCallStatusRunning {
		call.CancelFunc()
		call.Status = GeminiCallStatusCancelled
		now := time.Now()
		call.EndTime = &now
		return nil
	}
	return fmt.Errorf("call for session ID %s is not running (current status: %s)", sessionId, call.Status)
}

// completeGeminiCall marks a Gemini API call as completed.
func completeCall(sessionId string) {
	callsMutex.Lock()
	defer callsMutex.Unlock()

	if call, ok := activeCalls[sessionId]; ok {
		if call.Status == GeminiCallStatusRunning {
			call.Status = GeminiCallStatusCompleted
			now := time.Now()
			call.EndTime = &now
		}
	} else {
		// This might happen if the call was cancelled and removed before completion
		// Or if it was never registered (shouldn't happen if startGeminiCall is always used)
	}
}

// failGeminiCall marks a Gemini API call as failed with an error message.
func failCall(sessionId string, err error) {
	callsMutex.Lock()
	defer callsMutex.Unlock()

	if call, ok := activeCalls[sessionId]; ok {
		if call.Status == GeminiCallStatusRunning {
			call.Status = GeminiCallStatusError
			call.Error = err.Error()
			now := time.Now()
			call.EndTime = &now
		}
	} else {
		// Similar to completeGeminiCall, handle cases where call might not be in map
	}
}

// hasActiveCall checks if there is an active call for the given session ID.
func hasActiveCall(sessionId string) bool {
	callsMutex.Lock()
	defer callsMutex.Unlock()
	_, ok := activeCalls[sessionId]
	return ok
}

// removeGeminiCall removes a call from the active list (e.g., after completion or final error handling).
func removeCall(sessionId string) {
	callsMutex.Lock()
	defer callsMutex.Unlock()

	delete(activeCalls, sessionId)
}

// GetCallStartTime returns the start time of an active call for the given session ID.
func GetCallStartTime(sessionId string) (time.Time, bool) {
	callsMutex.Lock()
	defer callsMutex.Unlock()

	call, ok := activeCalls[sessionId]
	if !ok {
		return time.Time{}, false
	}
	return call.StartTime, true
}
