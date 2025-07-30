package main

import (
	"archive/zip"
	"bytes"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// embeddedZipReader will hold the zip.Reader for the embedded frontend
var embeddedZipReader *zip.Reader
var exeFile *os.File // Make exeFile a global variable

// initEmbeddedZip initializes the embedded zip reader
func initEmbeddedZip() {
	exePath, err := os.Executable()
	if err != nil {
		log.Fatalf("Failed to get executable path: %v", err)
	}

	exeFile, err := os.Open(exePath)
	if err != nil {
		log.Fatalf("Failed to open executable: %v", err)
	}

	exeInfo, err := exeFile.Stat()
	if err != nil {
		log.Fatalf("Failed to stat executable: %v", err)
	}
	exeSize := exeInfo.Size()

	// Create a new zip reader
	reader, err := zip.NewReader(exeFile, exeSize)
	if err != nil {
		log.Fatalf("Failed to create zip reader from embedded data: %v", err)
	}
	embeddedZipReader = reader
	log.Println("Embedded frontend zip initialized.")
}

// serveStaticFiles serves static files from frontend/dist or embedded zip
func serveStaticFiles(w http.ResponseWriter, r *http.Request) {
	// 1. Try to serve from frontend/dist for development
	frontendPath := filepath.Join(".", "frontend", "dist")
	fs := http.FileServer(http.Dir(frontendPath))

	// Check if the file exists on disk
	localFilePath := filepath.Join(frontendPath, r.URL.Path)
	if _, err := os.Stat(localFilePath); err == nil {
		// File exists on disk, serve it
		fs.ServeHTTP(w, r)
		return
	} else if !os.IsNotExist(err) {
		// Some other error occurred when checking file existence

		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// 2. If not found on disk, try to serve from embedded zip
	if embeddedZipReader != nil {
		// Normalize the path for zip file lookup (remove leading slash, if any)
		zipPath := strings.TrimPrefix(r.URL.Path, "/")
		if zipPath == "" || zipPath == "index.html" { // Handle root and explicit index.html
			zipPath = "index.html"
		} else {
			// Ensure paths are relative to the dist directory within the zip
			// e.g., /assets/foo.js should map to assets/foo.js in zip
			// The zip is created from frontend/dist, so paths are relative to dist.
			// If the request is for /foo.png, it should look for foo.png in the zip.
			// If the request is for /assets/bar.css, it should look for assets/bar.css in the zip.
			// So, just use the trimmed path directly.
		}

		// Find the file in the embedded zip
		for _, file := range embeddedZipReader.File {
			if file.Name == zipPath {
				rc, err := file.Open()
				if err != nil {
					log.Printf("Failed to open embedded zip file %s: %v", file.Name, err)
					http.Error(w, "Internal Server Error", http.StatusInternalServerError)
					return
				}
				defer rc.Close()

				// Read content into a buffer to satisfy io.ReadSeeker
				content, err := io.ReadAll(rc)
				if err != nil {
					log.Printf("Failed to read embedded zip file content %s: %v", file.Name, err)
					http.Error(w, "Internal Server Error", http.StatusInternalServerError)
					return
				}
				contentReader := bytes.NewReader(content)

				// Set Content-Type header
				w.Header().Set("Content-Type", getContentType(file.Name))
				http.ServeContent(w, r, file.Name, file.ModTime(), contentReader)
				return
			}
		}
	}

	// 3. If not found anywhere, serve index.html for client-side routing (SPA fallback)
	// This should only happen if the requested path is not a static asset and not found in embedded zip.
	// For development, it will serve from disk. For production, it will serve from embedded zip.
	// We need to handle this for both cases.
	// If the request is for a path like /some/route, and it's not a file, serve index.html.
	// This is the SPA fallback.

	// Try to serve index.html from disk first
	if _, err := os.Stat(filepath.Join(frontendPath, "index.html")); err == nil {
		http.ServeFile(w, r, filepath.Join(frontendPath, "index.html"))
		return
	}

	// If not found on disk, try to serve index.html from embedded zip
	if embeddedZipReader != nil {
		for _, file := range embeddedZipReader.File {
			if file.Name == "index.html" {
				rc, err := file.Open()
				if err != nil {
					log.Printf("Failed to open embedded index.html: %v", err)
					http.Error(w, "Internal Server Error", http.StatusInternalServerError)
					return
				}
				defer rc.Close()

				// Read content into a buffer to satisfy io.ReadSeeker
				content, err := io.ReadAll(rc)
				if err != nil {
					log.Printf("Failed to read embedded index.html content: %v", err)
					http.Error(w, "Internal Server Error", http.StatusInternalServerError)
					return
				}
				contentReader := bytes.NewReader(content)

				w.Header().Set("Content-Type", "text/html")
				http.ServeContent(w, r, file.Name, file.ModTime(), contentReader)
				return
			}
		}
	}

	// If index.html is not found anywhere, return 404
	http.NotFound(w, r)
}

// getContentType returns the Content-Type based on file extension
func getContentType(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".html":
		return "text/html"
	case ".css":
		return "text/css"
	case ".js":
		return "application/javascript"
	case ".json":
		return "application/json"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".svg":
		return "image/svg+xml"
	case ".ico":
		return "image/x-icon"
	case ".woff2":
		return "font/woff2"
	case ".woff":
		return "font/woff"
	case ".ttf":
		return "font/ttf"
	case ".eot":
		return "application/vnd.ms-fontobject"
	default:
		return "application/octet-stream"
	}
}
