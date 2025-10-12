package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"

	"github.com/fvbommel/sortorder"
)

// UILogicResultType indicates the outcome of the UI dialog operation.
type UILogicResultType string

const (
	UILogicResultTypeSuccess     UILogicResultType = "success"
	UILogicResultTypeCanceled    UILogicResultType = "canceled"
	UILogicResultTypeAlreadyOpen UILogicResultType = "already_open"
	UILogicResultTypeError       UILogicResultType = "error"
)

// DirectoryInfo represents a directory entry in the file system
type DirectoryInfo struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	IsParent bool   `json:"isParent"`
	IsRoot   bool   `json:"isRoot"`
}

// DirectoryNavigationResponse holds the directory listing information
type DirectoryNavigationResponse struct {
	CurrentPath string          `json:"currentPath"`
	Items       []DirectoryInfo `json:"items"`
	Error       string          `json:"error,omitempty"`
}

// PickDirectoryAPIResponse holds the path and the type of result for API response.
type PickDirectoryAPIResponse struct {
	SelectedPath string            `json:"selectedPath,omitempty"`
	Result       UILogicResultType `json:"result"`
	Error        string            `json:"error,omitempty"`
}

// getDirectoryList returns a list of directories in the specified path
func getDirectoryList(path string) ([]DirectoryInfo, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}

	var dirs []DirectoryInfo

	// Add parent directory entry (unless we're at root)
	parentPath := filepath.Dir(path)
	var shouldAddParent bool
	var parentDirPath string

	if parentPath != path {
		shouldAddParent = true
		parentDirPath = parentPath
	} else if runtime.GOOS == "windows" && len(path) == 3 && path[1] == ':' && path[2] == '\\' {
		// On Windows, if we're at a drive root (C:\, D:\), add parent to virtual root
		shouldAddParent = true
		parentDirPath = ""
	}

	if shouldAddParent {
		dirs = append(dirs, DirectoryInfo{
			Name:     "..",
			Path:     parentDirPath,
			IsParent: true,
			IsRoot:   parentDirPath == "",
		})
	}

	// Add subdirectories
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, DirectoryInfo{
				Name:     entry.Name(),
				Path:     filepath.Join(path, entry.Name()),
				IsParent: false,
				IsRoot:   false,
			})
		}
	}

	// Sort directories: parent first, then natural order
	sort.Slice(dirs, func(i, j int) bool {
		if dirs[i].IsParent {
			return true
		}
		if dirs[j].IsParent {
			return false
		}
		return sortorder.NaturalLess(dirs[i].Name, dirs[j].Name)
	})

	return dirs, nil
}

// handleDirectoryNavigation handles GET /api/ui/directory?path=<path>
func handleDirectoryNavigation(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")

	var dirs []DirectoryInfo
	var err error
	var resolvedPath string

	// On Windows, show drives when path is empty string
	if runtime.GOOS == "windows" && path == "" {
		dirs, err = getWindowsDrives()
		resolvedPath = ""
	} else {
		// For non-empty paths, resolve to absolute path
		if path == "" {
			path = "."
		}
		var absPath string
		absPath, err = filepath.Abs(path)
		if err != nil {
			absPath = filepath.Clean(path)
		}
		resolvedPath = absPath
		dirs, err = getDirectoryList(resolvedPath)
	}

	if err != nil {
		response := DirectoryNavigationResponse{
			CurrentPath: resolvedPath,
			Items:       []DirectoryInfo{},
			Error:       fmt.Sprintf("Failed to read directory: %v", err),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(response)
		return
	}

	response := DirectoryNavigationResponse{
		CurrentPath: resolvedPath,
		Items:       dirs,
		Error:       "",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handlePickDirectory handles the /api/ui/directory POST request.
// For web-based selection, this now expects the selected path in the request body.
func handlePickDirectory(w http.ResponseWriter, r *http.Request) {
	log.Println("Received /api/ui/directory request.")

	if r.Method == "GET" {
		// Legacy GET support - return navigation response instead
		cwd, err := os.Getwd()
		if err != nil {
			http.Error(w, "Failed to get current directory", http.StatusInternalServerError)
			return
		}

		dirs, err := getDirectoryList(cwd)
		if err != nil {
			response := DirectoryNavigationResponse{
				CurrentPath: cwd,
				Items:       []DirectoryInfo{},
				Error:       fmt.Sprintf("Failed to read directory: %v", err),
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(response)
			return
		}

		response := DirectoryNavigationResponse{
			CurrentPath: cwd,
			Items:       dirs,
			Error:       "",
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	// Handle POST request with selected path in body
	var request struct {
		SelectedPath string `json:"selectedPath"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		res := PickDirectoryAPIResponse{
			Result: UILogicResultTypeError,
			Error:  "Invalid request body",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(res)
		return
	}

	// Validate the selected path
	if request.SelectedPath == "" {
		res := PickDirectoryAPIResponse{
			Result: UILogicResultTypeError,
			Error:  "No path selected",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(res)
		return
	}

	// Check if the path exists and is a directory
	info, err := os.Stat(request.SelectedPath)
	if err != nil {
		res := PickDirectoryAPIResponse{
			Result: UILogicResultTypeError,
			Error:  fmt.Sprintf("Path does not exist: %v", err),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(res)
		return
	}

	if !info.IsDir() {
		res := PickDirectoryAPIResponse{
			Result: UILogicResultTypeError,
			Error:  "Selected path is not a directory",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(res)
		return
	}

	// Success
	res := PickDirectoryAPIResponse{
		SelectedPath: request.SelectedPath,
		Result:       UILogicResultTypeSuccess,
	}

	log.Printf("Directory selected: %s\n", request.SelectedPath)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(res)
}
