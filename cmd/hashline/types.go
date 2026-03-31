package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ────────────────────────────────────────────────────────────────────────────
// Anchor
// ────────────────────────────────────────────────────────────────────────────

// Anchor is a validated line reference: a 1-indexed line number paired with
// the expected 2-character hash computed by computeLineHash.
type Anchor struct {
	Line int
	Hash string
}

// anchorRE matches "LINE#HASH" with optional surrounding whitespace and
// tolerates the display suffix (":..." or "  ...") that may follow if the
// model copy-pastes a full annotated line instead of just the tag.
var anchorRE = regexp.MustCompile(`^\s*(\d+)\s*#\s*([ZPMQVRWSNKTXJBYH]{2})`)

// parseAnchor parses a string like "5#WS" into an Anchor.
// It is lenient: it strips the display suffix if present.
func parseAnchor(s string) (Anchor, error) {
	m := anchorRE.FindStringSubmatch(s)
	if m == nil {
		return Anchor{}, fmt.Errorf("invalid anchor %q: expected LINE#HASH (e.g. \"5#WS\")", s)
	}
	line, _ := strconv.Atoi(m[1])
	if line < 1 {
		return Anchor{}, fmt.Errorf("anchor line number must be >= 1, got %d in %q", line, s)
	}
	return Anchor{Line: line, Hash: m[2]}, nil
}

// ────────────────────────────────────────────────────────────────────────────
// Edit operations
// ────────────────────────────────────────────────────────────────────────────

// EditOp discriminates the operation type in the JSON input.
type EditOp string

const (
	OpReplaceLine  EditOp = "replace_line"
	OpReplaceRange EditOp = "replace_range"
	OpAppendAt     EditOp = "append_at"
	OpPrependAt    EditOp = "prepend_at"
	OpAppendFile   EditOp = "append_file"
	OpPrependFile  EditOp = "prepend_file"
)

// RawEdit is the JSON-decoded form of a single edit operation before anchor
// parsing. We use explicit string fields so we can produce clear error messages
// when anchors are invalid.
type RawEdit struct {
	Op    EditOp   `json:"op"`
	Pos   string   `json:"pos"`   // anchor for replace_line / replace_range / append_at / prepend_at
	End   string   `json:"end"`   // anchor for replace_range (end-inclusive)
	Lines []string `json:"lines"` // replacement / inserted lines (nil == delete)
}

// Edit is a fully validated edit operation with parsed anchors.
type Edit struct {
	Op    EditOp
	Pos   Anchor   // zero value if not applicable
	End   Anchor   // zero value if not applicable
	Lines []string // nil means delete
}

// EditRequest is the top-level JSON document accepted on stdin.
type EditRequest struct {
	Edits []RawEdit `json:"edits"`
}

// ────────────────────────────────────────────────────────────────────────────
// Result types (JSON output)
// ────────────────────────────────────────────────────────────────────────────

// EditResult is written to stdout after a successful edit.
type EditResult struct {
	OK               bool     `json:"ok"`
	FirstChangedLine int      `json:"firstChangedLine,omitempty"`
	Warnings         []string `json:"warnings,omitempty"`
}

// EditError is written to stdout when validation fails.
type EditError struct {
	OK      bool              `json:"ok"`
	Error   string            `json:"error"` // "mismatch" | "invalid" | "io"
	Message string            `json:"message"`
	Remaps  map[string]string `json:"remaps,omitempty"` // stale → current
}

func writeJSON(v any) error {
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// ────────────────────────────────────────────────────────────────────────────
// Hash-mismatch error
// ────────────────────────────────────────────────────────────────────────────

const mismatchContext = 2 // lines of context shown around each mismatch

// mismatch records one stale anchor.
type mismatch struct {
	line     int
	expected string
	actual   string
}

// buildMismatchError constructs the JSON error for one or more stale anchors.
// It displays grep-style output with ">>>" markers on changed lines, surrounded
// by context, so the model can quickly update its anchors.
func buildMismatchError(mismatches []mismatch, fileLines []string) EditError {
	remaps := map[string]string{}
	mismatchSet := map[int]mismatch{}
	for _, m := range mismatches {
		mismatchSet[m.line] = m
		remaps[intToStr(m.line)+"#"+m.expected] = intToStr(m.line) + "#" + m.actual
	}

	// Collect context lines
	show := map[int]bool{}
	for _, m := range mismatches {
		lo := m.line - mismatchContext
		hi := m.line + mismatchContext
		if lo < 1 {
			lo = 1
		}
		if hi > len(fileLines) {
			hi = len(fileLines)
		}
		for i := lo; i <= hi; i++ {
			show[i] = true
		}
	}

	// Sort displayed lines
	sorted := sortedKeys(show)

	var sb strings.Builder
	n := len(mismatches)
	if n == 1 {
		sb.WriteString("1 line has changed since last read.")
	} else {
		sb.WriteString(intToStr(n))
		sb.WriteString(" lines have changed since last read.")
	}
	sb.WriteString(" Use the updated LINE#HASH references shown below (>>> marks changed lines).\n\n")

	prev := -1
	for _, lineNum := range sorted {
		if prev != -1 && lineNum > prev+1 {
			sb.WriteString("    ...\n")
		}
		prev = lineNum
		content := fileLines[lineNum-1]
		hash := computeLineHash(lineNum, content)
		tag := intToStr(lineNum) + "#" + hash
		if _, isMismatch := mismatchSet[lineNum]; isMismatch {
			sb.WriteString(">>> ")
		} else {
			sb.WriteString("    ")
		}
		sb.WriteString(tag)
		sb.WriteString(":")
		sb.WriteString(content)
		sb.WriteString("\n")
	}

	return EditError{
		OK:      false,
		Error:   "mismatch",
		Message: sb.String(),
		Remaps:  remaps,
	}
}

// sortedKeys returns the keys of an int→bool map in ascending order.
func sortedKeys(m map[int]bool) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// insertion sort — small slices only
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}
