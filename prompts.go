package main

import (
	"bytes"
	"fmt"
	"text/template"
)

// PromptData holds data that can be passed to the prompt templates.
type PromptData struct {
	Builtin BuiltinPrompts
}

// BuiltinPrompts holds references to the default system prompts.
type BuiltinPrompts struct {
}

// SystemPrompt returns the default system prompt.
func (b BuiltinPrompts) SystemPrompt() string {
	return GetDefaultSystemPrompt()
}

// String returns a string representation of available methods.
func (b BuiltinPrompts) String() string {
	return "Available methods: .SystemPrompt()"
}

// EvaluatePrompt evaluates the given prompt string as a Go template.
func EvaluatePrompt(promptContent string) (string, error) {
	tmpl, err := template.New("prompt").Parse(promptContent)
	if err != nil {
		return "", fmt.Errorf("failed to parse prompt template: %w", err)
	}

	data := PromptData{
		Builtin: BuiltinPrompts{},
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, data)
	if err != nil {
		return "", fmt.Errorf("failed to execute prompt template: %w", err)
	}

	return buf.String(), nil
}

// GetEvaluatedSystemPrompt evaluates the system prompt template and returns the result.
func GetEvaluatedSystemPrompt(systemPromptTemplate string) (string, error) {
	evaluatedPrompt, err := EvaluatePrompt(systemPromptTemplate)
	if err != nil {
		return "", fmt.Errorf("error evaluating system prompt template: %w", err)
	}
	return evaluatedPrompt, nil
}
