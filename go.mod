module github.com/lifthrasiir/angel

go 1.24.4

require (
	github.com/lifthrasiir/angel/editor v0.0.0-00010101000000-000000000000 // indirect
	github.com/lifthrasiir/angel/filesystem v0.0.0-00010101000000-000000000000
	github.com/lifthrasiir/angel/gemini v0.0.0-00010101000000-000000000000
	github.com/lifthrasiir/angel/internal/chat v0.0.0-00010101000000-000000000000
	github.com/lifthrasiir/angel/internal/database v0.0.0-00010101000000-000000000000
	github.com/lifthrasiir/angel/internal/env v0.0.0-00010101000000-000000000000
	github.com/lifthrasiir/angel/internal/llm v0.0.0-00010101000000-000000000000
	github.com/lifthrasiir/angel/internal/prompts v0.0.0-00010101000000-000000000000
	github.com/lifthrasiir/angel/internal/tool v0.0.0-00010101000000-000000000000
	github.com/lifthrasiir/angel/internal/tool/file v0.0.0-00010101000000-000000000000
	github.com/lifthrasiir/angel/internal/tool/search_chat v0.0.0-00010101000000-000000000000
	github.com/lifthrasiir/angel/internal/tool/shell v0.0.0-00010101000000-000000000000
	github.com/lifthrasiir/angel/internal/tool/subagent v0.0.0-00010101000000-000000000000
	github.com/lifthrasiir/angel/internal/tool/todo v0.0.0-00010101000000-000000000000
	github.com/lifthrasiir/angel/internal/tool/webfetch v0.0.0-00010101000000-000000000000
	github.com/lifthrasiir/angel/internal/types v0.0.0-00010101000000-000000000000
)

replace (
	github.com/lifthrasiir/angel/editor => ./src/editor
	github.com/lifthrasiir/angel/filesystem => ./src/filesystem
	github.com/lifthrasiir/angel/gemini => ./src/gemini
	github.com/lifthrasiir/angel/internal/chat => ./src/internal/chat
	github.com/lifthrasiir/angel/internal/database => ./src/internal/database
	github.com/lifthrasiir/angel/internal/env => ./src/internal/env
	github.com/lifthrasiir/angel/internal/llm => ./src/internal/llm
	github.com/lifthrasiir/angel/internal/prompts => ./src/internal/prompts
	github.com/lifthrasiir/angel/internal/tool => ./src/internal/tool
	github.com/lifthrasiir/angel/internal/tool/file => ./src/internal/tool/file
	github.com/lifthrasiir/angel/internal/tool/search_chat => ./src/internal/tool/search_chat
	github.com/lifthrasiir/angel/internal/tool/shell => ./src/internal/tool/shell
	github.com/lifthrasiir/angel/internal/tool/subagent => ./src/internal/tool/subagent
	github.com/lifthrasiir/angel/internal/tool/todo => ./src/internal/tool/todo
	github.com/lifthrasiir/angel/internal/tool/webfetch => ./src/internal/tool/webfetch
	github.com/lifthrasiir/angel/internal/types => ./src/internal/types
)

require (
	github.com/fvbommel/sortorder v1.1.0
	github.com/gorilla/csrf v1.7.3
	github.com/gorilla/mux v1.8.1
	golang.org/x/oauth2 v0.23.0
)

require (
	cloud.google.com/go/compute/metadata v0.5.0 // indirect
	github.com/bmatcuk/doublestar/v4 v4.9.1 // indirect
	github.com/gorilla/securecookie v1.1.2 // indirect
	github.com/k3a/html2text v1.2.0 // indirect
	github.com/modelcontextprotocol/go-sdk v0.2.0 // indirect
	github.com/ncruces/go-sqlite3 v0.30.3 // indirect
	github.com/ncruces/julianday v1.0.0 // indirect
	github.com/sergi/go-diff v1.4.0 // indirect
	github.com/tetratelabs/wazero v1.10.1 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	golang.org/x/sys v0.38.0 // indirect
)
