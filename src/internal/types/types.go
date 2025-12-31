package types

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	. "github.com/lifthrasiir/angel/gemini"
)

// Branch struct to hold branch data
type Branch struct {
	ID                  string  `json:"id"`
	LocalSessionID      string  `json:"session_id"`
	ParentBranchID      *string `json:"parent_branch_id"`       // Pointer for nullable
	BranchFromMessageID *int    `json:"branch_from_message_id"` // Pointer for nullable
	CreatedAt           string  `json:"created_at"`
	PendingConfirmation *string `json:"pending_confirmation"`
}

// FileAttachment struct to hold file attachment data
type FileAttachment struct {
	FileName  string `json:"fileName"`
	MimeType  string `json:"mimeType"`
	Hash      string `json:"hash"`                // SHA-512/256 hash of the data
	Data      []byte `json:"data,omitempty"`      // Raw binary data, used temporarily for upload/download
	Omitted   bool   `json:"omitted,omitempty"`   // Whether attachment was omitted due to clearblobs
	SessionId string `json:"sessionId,omitempty"` // Session ID for blob URL (required for fetching from backend)
}

// Message struct to hold message data for database interaction
type Message struct {
	ID                      int              `json:"id"`
	LocalSessionID          string           `json:"session_id"`
	BranchID                string           `json:"branch_id"`
	ParentMessageID         *int             `json:"parent_message_id"`
	ChosenNextID            *int             `json:"chosen_next_id"`
	Text                    string           `json:"text"`
	Type                    MessageType      `json:"type"`
	Attachments             []FileAttachment `json:"attachments,omitempty"`
	CumulTokenCount         *int             `json:"cumul_token_count,omitempty"`
	CreatedAt               string           `json:"created_at"`
	Model                   string           `json:"model,omitempty"`
	CompressedUpToMessageID *int             `json:"compressed_up_to_message_id,omitempty"`
	Generation              int              `json:"generation"`
	State                   string           `json:"state,omitempty"`
	Aux                     string           `json:"aux,omitempty"`
	Indexed                 int              `json:"indexed"`
}

type MessageType string

const (
	RoleUser    = "user"
	RoleModel   = "model"
	RoleThought = "thought"

	TypeUserText         MessageType = "user"
	TypeModelText        MessageType = "model"
	TypeFunctionCall     MessageType = "function_call"
	TypeFunctionResponse MessageType = "function_response"
	TypeThought          MessageType = "thought"
	TypeCompression      MessageType = "compression"
	TypeSystemPrompt     MessageType = "system_prompt"
	TypeEnvChanged       MessageType = "env_changed"
	TypeError            MessageType = "error"
	TypeModelError       MessageType = "model_error"
	TypeCommand          MessageType = "command"
)

func (mt MessageType) Role() string {
	switch mt {
	case TypeUserText, TypeFunctionResponse, TypeCompression, TypeSystemPrompt, TypeEnvChanged, TypeCommand:
		return RoleUser
	case TypeModelText, TypeModelError, TypeFunctionCall, TypeError:
		return RoleModel
	case TypeThought:
		return RoleThought
	default:
		return ""
	}
}

func (mt MessageType) Curated() bool {
	return mt != TypeThought
}

// PossibleNextMessage struct to hold possible next message data for the frontend
type PossibleNextMessage struct {
	MessageID string `json:"messageId"`
	BranchID  string `json:"branchId"`
	UserText  string `json:"userText,omitempty"`
	Timestamp int64  `json:"timestamp,omitempty"`
}

// FrontendMessage struct to match the frontend's ChatMessage interface
type FrontendMessage struct {
	ID               string                `json:"id"`
	Parts            []Part                `json:"parts"`
	Type             MessageType           `json:"type"`
	Attachments      []FileAttachment      `json:"attachments,omitempty"`
	CumulTokenCount  *int                  `json:"cumul_token_count,omitempty"`
	SessionID        string                `json:"sessionId,omitempty"`
	BranchID         string                `json:"branchId,omitempty"`
	ParentMessageID  *string               `json:"parentMessageId,omitempty"`
	ChosenNextID     *string               `json:"chosenNextId,omitempty"`
	PossibleBranches []PossibleNextMessage `json:"possibleBranches,omitempty"`
	Model            string                `json:"model,omitempty"`
}

// Session struct to hold session data
type Session struct {
	ID              string `json:"id"`
	LastUpdated     string `json:"last_updated_at"`
	SystemPrompt    string `json:"system_prompt"`
	Name            string `json:"name"`
	WorkspaceID     string `json:"workspace_id"`
	PrimaryBranchID string `json:"primary_branch_id"`
}

// SplitSessionId splits a session ID into main session ID and suffix for subsessions
// Return values satisfy `sessionId == mainSessionId + suffix`.
func SplitSessionId(sessionId string) (mainSessionId, suffix string) {
	if sessionId == "" {
		return "", ""
	}
	sep := strings.IndexByte(sessionId[1:], '.')
	if sep < 0 {
		return sessionId, ""
	}
	return sessionId[:sep+1], sessionId[sep+1:]
}

func IsSubsessionId(sessionId string) bool {
	return sessionId != "" && strings.Contains(sessionId[1:], ".")
}

func IsTemporarySessionId(sessionId string) bool {
	return strings.HasPrefix(sessionId, ".")
}

// Workspace struct to hold workspace data
type Workspace struct {
	ID                  string `json:"id"`
	Name                string `json:"name"`
	DefaultSystemPrompt string `json:"default_system_prompt"`
	CreatedAt           string `json:"created_at"`
}

// GlobalPrompt struct to hold global prompt data
type PredefinedPrompt struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// MCPServerConfig struct to hold MCP server configuration data
type MCPServerConfig struct {
	Name       string          `json:"name"`
	ConfigJSON json.RawMessage `json:"config_json"`
	Enabled    bool            `json:"enabled"`
}

// OpenAIConfig struct to hold OpenAI-compatible API configuration data
type OpenAIConfig struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Endpoint  string `json:"endpoint"`
	APIKey    string `json:"api_key"`
	Enabled   bool   `json:"enabled"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// Hash generates a unique hash for OpenAI config to detect changes
func (config *OpenAIConfig) Hash() string {
	hasher := sha256.New()
	hasher.Write([]byte(config.Endpoint))
	hasher.Write([]byte(config.APIKey))
	hasher.Write([]byte(fmt.Sprintf("%v", config.Enabled)))
	return hex.EncodeToString(hasher.Sum(nil))
}

// GeminiAPIConfig represents a Gemini API configuration
type GeminiAPIConfig struct {
	ID              string               `json:"id"`
	Name            string               `json:"name"`
	APIKey          string               `json:"api_key"`
	Enabled         bool                 `json:"enabled"`
	LastUsedByModel map[string]time.Time `json:"last_used_by_model"`
	CreatedAt       string               `json:"created_at"`
	UpdatedAt       string               `json:"updated_at"`
}

// OAuthToken represents an OAuth token
type OAuthToken struct {
	ID              int                  `json:"id"`
	TokenData       string               `json:"token_data"`
	UserEmail       string               `json:"user_email"`
	ProjectID       string               `json:"project_id"`
	Kind            string               `json:"kind"`
	LastUsedByModel map[string]time.Time `json:"last_used_by_model"`
	CreatedAt       string               `json:"created_at"`
	UpdatedAt       string               `json:"updated_at"`
}

// ShellCommand struct to hold shell command data
type ShellCommand struct {
	ID            string
	BranchID      string
	Command       string
	Status        string
	StartTime     int64         // Unix timestamp
	EndTime       sql.NullInt64 // Unix timestamp, nullable
	Stdout        []byte
	Stderr        []byte
	ExitCode      sql.NullInt64  // Nullable
	ErrorMessage  sql.NullString // Nullable
	LastPolledAt  int64          // Unix timestamp
	NextPollDelay int64          // Seconds
	StdoutOffset  int64          // New: Last read offset for stdout
	StderrOffset  int64          // New: Last read offset for stderr
}
