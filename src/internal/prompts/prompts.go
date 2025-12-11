package prompts

import (
	"bytes"
	"embed"
	"fmt"
	"log"
	"runtime"
	"strings"
	"text/template"
	"time"

	"github.com/lifthrasiir/angel/internal/env"
)

//go:embed *.md
var promptsDir embed.FS

func Today() string {
	return time.Now().Format("January 2, 2006")
}

func Platform() string {
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

func NewPromptData(workspaceName string) PromptData {
	return PromptData{workspaceName: workspaceName}
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
func (d PromptData) Today() string              { return Today() }
func (d PromptData) Platform() string           { return Platform() }
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
		"Today":         Today(),
		"Platform":      Platform(),
		"WorkspaceName": p.data.workspaceName,
	}
}

func (p BuiltinPrompts) SystemPrompt() string {
	return ExecuteTemplate("system-prompt-minimal.md", p.systemPromptArgs())
}
func (p BuiltinPrompts) SystemPromptForCoding() string {
	return ExecuteTemplate("system-prompt-coding.md", p.systemPromptArgs())
}
func (p BuiltinPrompts) DynamicPromptTool() string {
	return ExecuteTemplate("tool-dynamic-prompt.md", nil)
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

func ExecuteTemplate(filename string, data map[string]any) string {
	// FuncMap is no longer needed here for formatRootContents
	tmpl, err := template.New("").ParseFS(promptsDir, "*.md")
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

// GetEnvChangeContext formats EnvChanged into a plain text string using a template.
func GetEnvChangeContext(envChanged env.EnvChanged) string {
	// Create a new map to hold the data for the template
	templateData := make(map[string]any)

	if envChanged.Roots != nil {
		// Create a temporary structure to hold the data for the template
		// This allows us to add the FormattedContents field
		type TempRootAdded struct {
			env.RootAdded
			FormattedContents string
		}

		var tempAdded []TempRootAdded
		for _, added := range envChanged.Roots.Added {
			var builder strings.Builder
			formatRootContents(&builder, added.Contents, "")
			tempAdded = append(tempAdded, TempRootAdded{
				RootAdded:         added,
				FormattedContents: builder.String(),
			})
		}

		templateData["Roots"] = map[string]any{
			"Value":   envChanged.Roots.Value,
			"Added":   tempAdded, // Use the temporary slice with formatted contents
			"Removed": envChanged.Roots.Removed,
			"Prompts": envChanged.Roots.Prompts,
		}
	}

	return ExecuteTemplate("environment-change.md", templateData)
}

// formatRootContents recursively formats RootContents for display in a tree-like structure.
func formatRootContents(builder *strings.Builder, contents []env.RootContents, prefix string) {
	for i, content := range contents {
		isLast := (i == len(contents)-1)
		var currentPrefix string
		if isLast {
			currentPrefix = prefix + "└─ "
		} else {
			currentPrefix = prefix + "├─ "
		}
		builder.WriteString(currentPrefix)

		builder.WriteString(content.Name) // content.Name will contain "..." if applicable
		builder.WriteString("\n")

		if len(content.Children) > 0 {
			var childPrefix string
			if isLast {
				childPrefix = prefix + "   "
			} else {
				childPrefix = prefix + "│  "
			}
			formatRootContents(builder, content.Children, childPrefix)
		}
	}
}
