package main

import (
	"embed"

	"github.com/lifthrasiir/angel/internal/server"
)

//go:embed frontend/dist
var embeddedFiles embed.FS

//go:embed frontend/login-unavailable.html
var loginUnavailableHTML []byte

//go:embed models.json
var modelsJSON []byte

func main() {
	server.Main("angel.db", embeddedFiles, loginUnavailableHTML, modelsJSON)
}
