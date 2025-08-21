package editor

import (
	"testing"
)

func TestDiff(t *testing.T) {
	tests := []struct {
		name         string
		old          []byte
		new          []byte
		path         string
		expected     string
		contextLines int
	}{
		{
			name:         "No change",
			old:          []byte("line1\nline2\nline3\n"),
			new:          []byte("line1\nline2\nline3\n"),
			path:         "test.txt",
			expected:     "--- a/test.txt\n+++ b/test.txt\n", // No hunks for no change
			contextLines: 1,
		},
		{
			name:         "Add line at end",
			old:          []byte("line1\nline2\n"),
			new:          []byte("line1\nline2\nline3\n"),
			path:         "test.txt",
			expected:     "--- a/test.txt\n+++ b/test.txt\n@@ -2,1 +2,2 @@\n line2\n+line3\n",
			contextLines: 1,
		},
		{
			name:         "Delete line at end",
			old:          []byte("line1\nline2\nline3\n"),
			new:          []byte("line1\nline2\n"),
			path:         "test.txt",
			expected:     "--- a/test.txt\n+++ b/test.txt\n@@ -2,2 +2,1 @@\n line2\n-line3\n",
			contextLines: 1,
		},
		{
			name:         "Change line",
			old:          []byte("line1\noldline2\nline3\n"),
			new:          []byte("line1\nnewline2\nline3\n"),
			path:         "test.txt",
			expected:     "--- a/test.txt\n+++ b/test.txt\n@@ -1,3 +1,3 @@\n line1\n-oldline2\n+newline2\n line3\n",
			contextLines: 1,
		},
		{
			name:         "Add multiple lines in middle",
			old:          []byte("line1\nline2\nline5\n"),
			new:          []byte("line1\nline2\nline3\nline4\nline5\n"),
			path:         "test.txt",
			expected:     "--- a/test.txt\n+++ b/test.txt\n@@ -2,2 +2,4 @@\n line2\n+line3\n+line4\n line5\n",
			contextLines: 1,
		},
		{
			name:         "Delete multiple lines in middle",
			old:          []byte("line1\nline2\nline3\nline4\nline5\n"),
			new:          []byte("line1\nline2\nline5\n"),
			path:         "test.txt",
			expected:     "--- a/test.txt\n+++ b/test.txt\n@@ -2,4 +2,2 @@\n line2\n-line3\n-line4\n line5\n",
			contextLines: 1,
		},
		{
			name:         "Empty old file, new content",
			old:          []byte(""),
			new:          []byte("line1\nline2\n"),
			path:         "empty.txt",
			expected:     "--- a/empty.txt\n+++ b/empty.txt\n@@ -0,0 +1,2 @@\n+line1\n+line2\n",
			contextLines: 1,
		},
		{
			name:         "Content in old file, empty new",
			old:          []byte("line1\nline2\n"),
			new:          []byte(""),
			path:         "empty.txt",
			expected:     "--- a/empty.txt\n+++ b/empty.txt\n@@ -1,2 +0,0 @@\n-line1\n-line2\n",
			contextLines: 1,
		},
		{
			name:         "Multiple diffs in a single hunk",
			old:          []byte("a\nb\nc\nd\ne\nf\ng\nh\ni\nj\n"),
			new:          []byte("a\nb\nX\nd\ne\nY\ng\nh\ni\nj\n"),
			path:         "multi.txt",
			expected:     "--- a/multi.txt\n+++ b/multi.txt\n@@ -2,6 +2,6 @@\n b\n-c\n+X\n d\n e\n-f\n+Y\n g\n",
			contextLines: 1,
		},
		{
			name:         "Multiple hunks",
			old:          []byte("a\nb\nc\nd\ne\nf\ng\nh\ni\nj\n"),
			new:          []byte("a\nb\nX\nd\ne\nf\nY\nh\ni\nj\n"),
			path:         "multi.txt",
			expected:     "--- a/multi.txt\n+++ b/multi.txt\n@@ -2,3 +2,3 @@\n b\n-c\n+X\n d\n@@ -7,3 +7,3 @@\n f\n-g\n+Y\n h\n",
			contextLines: 1,
		},
		{
			name:         "End of file, insufficient context",
			old:          []byte("line1\nline2\nline3\n"),
			new:          []byte("line1\nline2\nline3_changed\n"),
			path:         "end_context.txt",
			expected:     "--- a/end_context.txt\n+++ b/end_context.txt\n@@ -1,3 +1,3 @@\n line1\n line2\n-line3\n+line3_changed\n",
			contextLines: 3, // Context lines equal to total lines
		},
		{
			name:         "Start of file, insufficient context",
			old:          []byte("line1\nline2\nline3\n"),
			new:          []byte("line1_changed\nline2\nline3\n"),
			path:         "start_context.txt",
			expected:     "--- a/start_context.txt\n+++ b/start_context.txt\n@@ -1,3 +1,3 @@\n-line1\n+line1_changed\n line2\n line3\n",
			contextLines: 3, // Context lines equal to total lines
		},
		{
			name:         "Empty to empty file",
			old:          []byte(""),
			new:          []byte(""),
			path:         "empty_to_empty.txt",
			expected:     "--- a/empty_to_empty.txt\n+++ b/empty_to_empty.txt\n",
			contextLines: 1,
		},
		/* Currently missing newlines at the end of file are normalized, but should be indicated in the future.
		{
			name:         "No change, no trailing newline",
			old:          []byte("line1\nline2"),
			new:          []byte("line1\nline2"),
			path:         "no_newline_no_change.txt",
			expected:     "--- a/no_newline_no_change.txt\n+++ b/no_newline_no_change.txt\n",
			contextLines: 1,
		},
		{
			name:         "Add trailing newline",
			old:          []byte("line1\nline2"),
			new:          []byte("line1\nline2\n"),
			path:         "add_newline.txt",
			expected:     "--- a/add_newline.txt\n+++ b/add_newline.txt\n@@ -1,2 +1,3 @@\n line1\n line2\n+\n",
			contextLines: 1,
		},
		{
			name:         "Remove trailing newline",
			old:          []byte("line1\nline2\n"),
			new:          []byte("line1\nline2"),
			path:         "remove_newline.txt",
			expected:     "--- a/remove_newline.txt\n+++ b/remove_newline.txt\n@@ -1,3 +1,2 @@\n line1\n line2\n-\n\\ No newline at end of file\n",
			contextLines: 1,
		},
		{
			name:         "Change last line, both without trailing newline",
			old:          []byte("line1\nold_last_line"),
			new:          []byte("line1\nnew_last_line"),
			path:         "change_last_no_newline.txt",
			expected:     "--- a/change_last_no_newline.txt\n+++ b/change_last_no_newline.txt\n@@ -1,2 +1,2 @@\n line1\n-old_last_line\n+new_last_line\n\\ No newline at end of file\n",
			contextLines: 1,
		},
		*/
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			act := Diff(tt.old, tt.new, tt.path, tt.contextLines) // Use tt.contextLines
			if act != tt.expected {
				t.Errorf("Test %s failed:\nExpected:\n%sActual:\n%s", tt.name, tt.expected, act)
			}
		})
	}
}
