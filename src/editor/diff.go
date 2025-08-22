package editor

import (
	"bytes"
	"fmt"
	"unicode/utf8"

	"github.com/sergi/go-diff/diffmatchpatch"
)

// Diff generates a unified diff between two byte slices.
func Diff(src, dest []byte, contextLines int) string {
	// Empty files are assumed to have a trailing newline
	srcHadTrailingNewline := len(src) == 0 || src[len(src)-1] == '\n'
	destHadTrailingNewline := len(dest) == 0 || dest[len(dest)-1] == '\n'

	// Ensure both src and dest end with a newline for consistent diffing
	if !srcHadTrailingNewline {
		src = append(src, '\n')
	}
	if !destHadTrailingNewline {
		dest = append(dest, '\n')
	}

	dmp := diffmatchpatch.New()

	// Convert lines to runes for diffing
	a, b, lineArray := dmp.DiffLinesToRunes(string(src), string(dest))

	// Perform diff on runes
	diffs := dmp.DiffMainRunes(a, b, false) // false for line-level diff

	return generateUnifiedDiff(diffs, lineArray, contextLines)
}

// generateUnifiedDiff creates a unified diff from diffmatchpatch.Diffs.
// This implementation handles hunk headers and context lines.
// Assumes that diffs don't have consecutive DiffEqual entries.
func generateUnifiedDiff(diffs []diffmatchpatch.Diff, lineArray []string, contextLines int) string {
	const noChanges = "No changes"

	if len(diffs) == 0 {
		// No differences without contents.
		return noChanges
	}

	var buf bytes.Buffer

	// Temporary storage for context lines before a change
	var pendingDiffs []diffmatchpatch.Diff

	// The starting line numbers of the current hunk, 1-based
	hunkOldStart := 1
	hunkNewStart := 1

	// Cumulative number of lines in the current hunk
	hunkOldLines := 0
	hunkNewLines := 0

	writeHunk := func() {
		buf.WriteString(fmt.Sprintf("@@ -%d,%d +%d,%d @@\n", hunkOldStart, hunkOldLines, hunkNewStart, hunkNewLines))
		for _, diff := range pendingDiffs {
			var mark rune
			switch diff.Type {
			case diffmatchpatch.DiffDelete:
				mark = '-'
			case diffmatchpatch.DiffInsert:
				mark = '+'
			case diffmatchpatch.DiffEqual:
				mark = ' '
			}
			for _, r := range diff.Text {
				buf.WriteString(fmt.Sprintf("%c%s", mark, lineArray[runeToInt(r)]))
			}
		}
	}

	if len(diffs) == 1 {
		// No differences with contents. Handling this as an edge case simplifies the subsequent logic.
		switch diffs[0].Type {
		case diffmatchpatch.DiffEqual:
			return noChanges
		case diffmatchpatch.DiffDelete:
			hunkNewStart = 0
			hunkOldLines = utf8.RuneCountInString(diffs[0].Text)
			pendingDiffs = diffs
			writeHunk()
		case diffmatchpatch.DiffInsert:
			hunkOldStart = 0
			hunkNewLines = utf8.RuneCountInString(diffs[0].Text)
			pendingDiffs = diffs
			writeHunk()
		}
		return buf.String()
	}

	for _, diff := range diffs {
		diffLines := utf8.RuneCountInString(diff.Text)

		switch diff.Type {
		case diffmatchpatch.DiffEqual:
			if len(pendingDiffs) == 0 {
				// This starts the first hunk. This can't be last because the edge case has been already handled.
				hunkStart0 := max(0, diffLines-contextLines)
				hunkOldStart = hunkStart0 + 1
				hunkNewStart = hunkStart0 + 1
				hunkOldLines = diffLines - hunkStart0
				hunkNewLines = diffLines - hunkStart0
				pendingDiffs = []diffmatchpatch.Diff{{
					Type: diffmatchpatch.DiffEqual,
					Text: string([]rune(diff.Text)[hunkStart0:]),
				}}
			} else if diffLines > contextLines*2 {
				// This diff is on the boundary of two hunks.
				pendingDiffs = append(pendingDiffs, diffmatchpatch.Diff{
					Type: diffmatchpatch.DiffEqual,
					Text: string([]rune(diff.Text)[:contextLines]),
				})
				hunkOldLines += contextLines
				hunkNewLines += contextLines
				writeHunk()

				hunkOldStart += hunkOldLines + (diffLines - contextLines)
				hunkNewStart += hunkNewLines + (diffLines - contextLines)
				hunkOldLines = contextLines
				hunkNewLines = contextLines
				pendingDiffs = []diffmatchpatch.Diff{{
					Type: diffmatchpatch.DiffEqual,
					Text: string([]rune(diff.Text)[diffLines-contextLines:]),
				}}
			} else {
				// This diff is in the middle of larger hunk.
				hunkOldLines += diffLines
				hunkNewLines += diffLines
				pendingDiffs = append(pendingDiffs, diff)
			}
		case diffmatchpatch.DiffDelete:
			hunkOldLines += diffLines
			pendingDiffs = append(pendingDiffs, diff)
		case diffmatchpatch.DiffInsert:
			hunkNewLines += diffLines
			pendingDiffs = append(pendingDiffs, diff)
		}
	}

	// Finalize the last hunk if any. The hunk with a single DiffEqual at the end is ignored.
	if len(pendingDiffs) > 1 || (len(pendingDiffs) == 1 && pendingDiffs[0].Type != diffmatchpatch.DiffEqual) {
		writeHunk()
	}

	return buf.String()
}

// Copied from diffmatchpatch.runeToInt
func runeToInt(r rune) uint32 {
	i := uint32(r)
	if i < (1 << diffmatchpatch.ONE_BYTE_BITS) {
		return i
	}

	bytes := []byte{0, 0, 0, 0}

	size := utf8.EncodeRune(bytes, r)

	if size == 2 {
		return uint32(bytes[0]&0b11111)<<6 | uint32(bytes[1]&0b111111)
	}

	if size == 3 {
		result := uint32(bytes[0]&0b1111)<<12 | uint32(bytes[1]&0b111111)<<6 | uint32(bytes[2]&0b111111)
		if result >= diffmatchpatch.UNICODE_INVALID_RANGE_END {
			return result - diffmatchpatch.UNICODE_INVALID_RANGE_DELTA
		}

		return result
	}

	if size == 4 {
		result := uint32(bytes[0]&0b111)<<18 | uint32(bytes[1]&0b111111)<<12 | uint32(bytes[2]&0b111111)<<6 | uint32(bytes[3]&0b111111)
		return result - diffmatchpatch.UNICODE_INVALID_RANGE_DELTA - 3
	}

	panic(fmt.Sprintf("Unexpected state decoding rune=%v size=%d", r, size))
}
