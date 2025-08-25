module github.com/lifthrasiir/angel

go 1.24.4

require (
	github.com/google/uuid v1.6.0
	github.com/gorilla/mux v1.8.1
	github.com/k3a/html2text v1.2.0 // Added for web_fetch tool
	github.com/mattn/go-sqlite3 v1.14.29
	github.com/modelcontextprotocol/go-sdk v0.2.0
	golang.org/x/oauth2 v0.23.0
)

replace (
	github.com/lifthrasiir/angel/editor => ./src/editor
	github.com/lifthrasiir/angel/fs => ./src/fs
)

require (
	github.com/gorilla/csrf v1.7.3
	github.com/lifthrasiir/angel/editor v0.0.0-00010101000000-000000000000
	github.com/lifthrasiir/angel/fs v0.0.0-00010101000000-000000000000
	github.com/sqweek/dialog v0.0.0-20240226140203-065105509627
)

require (
	cloud.google.com/go/compute/metadata v0.5.0 // indirect
	github.com/TheTitanrain/w32 v0.0.0-20180517000239-4f5cfb03fabf // indirect
	github.com/bmatcuk/doublestar/v4 v4.9.1 // indirect
	github.com/gorilla/securecookie v1.1.2 // indirect
	github.com/sergi/go-diff v1.4.0 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	golang.org/x/sys v0.28.0 // indirect
)
