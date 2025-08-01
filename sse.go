package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
)

const (
	// SSE Event Types
	EventSessionID          = "S" // Initial session ID
	EventInitialState       = "0" // Initial state with history (for active call)
	EventInitialStateNoCall = "1" // Initial state with history (for load session when no active call)
	EventFunctionCall       = "F" // Function call
	EventThought            = "T" // Thought process
	EventModelMessage       = "M" // Model message (text)
	EventFunctionReply      = "R" // Function response
	EventComplete           = "Q" // Query complete
	EventSessionName        = "N" // Session name inferred/updated
	EventError              = "E" // Error message
)

// sseWriter wraps http.ResponseWriter and http.Flusher to handle client disconnections gracefully.
//
// Connection cleanup sequence analysis from Go stdlib net/http/server.go:
// 1. defer cancelCtx() executes first -> context.Done() signal sent
// 2. c.finalFlush() called -> c.bufw.Flush() executed
// 3. putBufioWriter(c.bufw) called -> bufio.Writer.Reset(nil) -> panic if Flush() called after this
//
// Solution: Monitor request context to detect disconnection before Reset(nil) happens
type sseWriter struct {
	http.ResponseWriter
	http.Flusher
	mu           sync.Mutex
	disconnected bool
	ctx          context.Context
	sessionId    string // Add sessionId to sseWriter
}

// Close marks the sseWriter as disconnected and removes it from the active writers.
func (s *sseWriter) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.disconnected {
		s.disconnected = true
		removeSseWriter(s.sessionId, s)
	}
}

// prepareSSEEventData prepares the SSE event data string.
// Note: `eventType` is for the logical message kind, not the browser event type!
func prepareSSEEventData(eventType, data string) []byte {
	escapedData := strings.ReplaceAll(data, "\n", "\ndata: ")
	return []byte(fmt.Sprintf("data: %s\ndata: %s\n\n", eventType, escapedData))
}

var (
	activeSseWriters = make(map[string][]*sseWriter) // sessionId -> list of sseWriters
	sseWritersMutex  sync.Mutex
)

// addSseWriter adds an sseWriter to the activeSseWriters map.
func addSseWriter(sessionId string, sseW *sseWriter) {
	sseWritersMutex.Lock()
	defer sseWritersMutex.Unlock()
	activeSseWriters[sessionId] = append(activeSseWriters[sessionId], sseW)
}

// removeSseWriter removes an sseWriter from the activeSseWriters map.
func removeSseWriter(sessionId string, sseW *sseWriter) {
	sseWritersMutex.Lock()
	defer sseWritersMutex.Unlock()
	writers := activeSseWriters[sessionId]
	for i, w := range writers {
		if w == sseW {
			activeSseWriters[sessionId] = append(writers[:i], writers[i+1:]...)
			return
		}
	}
}

// broadcastToSession sends an event to all active sseWriters for a given session.
func broadcastToSession(sessionId string, eventType string, data string) {
	sseWritersMutex.Lock()
	defer sseWritersMutex.Unlock()

	writers, ok := activeSseWriters[sessionId]
	if !ok || len(writers) == 0 {
		return
	}

	// Prepare the event data once
	eventData := prepareSSEEventData(eventType, data)

	for _, sseW := range writers {
		sseW.mu.Lock()
		if !sseW.disconnected {
			_, err := sseW.writeUnsafe(eventData)
			if err == nil {
				sseW.flushUnsafe()
			}
		} else {
			log.Printf("Skipping broadcast to disconnected sseWriter for session %s", sessionId)
		}
		sseW.mu.Unlock()
	}
}

// Write implements the io.Writer interface for sseWriter.
// It checks for write errors and marks the connection as disconnected if an error occurs.
func (s *sseWriter) Write(p []byte) (n int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeUnsafe(p)
}

// writeUnsafe performs the actual write without locking (must be called with mutex held)
func (s *sseWriter) writeUnsafe(p []byte) (n int, err error) {
	if s.disconnected {
		return len(p), nil // Return nil error and assume all bytes are "written" to avoid stopping execution
	}
	n, err = s.ResponseWriter.Write(p)
	if err != nil {
		log.Printf("sseWriter: Write error: %v", err)
		s.disconnected = true
		return n, nil // Do not return error to caller, just log and mark disconnected
	}
	return n, nil // Return nil error on success
}

// Flush implements the http.Flusher interface for sseWriter.
// It only flushes if the connection is not marked as disconnected.
func (s *sseWriter) Flush() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.flushUnsafe()
}

// flushUnsafe performs the actual flush without locking (must be called with mutex held)
func (s *sseWriter) flushUnsafe() {
	if s.disconnected {
		return
	}

	// Check if context is cancelled (connection cleanup started)
	// This happens before bufio.Writer.Reset(nil) which would cause panic
	select {
	case <-s.ctx.Done():
		s.disconnected = true
		return
	default:
		// Context not cancelled, safe to flush
	}

	s.Flusher.Flush()
}

func (sseW *sseWriter) sendServerEvent(eventType, data string) {
	sseW.mu.Lock()
	defer sseW.mu.Unlock()

	if sseW.disconnected {
		return
	}

	eventData := prepareSSEEventData(eventType, data)
	_, err := sseW.writeUnsafe(eventData)
	if err == nil {
		sseW.flushUnsafe()
	}
}
