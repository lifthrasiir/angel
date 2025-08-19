package main

import (
	"bytes"
	"fmt"
	"text/template"
)

// PromptData holds data that can be passed to the prompt templates.
type PromptData struct {
	workspaceName string
}

func (PromptData) String() string {
	return `Available methods:

.Builtin    Built-in prompts. Print to inspect.
.Today      Current date in 'August 10, 2025' format.
.Platform   Current operating system (windows, macos, linux etc).
.Workspace  Workspace information. Print to inspect.
`
}

func (d PromptData) Builtin() BuiltinPrompts    { return BuiltinPrompts{data: d} }
func (PromptData) Today() string                { return TodayInPrompt() }
func (PromptData) Platform() string             { return PlatformInPrompt() }
func (d PromptData) Workspace() PromptWorkspace { return PromptWorkspace{data: d} }

// BuiltinPrompts holds references to the default system prompts.
type BuiltinPrompts struct{ data PromptData }

func (BuiltinPrompts) String() string {
	return `Available methods:

.Builtin.SystemPrompt           Default, minimal system prompt.
.Builtin.SystemPromptForCoding  System prompt suitable for coding agents.
.Builtin.DynamicPromptTool      Context for dynamic prompt tool ('new_system_prompt').
`
}

func (p BuiltinPrompts) SystemPrompt() string {
	return p.data.GetMinimalSystemPrompt()
}
func (p BuiltinPrompts) SystemPromptForCoding() string {
	return p.data.GetDefaultSystemPromptForCoding()
}
func (p BuiltinPrompts) DynamicPromptTool() string {
	return GetDynamicPromptToolPrompt()
}

// PromptWorkspace holds the current workspace information.
type PromptWorkspace struct{ data PromptData }

func (PromptWorkspace) String() string {
	return `Available methods:

.Workspace.Name     Workspace name.
`
}

func (w PromptWorkspace) Name() string { return w.data.workspaceName }

// EvaluatePrompt evaluates the given prompt string as a Go template.
func (d PromptData) EvaluatePrompt(promptContent string) (string, error) {
	tmpl, err := template.New("prompt").Parse(promptContent)
	if err != nil {
		return "", fmt.Errorf("failed to parse prompt template: %w", err)
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, d)
	if err != nil {
		return "", fmt.Errorf("failed to execute prompt template: %w", err)
	}

	return buf.String(), nil
}
