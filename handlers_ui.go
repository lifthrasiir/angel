package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"

	"github.com/sqweek/dialog"
)

// UILogicResultType indicates the outcome of the UI dialog operation.
type UILogicResultType string

const (
	UILogicResultTypeSuccess     UILogicResultType = "success"
	UILogicResultTypeCanceled    UILogicResultType = "canceled"
	UILogicResultTypeAlreadyOpen UILogicResultType = "already_open"
	UILogicResultTypeError       UILogicResultType = "error"
)

// PickDirectoryAPIResponse holds the path and the type of result for API response.
type PickDirectoryAPIResponse struct {
	SelectedPath string            `json:"selectedPath,omitempty"`
	Result       UILogicResultType `json:"result"`
	Error        string            `json:"error,omitempty"`
}

var (
	uiDialogMutex    sync.Mutex // Ensures only one UI dialog (like directory picker) is active at a time
	isUIDialogActive bool       // Flag to track if a UI dialog is currently open
)

// pickDirectoryInternal opens a native directory picker dialog.
// It uses a mutex to ensure only one dialog is active at a time.
// The context is used to cancel the dialog operation if the HTTP request is cancelled.
func pickDirectoryInternal(ctx context.Context) PickDirectoryAPIResponse {
	log.Println("Attempting to acquire UI dialog mutex...")
	if !uiDialogMutex.TryLock() { // Acquire a non-blocking lock
		log.Println("UI dialog mutex already locked, another dialog is active.")
		return PickDirectoryAPIResponse{Result: UILogicResultTypeAlreadyOpen, Error: "Another dialog is already open."}
	}
	defer uiDialogMutex.Unlock()
	log.Println("UI dialog mutex acquired.")

	// Double-check after acquiring lock in case another goroutine set it to true
	if isUIDialogActive {
		log.Println("isUIDialogActive flag is true, another dialog is active.")
		return PickDirectoryAPIResponse{Result: UILogicResultTypeAlreadyOpen, Error: "Another dialog is already open."}
	}

	isUIDialogActive = true
	defer func() {
		isUIDialogActive = false
		log.Println("UI dialog operation finished, isUIDialogActive set to false.")
	}()

	resultChan := make(chan PickDirectoryAPIResponse, 1)

	go func() {
		log.Println("Opening directory picker dialog...")
		dir, err := dialog.Directory().Title("Select Directory").Browse()
		log.Printf("Directory picker dialog closed. Dir: %s, Error: %v\n", dir, err)

		if err != nil {
			if err == dialog.ErrCancelled {
				resultChan <- PickDirectoryAPIResponse{Result: UILogicResultTypeCanceled}
			} else {
				resultChan <- PickDirectoryAPIResponse{Result: UILogicResultTypeError, Error: fmt.Sprintf("Failed to select directory: %v", err)}
			}
		} else {
			resultChan <- PickDirectoryAPIResponse{SelectedPath: dir, Result: UILogicResultTypeSuccess}
		}
	}()

	select {
	case <-ctx.Done():
		// HTTP request was cancelled. We cannot programmatically close the native dialog,
		// but we can at least signal that the operation was interrupted.
		// The dialog will remain open on the user's screen but its result won't be processed.
		log.Printf("Directory picking cancelled by context: %v", ctx.Err())
		return PickDirectoryAPIResponse{Result: UILogicResultTypeCanceled, Error: "Operation cancelled by client."}
	case res := <-resultChan:
		log.Printf("Directory picking completed: %+v\n", res)
		return res
	}
}

// handlePickDirectory handles the /api/ui/directory POST request.
// It triggers a native directory selection dialog on the server side.
func handlePickDirectory(w http.ResponseWriter, r *http.Request) {
	log.Println("Received /api/ui/directory request.")
	// Use the request context to allow cancellation if the client disconnects.
	res := pickDirectoryInternal(r.Context())

	w.Header().Set("Content-Type", "application/json")
	encoder := json.NewEncoder(w)

	switch res.Result {
	case UILogicResultTypeSuccess:
		log.Printf("Directory selected: %s\n", res.SelectedPath)
		w.WriteHeader(http.StatusOK)
		encoder.Encode(res)
	case UILogicResultTypeCanceled:
		log.Println("Directory selection canceled by user or client.")
		w.WriteHeader(http.StatusOK) // 200 OK for user cancellation
		encoder.Encode(res)
	case UILogicResultTypeAlreadyOpen:
		log.Println("Another directory dialog is already open.")
		w.WriteHeader(http.StatusConflict) // 409 Conflict
		encoder.Encode(res)
	case UILogicResultTypeError:
		log.Printf("Directory selection error: %v\n", res.Error)
		w.WriteHeader(http.StatusInternalServerError) // 500 Internal Server Error
		encoder.Encode(res)
	default:
		log.Printf("Unknown result type: %s\n", res.Result)
		w.WriteHeader(http.StatusInternalServerError)
		encoder.Encode(PickDirectoryAPIResponse{
			Result: UILogicResultTypeError,
			Error:  "Unknown error during directory selection.",
		})
	}
}
