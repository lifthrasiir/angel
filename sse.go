package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	. "github.com/lifthrasiir/angel/internal/types"
)

const (
	// Ping interval - send ping if no other messages for 15 seconds
	PingInterval = 15 * time.Second
)

var _ EventWriter = (*sseWriter)(nil)

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
	headersSent  bool
	ctx          context.Context
	sessionId    string
	refCount     int
	lastPingSent time.Time // Track when ping was last sent
	pingTimer    *time.Timer
	pingMutex    sync.Mutex
}

func newSseWriter(ctx context.Context, sessionId string, w http.ResponseWriter) *sseWriter {
	flusher, ok := w.(http.Flusher)
	if !ok {
		sendInternalServerError(w, nil, nil, "Streaming unsupported!")
		return nil
	}

	sseW := &sseWriter{
		ResponseWriter: w,
		Flusher:        flusher,
		ctx:            ctx,
		sessionId:      sessionId,
	}

	return sseW
}

// initSseWriter gets called on the first acquisition or message sent, whichever is earlier.
// This initialization can't be done in newSseWriter because
// sseWriter has to be injected from the caller before the initial acquisition.
func (sseW *sseWriter) postInitUnsafe() {
	if sseW.headersSent {
		return
	}

	header := sseW.ResponseWriter.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache")
	header.Set("Connection", "keep-alive")
	header.Set("Access-Control-Allow-Origin", "*")
	header.Set("X-Accel-Buffering", "no") // For nginx

	sseW.lastPingSent = time.Now()
	sseW.startPingTimer()

	sseW.headersSent = true
}

// Close marks the sseWriter as disconnected and removes it from the active writers.
func (s *sseWriter) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.disconnected {
		s.disconnected = true
		s.stopPingTimer()
		s.Release()
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

// Acquire adds an sseWriter to the activeSseWriters map.
func (sseW *sseWriter) Acquire() {
	sseWritersMutex.Lock()
	defer sseWritersMutex.Unlock()

	if sseW.refCount == 0 {
		sseW.postInitUnsafe()
		activeSseWriters[sseW.sessionId] = append(activeSseWriters[sseW.sessionId], sseW)
	}
	sseW.refCount++
}

// Release removes an sseWriter from the activeSseWriters map.
func (sseW *sseWriter) Release() {
	sseWritersMutex.Lock()
	defer sseWritersMutex.Unlock()

	sseW.refCount--
	if sseW.refCount > 0 {
		return
	}

	writers := activeSseWriters[sseW.sessionId]
	for i, w := range writers {
		if w == sseW {
			activeSseWriters[sseW.sessionId] = append(writers[:i], writers[i+1:]...)
			return
		}
	}
}

// Broadcast sends an event to all active sseWriters for a given session.
func (sseW *sseWriter) Broadcast(eventType EventType, data string) {
	sseWritersMutex.Lock()
	defer sseWritersMutex.Unlock()

	writers, ok := activeSseWriters[sseW.sessionId]
	if !ok || len(writers) == 0 {
		return
	}

	sseW.postInitUnsafe()

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
			log.Printf("Skipping broadcast to disconnected sseWriter for session %s", sseW.sessionId)
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

func (sseW *sseWriter) Send(eventType EventType, data string) {
	sseW.mu.Lock()
	defer sseW.mu.Unlock()

	if sseW.disconnected {
		return
	}

	sseW.postInitUnsafe()

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
