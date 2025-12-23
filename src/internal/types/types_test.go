package types

import (
	"testing"
)

func TestSessionId(t *testing.T) {
	tests := []struct {
		input          string
		expectedMain   string
		expectedSuffix string
	}{
		{"", "", ""},
		{"session123", "session123", ""},
		{".temp456", ".temp456", ""},
		{"session789.suffix", "session789", ".suffix"},
		{".temp123.suffix", ".temp123", ".suffix"},
		{"session123.subsession456.suffix", "session123", ".subsession456.suffix"},
	}

	for _, test := range tests {
		main, suffix := SplitSessionId(test.input)
		if main != test.expectedMain || suffix != test.expectedSuffix {
			t.Errorf("SplitSessionId(%q) = (%q, %q); want (%q, %q)",
				test.input, main, suffix, test.expectedMain, test.expectedSuffix)
		}

		if IsSubsessionId(test.input) != (test.expectedSuffix != "") {
			t.Errorf("IsSubsessionId(%q) = %v; want %v",
				test.input, IsSubsessionId(test.input), test.expectedSuffix != "")
		}
	}
}
