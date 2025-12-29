package chat

import (
	"math"
	"strconv"
	"testing"
)

func TestGenerateCopySessionName(t *testing.T) {
	maxInt := strconv.Itoa(math.MaxInt)

	tests := []struct {
		name       string
		copiedName string
	}{
		{name: "", copiedName: "New Chat (Copy)"},
		{name: "(Copy)", copiedName: "(Copy) (Copy)"},
		{name: " (Copy)", copiedName: " (Copy 2)"},
		{name: " (Copy 2)", copiedName: " (Copy 3)"},
		{name: "Some session", copiedName: "Some session (Copy)"},
		{name: "Some session (Copy)", copiedName: "Some session (Copy 2)"},
		{name: "Some session (Copy 0)", copiedName: "Some session (Copy)"},
		{name: "Some session (Copy 1)", copiedName: "Some session (Copy 2)"},
		{name: "Some session (Copy 2)", copiedName: "Some session (Copy 3)"},
		{name: "Some session (Copy 9)", copiedName: "Some session (Copy 10)"},
		{name: "Some session (Copy " + maxInt + ")", copiedName: "Some session (Copy " + maxInt + ") (Copy)"},
		{name: "Some session (Copy " + maxInt + maxInt + ")", copiedName: "Some session (Copy " + maxInt + maxInt + ") (Copy)"},
		{name: "Another session(copy)", copiedName: "Another session (Copy 2)"},
		{name: "Another session\t(COPY\u30007)\r\n", copiedName: "Another session (Copy 8)"},
		{name: "Yet another session ( Copy )", copiedName: "Yet another session ( Copy ) (Copy)"},
		{name: "Yet another session (Copy", copiedName: "Yet another session (Copy (Copy)"},
	}

	for _, tt := range tests {
		actual := generateCopySessionName(tt.name)
		if actual != tt.copiedName {
			t.Errorf("generateCopySessionName(%q) = %q; want %q", tt.name, actual, tt.copiedName)
		}
	}
}
