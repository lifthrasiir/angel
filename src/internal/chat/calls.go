package chat

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Call struct represents an ongoing API call.
type Call struct {
	SessionID  string             `json:"sessionId"`
	CancelFunc context.CancelFunc `json:"-"` // context.CancelFunc cannot be marshaled to JSON
	StartTime  time.Time          `json:"startTime"`
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
		call.CancelFunc()
		cancelledCalls = append(cancelledCalls, sessionId)
	}

	// Then, cancel all subagent calls (sessions that start with sessionId + ".")
	subsessionPrefix := sessionId + "."
	for subsessionId, call := range activeCalls {
		if strings.HasPrefix(subsessionId, subsessionPrefix) {
			call.CancelFunc()
			cancelledCalls = append(cancelledCalls, subsessionId)
		}
	}

	if len(cancelledCalls) == 0 {
		return fmt.Errorf("no active calls found for session ID: %s or its subagents", sessionId)
	}

	return nil
}

// completeCall marks an API call as completed and removes it from the active list.
func completeCall(sessionId string) {
	callsMutex.Lock()
	defer callsMutex.Unlock()

	delete(activeCalls, sessionId)

	subsessionPrefix := sessionId + "."
	for subsessionId := range activeCalls {
		if strings.HasPrefix(subsessionId, subsessionPrefix) {
			delete(activeCalls, subsessionId)
		}
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
