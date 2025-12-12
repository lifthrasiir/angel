package env

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

// mockReadDirNFunc is a mock implementation of ReadDirNFunc for testing.
type mockReadDirNFunc struct {
	mockData map[string][]string
	mockErr  map[string]error
}

func (m *mockReadDirNFunc) ReadDirN(path string, n int) ([]string, error) {
	// Normalize path for Windows compatibility in tests
	normalizedPath := filepath.ToSlash(path) // Convert to forward slashes for map lookup

	if err, ok := m.mockErr[normalizedPath]; ok {
		return nil, err
	}
	if data, ok := m.mockData[normalizedPath]; ok {
		if n <= 0 || n >= len(data) {
			return data, nil
		}
		return data[:n], nil
	}
	return nil, errors.New("path not found in mock data: " + path)
}

func TestGetRootContents(t *testing.T) {
	tests := []struct {
		name         string
		rootPath     string
		maxEntries   int
		mockReadDirN *mockReadDirNFunc
		want         []RootContents
		wantErr      bool
	}{
		{
			name:       "Empty directory",
			rootPath:   "root",
			maxEntries: 10,
			mockReadDirN: &mockReadDirNFunc{
				mockData: map[string][]string{
					"root": {},
				},
			},
			want:    []RootContents{},
			wantErr: false,
		},
		{
			name:       "Single file",
			rootPath:   "root",
			maxEntries: 10,
			mockReadDirN: &mockReadDirNFunc{
				mockData: map[string][]string{
					"root": {"file.txt"},
				},
			},
			want:    []RootContents{{Name: "file.txt"}},
			wantErr: false,
		},
		{
			name:       "Single directory",
			rootPath:   "root",
			maxEntries: 10,
			mockReadDirN: &mockReadDirNFunc{
				mockData: map[string][]string{
					"root":     {"dir/"},
					"root/dir": {},
				},
			},
			want:    []RootContents{{Name: "dir/", Children: []RootContents{}}},
			wantErr: false,
		},
		{
			name:       "Mixed content",
			rootPath:   "root",
			maxEntries: 10,
			mockReadDirN: &mockReadDirNFunc{
				mockData: map[string][]string{
					"root":      {"file1.txt", "dir1/", "file2.txt"},
					"root/dir1": {"nested_file.txt"},
				},
			},
			want: []RootContents{
				{Name: "file1.txt"},
				{Name: "dir1/", Children: []RootContents{{Name: "nested_file.txt"}}},
				{Name: "file2.txt"},
			},
			wantErr: false,
		},
		{
			name:       "Max entries limit - exact",
			rootPath:   "root",
			maxEntries: 3,
			mockReadDirN: &mockReadDirNFunc{
				mockData: map[string][]string{
					"root": {"file1.txt", "file2.txt", "file3.txt", "file4.txt"},
				},
			},
			want: []RootContents{
				{Name: "file1.txt"},
				{Name: "file2.txt"},
				{Name: "file3.txt"},
				{Name: "..."},
			},
			wantErr: false,
		},
		{
			name:       "Max entries limit - with directory",
			rootPath:   "root",
			maxEntries: 3,
			mockReadDirN: &mockReadDirNFunc{
				mockData: map[string][]string{
					"root":      {"file1.txt", "dir1/", "file2.txt", "file3.txt"},
					"root/dir1": {"nested_file.txt"},
				},
			},
			want: []RootContents{
				{Name: "file1.txt"},
				{Name: "dir1/", Children: []RootContents{{Name: "..."}}},
				{Name: "file2.txt"},
				{Name: "..."},
			},
			wantErr: false,
		},
		{
			name:       "Error reading directory",
			rootPath:   "root",
			maxEntries: 10,
			mockReadDirN: &mockReadDirNFunc{
				mockErr: map[string]error{
					"root": errors.New("permission denied"),
				},
			},
			want:    []RootContents{{Name: "Error: permission denied"}},
			wantErr: false,
		},
		{
			name:       "Nested directories with limit",
			rootPath:   "root",
			maxEntries: 5,
			mockReadDirN: &mockReadDirNFunc{
				mockData: map[string][]string{
					"root":      {"dirA/", "dirB/", "file1.txt"},
					"root/dirA": {"fileA1.txt", "fileA2.txt", "fileA3.txt"},
					"root/dirB": {"fileB1.txt"},
				},
			},
			want: []RootContents{
				{Name: "dirA/", Children: []RootContents{
					{Name: "fileA1.txt"},
					{Name: "fileA2.txt"},
					{Name: "..."},
				}},
				{Name: "dirB/", Children: []RootContents{
					{Name: "..."},
				}},
				{Name: "file1.txt"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := getRootContents(tt.mockReadDirN.ReadDirN, tt.rootPath, tt.maxEntries)
			if (err != nil) != tt.wantErr {
				t.Errorf("getRootContents() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// The getRootContents function returns the children of the rootPath, not the rootPath itself.
			// So, the 'want' values in the test cases should directly represent the expected children.
			// The comparison logic here is simplified as 'want' now directly matches 'got'.
			if (err != nil) != tt.wantErr {
				t.Errorf("getRootContents() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// The getRootContents function returns the children of the rootPath, not the rootPath itself.
			// So, the 'want' values in the test cases should directly represent the expected children.
			// The comparison logic here is simplified as 'want' now directly matches 'got'.
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("getRootContents() got = %#v, want %#v", got, tt.want)
			}
		})
	}
}
