package chat

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// CallStatus enum defines the status of an API call.
type CallStatus string

const (
	CallStatusRunning   CallStatus = "running"
	CallStatusCancelled CallStatus = "cancelled"
	CallStatusCompleted CallStatus = "completed"
	CallStatusError     CallStatus = "error"
)

// Call struct represents an ongoing API call.
type Call struct {
	SessionID  string             `json:"sessionId"`
	CancelFunc context.CancelFunc `json:"-"` // context.CancelFunc cannot be marshaled to JSON
	Status     CallStatus         `json:"status"`
	StartTime  time.Time          `json:"startTime"`
	EndTime    *time.Time         `json:"endTime,omitempty"`
	Error      string             `json:"error,omitempty"`
}

var (
	activeCalls = make(map[string]*Call)
	callsMutex  sync.Mutex
)

// startCall registers a new API call.
func startCall(sessionId string, cancelFunc context.CancelFunc) error {
	callsMutex.Lock()
	defer callsMutex.Unlock()

	if _, ok := activeCalls[sessionId]; ok {
		return fmt.Errorf("a call for session ID %s is already active", sessionId)
	}

	activeCalls[sessionId] = &Call{
		SessionID:  sessionId,
		CancelFunc: cancelFunc,
		Status:     CallStatusRunning,
		StartTime:  time.Now(),
	}
	return nil
}

// CancelCall cancels an active API call and all its subagent calls.
func CancelCall(sessionId string) error {
	callsMutex.Lock()
	defer callsMutex.Unlock()

	var cancelledCalls []string

	// First, cancel the main session if it exists
	if call, ok := activeCalls[sessionId]; ok {
		if call.Status == CallStatusRunning {
			call.CancelFunc()
			call.Status = CallStatusCancelled
			now := time.Now()
			call.EndTime = &now
			cancelledCalls = append(cancelledCalls, sessionId)
		}
	}

	// Then, cancel all subagent calls (sessions that start with sessionId + ".")
	subsessionPrefix := sessionId + "."
	for subsessionId, call := range activeCalls {
		if call.Status == CallStatusRunning && strings.HasPrefix(subsessionId, subsessionPrefix) {
			call.CancelFunc()
			call.Status = CallStatusCancelled
			now := time.Now()
			call.EndTime = &now
			cancelledCalls = append(cancelledCalls, subsessionId)
		}
	}

	if len(cancelledCalls) == 0 {
		return fmt.Errorf("no active calls found for session ID: %s or its subagents", sessionId)
	}

	return nil
}

// completeCall marks an API call as completed.
func completeCall(sessionId string) {
	callsMutex.Lock()
	defer callsMutex.Unlock()

	if call, ok := activeCalls[sessionId]; ok {
		if call.Status == CallStatusRunning {
			call.Status = CallStatusCompleted
			now := time.Now()
			call.EndTime = &now
		}
	} else {
		// This might happen if the call was cancelled and removed before completion
		// Or if it was never registered (shouldn't happen if startCall is always used)
	}
}

// failCall marks an API call as failed with an error message.
func failCall(sessionId string, err error) {
	callsMutex.Lock()
	defer callsMutex.Unlock()

	if call, ok := activeCalls[sessionId]; ok {
		if call.Status == CallStatusRunning {
			call.Status = CallStatusError
			call.Error = err.Error()
			now := time.Now()
			call.EndTime = &now
		}
	} else {
		// Similar to completeGeminiCall, handle cases where call might not be in map
	}
}

// HasActiveCall checks if there is an active call for the given session ID or any of its subagents.
func HasActiveCall(sessionId string) bool {
	callsMutex.Lock()
	defer callsMutex.Unlock()

	// Check main session
	if _, ok := activeCalls[sessionId]; ok {
		return true
	}

	// Check all subagent sessions
	subsessionPrefix := sessionId + "."
	for subsessionId := range activeCalls {
		if strings.HasPrefix(subsessionId, subsessionPrefix) {
			return true
		}
	}

	return false
}

// removeCall removes a call from the active list (e.g., after completion or final error handling).
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
