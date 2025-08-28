{{- if .Roots -}}
  {{- if .Roots.Added -}}
    {{- if gt (len .Roots.Added) 0 -}}
---

The following directories are now available from your working environment. You are also given the contents of each directory, which is current as of the following user message. Note that your tools' relative path operations (e.g., `read_file("file.txt")`) resolve against your session's *anonymous working directory*, which is separate. Therefore, to interact with files in the provided directories, you must use their full absolute paths.{{- "\n\n" -}}
      {{- range .Roots.Added -}}
## New directory `{{.Path}}`{{- "\n" -}}
        {{- if gt (len .Contents) 0 -}}
{{ .FormattedContents }}
        {{- else -}}
This directory is empty.{{- "\n" -}}
        {{- end -}}
      {{- end -}}
    {{- end -}}
  {{- end -}}
{{- "\n" -}}
  {{- if .Roots.Removed -}}
    {{- if gt (len .Roots.Removed) 0 -}}
## Paths no longer available:
The following directories are now unavailable from your working environment. You can no longer access these directories.{{- "\n\n" -}}
      {{- range .Roots.Removed -}}
- {{.Path}}{{- "\n" -}}
      {{- end -}}
    {{- end -}}
  {{- end -}}
{{- "\n" -}}
  {{- if .Roots.Prompts -}}
    {{- if gt (len .Roots.Prompts) 0 -}}
---

You are also given the following per-directory directives.{{- "\n" -}}
      {{- if gt (len .Roots.Removed) 0 -}}
Forget all prior per-directory directives in advance.{{- "\n" -}}
      {{- end -}}
{{- "\n" -}}
      {{- range .Roots.Prompts -}}
## Directives from `{{.Path}}`
{{.Prompt}}{{- "\n\n" -}}
      {{- end -}}
    {{- end -}}
  {{- end -}}
{{- end -}}