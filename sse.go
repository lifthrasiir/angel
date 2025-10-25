package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

type EventType rune

const (
	// Ping interval - send ping if no other messages for 15 seconds
	PingInterval = 15 * time.Second
)

const (
	// SSE Event Types
	//
	// Sending initial messages: A -> 0 -> any number of T/M/F/R/C/I -> P or (Q -> N) or E
	// Sending subsequent messages: any number of G -> A -> any number of T/M/F/R/C/I -> P/Q/E
	// Loading messages and streaming current call: 1 or (0 -> any number of T/M/F/R/C/I -> Q/E)
	EventInitialState        EventType = '0' // Initial state with history (for active call)
	EventInitialStateNoCall  EventType = '1' // Initial state with history (for load session when no active call)
	EventAcknowledge         EventType = 'A' // Acknowledge message ID
	EventThought             EventType = 'T' // Thought process
	EventModelMessage        EventType = 'M' // Model message (text)
	EventFunctionCall        EventType = 'F' // Function call
	EventFunctionResponse    EventType = 'R' // Function response
	EventInlineData          EventType = 'I' // Inline file/image data with hash keys
	EventComplete            EventType = 'Q' // Query complete
	EventSessionName         EventType = 'N' // Session name inferred/updated
	EventCumulTokenCount     EventType = 'C' // Cumulative token count update
	EventPendingConfirmation EventType = 'P' // Pending confirmation exists for the last message which is EventFunctionCall
	EventGenerationChanged   EventType = 'G' // Generation changed event
	EventPing                EventType = '.' // Ping message for connection keep-alive
	EventError               EventType = 'E' // Error message
)

// FunctionResponsePayload defines the structure for the EventFunctionResponse payload
type FunctionResponsePayload struct {
	Response    map[string]interface{} `json:"response"`
	Attachments []FileAttachment       `json:"attachments,omitempty"`
}

// InlineDataPayload defines the structure for the EventInlineData payload
type InlineDataPayload struct {
	MessageId   string           `json:"messageId"`
	Attachments []FileAttachment `json:"attachments"`
}

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
	refCount     int
	lastPingSent time.Time // Track when ping was last sent
	pingTimer    *time.Timer
	pingMutex    sync.Mutex
}

func newSseWriter(sessionId string, w http.ResponseWriter, r *http.Request) *sseWriter {
	header := w.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache")
	header.Set("Connection", "keep-alive")
	header.Set("Access-Control-Allow-Origin", "*")
	header.Set("X-Accel-Buffering", "no") // For nginx

	flusher, ok := w.(http.Flusher)
	if !ok {
		sendInternalServerError(w, r, nil, "Streaming unsupported!")
		return nil
	}

	sseW := &sseWriter{
		ResponseWriter: w,
		Flusher:        flusher,
		ctx:            r.Context(),
		sessionId:      sessionId,
		lastPingSent:   time.Now(),
	}

	// Start the ping timer
	sseW.startPingTimer()

	return sseW
}

// Close marks the sseWriter as disconnected and removes it from the active writers.
func (s *sseWriter) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.disconnected {
		s.disconnected = true
		s.stopPingTimer()
		removeSseWriter(s.sessionId, s)
	}
}

// prepareSSEEventData prepares the SSE event data string.
// Note: `eventType` is for the logical message kind, not the browser event type!
func prepareSSEEventData(eventType EventType, data string) []byte {
	escapedData := strings.ReplaceAll(data, "\n", "\ndata: ")
	return []byte(fmt.Sprintf("data: %c\ndata: %s\n\n", eventType, escapedData))
}

var (
	activeSseWriters = make(map[string][]*sseWriter) // sessionId -> list of sseWriters
	sseWritersMutex  sync.Mutex
)

// addSseWriter adds an sseWriter to the activeSseWriters map.
func addSseWriter(sessionId string, sseW *sseWriter) {
	sseWritersMutex.Lock()
	defer sseWritersMutex.Unlock()

	if sseW.refCount == 0 {
		activeSseWriters[sessionId] = append(activeSseWriters[sessionId], sseW)
	}
	sseW.refCount++
}

// removeSseWriter removes an sseWriter from the activeSseWriters map.
func removeSseWriter(sessionId string, sseW *sseWriter) {
	sseWritersMutex.Lock()
	defer sseWritersMutex.Unlock()

	sseW.refCount--
	if sseW.refCount > 0 {
		return
	}

	writers := activeSseWriters[sessionId]
	for i, w := range writers {
		if w == sseW {
			activeSseWriters[sessionId] = append(writers[:i], writers[i+1:]...)
			return
		}
	}
}

// broadcastToSession sends an event to all active sseWriters for a given session.
func broadcastToSession(sessionId string, eventType EventType, data string) {
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

func (sseW *sseWriter) sendServerEvent(eventType EventType, data string) {
	sseW.mu.Lock()
	defer sseW.mu.Unlock()

	if sseW.disconnected {
		return
	}

	// Don't reset timer for ping events themselves
	if eventType != EventPing {
		sseW.resetPingTimer()
	}

	eventData := prepareSSEEventData(eventType, data)
	_, err := sseW.writeUnsafe(eventData)
	if err == nil {
		sseW.flushUnsafe()
	}
}

// startPingTimer starts the automatic ping timer for this sseWriter
func (s *sseWriter) startPingTimer() {
	s.pingMutex.Lock()
	defer s.pingMutex.Unlock()

	if s.pingTimer != nil {
		s.pingTimer.Stop()
	}

	s.pingTimer = time.AfterFunc(PingInterval, s.sendPing)
}

// stopPingTimer stops the ping timer
func (s *sseWriter) stopPingTimer() {
	s.pingMutex.Lock()
	defer s.pingMutex.Unlock()

	if s.pingTimer != nil {
		s.pingTimer.Stop()
		s.pingTimer = nil
	}
}

// resetPingTimer resets the ping timer to start counting from now
func (s *sseWriter) resetPingTimer() {
	s.pingMutex.Lock()
	defer s.pingMutex.Unlock()

	if s.pingTimer != nil {
		s.pingTimer.Stop()
	}
	s.pingTimer = time.AfterFunc(PingInterval, s.sendPing)
}

// sendPing sends a ping message and schedules the next ping
func (s *sseWriter) sendPing() {
	// Check if still connected before trying to send
	s.mu.Lock()
	if s.disconnected {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	// Send the ping event
	eventData := prepareSSEEventData(EventPing, "")

	s.mu.Lock()
	if !s.disconnected {
		_, err := s.writeUnsafe(eventData)
		if err == nil {
			s.flushUnsafe()
		}

		// Schedule next ping if still connected
		if !s.disconnected {
			s.mu.Unlock()
			s.pingMutex.Lock()
			if s.pingTimer != nil {
				s.pingTimer.Stop()
			}
			s.pingTimer = time.AfterFunc(PingInterval, s.sendPing)
			s.pingMutex.Unlock()
		} else {
			s.mu.Unlock()
		}
	} else {
		s.mu.Unlock()
	}
}
