package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
)

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
)

func (mt MessageType) Role() string {
	switch mt {
	case TypeUserText, TypeFunctionResponse, TypeCompression, TypeSystemPrompt, TypeEnvChanged:
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

// MessageChain represents a sequence of messages in a conversation branch.
type MessageChain struct {
	SessionID             string
	BranchID              string
	Messages              []Message
	LastMessageID         int
	LastMessageGeneration int
	LastMessageModel      string
}

// NewMessageChain creates a new MessageChain with the given session and branch IDs.
// It also initializes LastMessage by fetching the last message from the database.
func NewMessageChain(ctx context.Context, db DbOrTx, sessionID, branchID string) (mc *MessageChain, err error) {
	mc = &MessageChain{
		SessionID: sessionID,
		BranchID:  branchID,
		Messages:  []Message{},
	}

	// Get the last message ID for the current branch from the database
	mc.LastMessageID, mc.LastMessageModel, mc.LastMessageGeneration, err = GetLastMessageInBranch(db, mc.SessionID, mc.BranchID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// No messages in this branch yet, LastMessage remains nil
			mc.LastMessageID = 0
			mc.LastMessageGeneration = 0
			mc.LastMessageModel = ""
		} else {
			return nil, fmt.Errorf("failed to get last message in branch for NewMessageChain: %w", err)
		}
	}

	if mc.LastMessageModel == "" {
		mc.LastMessageModel = DefaultGeminiModel
	}

	return mc, nil
}

// Add adds a message to the chain, updating parent_message_id and chosen_next_id.
// It returns the offset to mc.Messages.
func (mc *MessageChain) Add(ctx context.Context, db DbOrTx, msg Message) (Message, error) {
	msg.SessionID = mc.SessionID
	msg.BranchID = mc.BranchID

	var parentMessageID *int
	if mc.LastMessageID != 0 {
		parentMessageID = &mc.LastMessageID
	}
	msg.ParentMessageID = parentMessageID

	if msg.Generation == 0 {
		msg.Generation = mc.LastMessageGeneration // Default to last message's generation
	}
	if msg.Model == "" {
		msg.Model = mc.LastMessageModel // Default to last message's model
	}

	// Add the message to the database
	messageID, err := AddMessageToSession(ctx, db, msg)
	if err != nil {
		return Message{}, fmt.Errorf("failed to add message to session: %w", err)
	}
	msg.ID = messageID // Update the message ID after it's saved to DB

	// If there was a previous message, update its chosen_next_id
	if mc.LastMessageID != 0 {
		if err := UpdateMessageChosenNextID(db, mc.LastMessageID, &messageID); err != nil {
			return Message{}, fmt.Errorf("failed to update chosen_next_id for previous message: %w", err)
		}
	} else {
		// This is the first message in the chain, update session's chosen_first_id
		sqlDB, ok := db.(*sql.DB)
		if !ok {
			// Can't cast to *sql.DB, skip session update
			return msg, nil
		}
		if err := UpdateSessionChosenFirstID(sqlDB, mc.SessionID, &messageID); err != nil {
			// Non-fatal error, log but continue
			log.Printf("Failed to update chosen_first_id for session %s: %v", mc.SessionID, err)
		}
	}

	mc.Messages = append(mc.Messages, msg)
	mc.LastMessageID = msg.ID

	return msg, nil
}
