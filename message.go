package main

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
