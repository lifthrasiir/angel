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

// GetEnvChangeContext formats EnvChanged into a plain text string.
func GetEnvChangeContext(envChanged EnvChanged) string {
	var builder strings.Builder

	if envChanged.Roots != nil {
		if len(envChanged.Roots.Added) > 0 {
			builder.WriteString("The following directories are now available from your working environment. You are also given the contents of each directory, which is current as of the following user message.\n\n")
			for _, added := range envChanged.Roots.Added {
				builder.WriteString(fmt.Sprintf("## New directory `%s`\n", added.Path))
				formatRootContents(&builder, added.Contents, "") // No initial indent for root contents
				builder.WriteString("\n")
			}
		}

		if len(envChanged.Roots.Removed) > 0 {
			builder.WriteString("## Paths no longer available:\n")
			builder.WriteString("The following directories are now unavailable from your working environment. You can no longer access these directories.\n\n")
			for _, removed := range envChanged.Roots.Removed {
				builder.WriteString(fmt.Sprintf("- %s\n", removed.Path))
			}
			builder.WriteString("\n")
		}

		if len(envChanged.Roots.Prompts) > 0 {
			builder.WriteString("---\n\n")
			builder.WriteString("You are also given the following per-directory directives.\n")
			if len(envChanged.Roots.Removed) > 0 {
				builder.WriteString("Forget all prior per-directory directives in advance.\n")
			}
			builder.WriteString("\n")

			for _, prompt := range envChanged.Roots.Prompts {
				builder.WriteString(fmt.Sprintf("## Directives from `%s`\n", prompt.Path))
				builder.WriteString(fmt.Sprintf("%s\n", prompt.Prompt))
				builder.WriteString("\n")
			}
		}
	}

	return builder.String()
}

// formatRootContents recursively formats RootContents for display.
func formatRootContents(builder *strings.Builder, contents []RootContents, indent string) {
	for _, content := range contents {
		builder.WriteString(fmt.Sprintf("%s%s", indent, content.Path))
		if content.IsDir {
			builder.WriteString("/")
		}
		if content.HasMore {
			builder.WriteString(" ...")
		}
		builder.WriteString("\n")
		if len(content.Children) > 0 {
			formatRootContents(builder, content.Children, indent+"  ")
		}
	}
}
