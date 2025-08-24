package main

import (
	"bytes"
	"embed"
	"fmt"
	"log"
	"runtime"
	"strings"
	"text/template"
	"time"
)

//go:embed prompts/*.md
var promptsDir embed.FS

func TodayInPrompt() string {
	return time.Now().Format("January 2, 2006")
}

func PlatformInPrompt() string {
	platform, _, _ := strings.Cut(runtime.GOOS, "/")
	switch platform {
	case "darwin":
		return "macos"
	default:
		return platform
	}
}

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
func (d PromptData) Today() string              { return TodayInPrompt() }
func (d PromptData) Platform() string           { return PlatformInPrompt() }
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

func (p BuiltinPrompts) systemPromptArgs() map[string]any {
	return map[string]any{
		"Today":         TodayInPrompt(),
		"Platform":      PlatformInPrompt(),
		"WorkspaceName": p.data.workspaceName,
	}
}

func (p BuiltinPrompts) SystemPrompt() string {
	return executePromptTemplate("system-prompt-minimal.md", p.systemPromptArgs())
}
func (p BuiltinPrompts) SystemPromptForCoding() string {
	return executePromptTemplate("system-prompt-coding.md", p.systemPromptArgs())
}
func (p BuiltinPrompts) DynamicPromptTool() string {
	return executePromptTemplate("tool-dynamic-prompt.md", nil)
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

func executePromptTemplate(filename string, data map[string]any) string {
	tmpl, err := template.New("").ParseFS(promptsDir, "prompts/*.md")
	if err != nil {
		log.Printf("Error parsing template files: %v", err)
		return ""
	}

	tmpl = tmpl.Lookup(filename)
	if tmpl == nil {
		log.Printf("Error: template %s not found after parsing all files", filename)
		return ""
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		log.Printf("Error executing template %s: %v", filename, err)
		return ""
	}
	return buf.String()
}
