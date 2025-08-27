package main

import (
	"encoding/json"
	"fmt" // Added for debugging
	"log"
	"os"
	"path/filepath"
	"strings"
)

// EnvChanged represents the structure for environment change messages.
type EnvChanged struct {
	Roots *RootsChanged `json:"roots,omitempty"`
}

// RootsChanged details the changes in session roots.
type RootsChanged struct {
	Value   []string      `json:"value"`
	Added   []RootAdded   `json:"added,omitempty"`
	Removed []RootRemoved `json:"removed,omitempty"`
	Prompts []RootPrompt  `json:"prompts,omitempty"`
}

type RootAdded struct {
	Path     string         `json:"path"`
	Contents []RootContents `json:"contents"`
}

type RootRemoved struct {
	Path string `json:"path"`
}

// RootContents represents a file or directory within a root.
type RootContents struct {
	Name     string         `json:"name"`
	Children []RootContents `json:"children"`
}

// MarshalJSON implements the json.Marshaler interface for RootContents.
func (rc RootContents) MarshalJSON() ([]byte, error) {
	if rc.Children == nil {
		// If no children, marshal as a string (just the Name)
		return json.Marshal(rc.Name)
	}
	// If children exist, marshal as an object
	type Alias RootContents // Create an alias to avoid infinite recursion
	return json.Marshal(&struct {
		Alias
	}{
		Alias: (Alias)(rc),
	})
}

// UnmarshalJSON implements the json.Unmarshaler interface for RootContents.
func (rc *RootContents) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as a string first (for Name only)
	var name string
	if err := json.Unmarshal(data, &name); err == nil {
		rc.Name = name
		rc.Children = nil // Ensure children are nil for string representation
		return nil
	}

	// If not a string, try to unmarshal as an object
	type Alias RootContents
	aux := &struct {
		*Alias
	}{
		Alias: (*Alias)(rc),
	}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	return nil
}

type RootPrompt struct {
	Path   string `json:"path"`
	Prompt string `json:"prompt"`
}

// ReadDirNFunc is a function type that encapsulates directory reading operations.
type ReadDirNFunc func(path string, n int) ([]string, error)

// osReadDirN implements ReadDirNFunc using os package functions.
func osReadDirN(path string, n int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	fileInfos, err := f.Readdir(n)
	if err != nil {
		return nil, err
	}

	entries := make([]string, len(fileInfos))
	for i, info := range fileInfos {
		name := info.Name()
		if info.IsDir() {
			name += "/"
		}
		entries[i] = name
	}
	return entries, nil
}

// calculateRootsChanged compares old and new roots and generates RootsChanged data.
func calculateRootsChanged(oldRoots, newRoots []string) (RootsChanged, error) {
	rootsChanged := RootsChanged{
		Value: newRoots,
	}

	oldMap := make(map[string]bool)
	for _, r := range oldRoots {
		oldMap[r] = true
	}

	newMap := make(map[string]bool)
	for _, r := range newRoots {
		newMap[r] = true
	}

	// Determine added roots
	for _, newRoot := range newRoots {
		if !oldMap[newRoot] {
			contents, err := getRootContents(osReadDirN, newRoot, 200) // Limit entries to 200
			if err != nil {
				log.Printf("calculateRootsChanged: Failed to get contents for added root %s: %v", newRoot, err)
				// Continue even if there's an error, don't block the whole process
			}
			rootsChanged.Added = append(rootsChanged.Added, RootAdded{Path: newRoot, Contents: contents})
		}
	}

	// Determine removed roots
	for _, oldRoot := range oldRoots {
		if !newMap[oldRoot] {
			rootsChanged.Removed = append(rootsChanged.Removed, RootRemoved{Path: oldRoot})
		}
	}

	// Determine prompts
	// If there are removed roots, search all current roots for prompts.
	// Otherwise, only search added roots for prompts.
	rootsToSearchForPrompts := newRoots
	if len(rootsChanged.Removed) == 0 {
		rootsToSearchForPrompts = []string{}
		for _, added := range rootsChanged.Added {
			rootsToSearchForPrompts = append(rootsToSearchForPrompts, added.Path)
		}
	}

	for _, rootPath := range rootsToSearchForPrompts {
		// Search for GEMINI.md within each rootPath
		geminiMDPath := filepath.Join(rootPath, "GEMINI.md")
		content, err := os.ReadFile(geminiMDPath)
		if err == nil {
			rootsChanged.Prompts = append(rootsChanged.Prompts, RootPrompt{Path: geminiMDPath, Prompt: string(content)})
		} else if !os.IsNotExist(err) {
			log.Printf("calculateRootsChanged: Failed to read GEMINI.md from %s: %v", geminiMDPath, err)
		}
	}

	return rootsChanged, nil
}

// getRootContents gets the contents of a directory using BFS, up to a certain limit.
func getRootContents(readDirNFunc ReadDirNFunc, rootPath string, maxEntries int) ([]RootContents, error) {
	// Map to store children for each directory path
	// Key: absolute path of the parent directory
	// Value: list of RootContents (children)
	processedChildren := make(map[string][]RootContents)

	// Queue for BFS
	queue := []string{rootPath}

	numEntries := 0 // Total entries recorded so far

	// BFS loop
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		// Calculate how many more entries we can read from this directory
		// We read one more than the remaining capacity to determine if "..." is needed.
		remainingCapacity := maxEntries - numEntries + 1

		// Read at most 'remainingCapacity' entries from the current directory
		names, err := readDirNFunc(current, remainingCapacity)
		if err != nil {
			log.Printf("getRootContents: Failed to read directory entries for %s: %v", current, err)
			processedChildren[current] = []RootContents{{Name: fmt.Sprintf("Error: %v", err)}}
			continue
		}

		currentDirEntries := []RootContents{}
		numEntriesInCurrentDir := 0 // Count of entries from current directory that will be added

		// Process entries from current directory
		// We only process up to maxEntries - numEntries entries,
		// the last one is just for checking if "..." is needed.
		processLimit := maxEntries - numEntries
		if processLimit < 0 {
			processLimit = 0
		}

		for i, name := range names {
			if i >= processLimit {
				break
			}

			fullPath := filepath.Join(current, name)
			rc := RootContents{Name: name}
			if strings.HasSuffix(name, "/") {
				rc.Children = []RootContents{} // Ensure Children is an empty slice, not nil
				queue = append(queue, fullPath)
			}
			currentDirEntries = append(currentDirEntries, rc)
			numEntriesInCurrentDir++
		}

		numEntries += numEntriesInCurrentDir // Update total entries

		// Add "..." if we read more entries than we processed,
		// meaning there are more entries in this directory than we could include.
		if len(names) > numEntriesInCurrentDir {
			currentDirEntries = append(currentDirEntries, RootContents{Name: "..."})
		}

		// Store processed children for current directory
		processedChildren[current] = currentDirEntries
	}

	// Recursive function to build the final tree from the map
	var buildTree func(parentPath string) []RootContents
	buildTree = func(parentPath string) []RootContents {
		var result []RootContents
		children, ok := processedChildren[parentPath]
		if !ok {
			return []RootContents{} // Return empty slice instead of nil
		}

		for _, rc := range children {
			newRc := rc
			if strings.HasSuffix(newRc.Name, "/") { // It's a directory
				fullPath := filepath.Join(parentPath, strings.TrimSuffix(newRc.Name, "/"))
				children := buildTree(fullPath)
				if children == nil {
					newRc.Children = []RootContents{}
				} else {
					newRc.Children = children
				}
			}
			result = append(result, newRc)
		}
		return result
	}

	// Build the tree starting from the rootPath
	finalTree := buildTree(rootPath)
	if finalTree == nil {
		return []RootContents{}, nil // Return empty slice instead of nil
	}
	return finalTree, nil
}
