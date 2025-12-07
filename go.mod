module github.com/lifthrasiir/angel

go 1.24.4

require (
	github.com/lifthrasiir/angel/editor v0.0.0-00010101000000-000000000000
	github.com/lifthrasiir/angel/fs v0.0.0-00010101000000-000000000000
	github.com/lifthrasiir/angel/gemini v0.0.0-00010101000000-000000000000
)

replace (
	github.com/lifthrasiir/angel/editor => ./src/editor
	github.com/lifthrasiir/angel/fs => ./src/fs
	github.com/lifthrasiir/angel/gemini => ./src/gemini
)

require (
	github.com/fvbommel/sortorder v1.1.0
	github.com/google/uuid v1.6.0
	github.com/gorilla/csrf v1.7.3
	github.com/gorilla/mux v1.8.1
	github.com/k3a/html2text v1.2.0
	github.com/modelcontextprotocol/go-sdk v0.2.0
	github.com/ncruces/go-sqlite3 v0.30.3
	golang.org/x/oauth2 v0.23.0
)

require (
	cloud.google.com/go/compute/metadata v0.5.0 // indirect
	github.com/bmatcuk/doublestar/v4 v4.9.1 // indirect
	github.com/gorilla/securecookie v1.1.2 // indirect
	github.com/ncruces/julianday v1.0.0 // indirect
	github.com/sergi/go-diff v1.4.0 // indirect
	github.com/tetratelabs/wazero v1.10.1 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	golang.org/x/sys v0.38.0 // indirect
)
