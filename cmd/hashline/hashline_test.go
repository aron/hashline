package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// ────────────────────────────────────────────────────────────────────────────
// Hash stability
// ────────────────────────────────────────────────────────────────────────────

func TestHashStability(t *testing.T) {
	// Same line always produces the same 2-char tag.
	for i := 0; i < 10; i++ {
		h1 := computeLineHash(1, "func main() {")
		h2 := computeLineHash(1, "func main() {")
		if h1 != h2 {
			t.Errorf("non-deterministic hash: %q vs %q", h1, h2)
		}
		if len(h1) != 2 {
			t.Errorf("expected 2-char hash, got %q (len %d)", h1, len(h1))
		}
	}
}

func TestHashAlphabetOnly(t *testing.T) {
	// Hash characters must be from nibbleAlpha only.
	lines := []string{"", "  ", "{", "}", "// comment", "func foo() {", "\t\tif err != nil {"}
	for i, line := range lines {
		h := computeLineHash(i+1, line)
		for _, c := range h {
			if !strings.ContainsRune(nibbleAlpha, c) {
				t.Errorf("hash %q for line %q contains character %q not in nibbleAlpha", h, line, c)
			}
		}
	}
}

func TestHashSeeding(t *testing.T) {
	// Blank lines at different 1-indexed positions must get different hashes
	// (the non-alpha seeding prevents all blank lines from colliding).
	seen := map[string]int{}
	for i := 1; i <= 20; i++ {
		h := computeLineHash(i, "")
		if prev, ok := seen[h]; ok {
			// Some collisions are expected with 256 values — just ensure not ALL the same.
			_ = prev
		}
		seen[h] = i
	}
	if len(seen) < 5 {
		t.Errorf("too many blank-line hash collisions: only %d distinct hashes for 20 blank lines", len(seen))
	}
}

func TestHashTrailingWhitespace(t *testing.T) {
	// Trailing whitespace is stripped before hashing.
	h1 := computeLineHash(1, "hello")
	h2 := computeLineHash(1, "hello   ")
	h3 := computeLineHash(1, "hello\r")
	if h1 != h2 {
		t.Errorf("trailing space changed hash: %q vs %q", h1, h2)
	}
	if h1 != h3 {
		t.Errorf("trailing CR changed hash: %q vs %q", h1, h3)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Anchor parsing
// ────────────────────────────────────────────────────────────────────────────

func TestParseAnchor(t *testing.T) {
	cases := []struct {
		in       string
		wantLine int
		wantHash string
		wantErr  bool
	}{
		{"5#WS", 5, "WS", false},
		{"1#ZP", 1, "ZP", false},
		{"100#NB", 100, "NB", false},
		// Tolerates display suffix
		{"5#WS:func main() {", 5, "WS", false},
		// Bad inputs
		{"abc", 0, "", true},
		{"5:WS", 0, "", true}, // colon instead of hash
		{"0#ZP", 0, "", true}, // line 0 is invalid
	}

	for _, tc := range cases {
		a, err := parseAnchor(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseAnchor(%q): expected error, got %+v", tc.in, a)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseAnchor(%q): unexpected error: %v", tc.in, err)
			continue
		}
		if a.Line != tc.wantLine || a.Hash != tc.wantHash {
			t.Errorf("parseAnchor(%q) = {%d, %q}, want {%d, %q}",
				tc.in, a.Line, a.Hash, tc.wantLine, tc.wantHash)
		}
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Read output format
// ────────────────────────────────────────────────────────────────────────────

func TestCmdReadFormat(t *testing.T) {
	// Create a temp file
	content := "line one\nline two\nline three\n"
	f := writeTempFile(t, content)

	out := captureRead(t, f, nil)

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %v", len(lines), lines)
	}

	for i, line := range lines {
		// Each line must match "N#HH:content"
		lineNum := i + 1
		sepIdx := strings.Index(line, ":")
		if sepIdx < 0 {
			t.Errorf("line %d missing ':' separator: %q", lineNum, line)
			continue
		}
		tag := line[:sepIdx]
		if !strings.Contains(tag, "#") {
			t.Errorf("line %d tag missing '#': %q", lineNum, tag)
		}
	}
}

func TestCmdReadOffset(t *testing.T) {
	content := "a\nb\nc\nd\ne\n"
	f := writeTempFile(t, content)

	out := captureRead(t, f, []string{"--offset", "3"})

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	// Expect lines 3,4,5
	if len(lines) < 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	// First line should be line 3
	if !strings.HasPrefix(lines[0], "3#") {
		t.Errorf("first line should start with '3#', got %q", lines[0])
	}
}

func TestCmdReadLimit(t *testing.T) {
	content := "a\nb\nc\nd\ne\n"
	f := writeTempFile(t, content)

	out := captureRead(t, f, []string{"--limit", "2"})

	// Should have 2 content lines + truncation notice
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	contentLines := 0
	for _, l := range lines {
		if strings.Contains(l, "#") && strings.Contains(l, ":") {
			contentLines++
		}
	}
	if contentLines != 2 {
		t.Errorf("expected 2 content lines, got %d in output:\n%s", contentLines, out)
	}
	if !strings.Contains(out, "truncated") {
		t.Errorf("expected truncation notice, got:\n%s", out)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Edit operations — round-trip
// ────────────────────────────────────────────────────────────────────────────

func TestEditReplaceLine(t *testing.T) {
	content := "line one\nline two\nline three\n"
	f := writeTempFile(t, content)

	// Read to get hashes
	out := captureRead(t, f, nil)
	tags := extractTags(out)

	// Replace line 2
	req := EditRequest{
		Edits: []RawEdit{
			{Op: OpReplaceLine, Pos: tags[1], Lines: []string{"replaced"}},
		},
	}
	result := runEdit(t, f, req)
	if !result.OK {
		t.Fatalf("edit failed: %s", editErrMsg(f, req))
	}

	got := readFile(t, f)
	if !strings.Contains(got, "replaced") {
		t.Errorf("expected 'replaced' in output, got:\n%s", got)
	}
	if strings.Contains(got, "line two") {
		t.Errorf("expected 'line two' to be replaced, got:\n%s", got)
	}
}

func TestEditReplaceRange(t *testing.T) {
	content := "a\nb\nc\nd\ne\n"
	f := writeTempFile(t, content)
	tags := extractTags(captureRead(t, f, nil))

	// Replace lines 2–4 with a single line
	req := EditRequest{
		Edits: []RawEdit{
			{Op: OpReplaceRange, Pos: tags[1], End: tags[3], Lines: []string{"middle"}},
		},
	}
	result := runEdit(t, f, req)
	if !result.OK {
		t.Fatalf("edit failed: %s", editErrMsg(f, req))
	}

	got := readFile(t, f)
	want := "a\nmiddle\ne\n"
	if got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

func TestEditAppendAt(t *testing.T) {
	content := "a\nb\nc\n"
	f := writeTempFile(t, content)
	tags := extractTags(captureRead(t, f, nil))

	req := EditRequest{
		Edits: []RawEdit{
			{Op: OpAppendAt, Pos: tags[0], Lines: []string{"inserted"}},
		},
	}
	result := runEdit(t, f, req)
	if !result.OK {
		t.Fatalf("edit failed: %s", editErrMsg(f, req))
	}

	got := readFile(t, f)
	want := "a\ninserted\nb\nc\n"
	if got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

func TestEditPrependAt(t *testing.T) {
	content := "a\nb\nc\n"
	f := writeTempFile(t, content)
	tags := extractTags(captureRead(t, f, nil))

	req := EditRequest{
		Edits: []RawEdit{
			{Op: OpPrependAt, Pos: tags[1], Lines: []string{"before-b"}},
		},
	}
	result := runEdit(t, f, req)
	if !result.OK {
		t.Fatalf("edit failed: %s", editErrMsg(f, req))
	}

	got := readFile(t, f)
	want := "a\nbefore-b\nb\nc\n"
	if got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

func TestEditAppendFile(t *testing.T) {
	content := "a\nb\n"
	f := writeTempFile(t, content)
	captureRead(t, f, nil)

	req := EditRequest{
		Edits: []RawEdit{
			{Op: OpAppendFile, Lines: []string{"eof-line"}},
		},
	}
	result := runEdit(t, f, req)
	if !result.OK {
		t.Fatalf("edit failed: %s", editErrMsg(f, req))
	}

	got := readFile(t, f)
	if !strings.HasSuffix(got, "\neof-line\n") {
		t.Errorf("expected eof-line at end, got:\n%s", got)
	}
}

func TestEditPrependFile(t *testing.T) {
	content := "a\nb\n"
	f := writeTempFile(t, content)
	captureRead(t, f, nil)

	req := EditRequest{
		Edits: []RawEdit{
			{Op: OpPrependFile, Lines: []string{"bof-line"}},
		},
	}
	result := runEdit(t, f, req)
	if !result.OK {
		t.Fatalf("edit failed: %s", editErrMsg(f, req))
	}

	got := readFile(t, f)
	if !strings.HasPrefix(got, "bof-line\n") {
		t.Errorf("expected bof-line at start, got:\n%s", got)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Mismatch detection
// ────────────────────────────────────────────────────────────────────────────

func TestEditMismatch(t *testing.T) {
	content := "line one\nline two\nline three\n"
	f := writeTempFile(t, content)
	tags := extractTags(captureRead(t, f, nil))

	// Modify the file out-of-band to invalidate the hash
	writeFile(t, f, "line one\nXXX changed XXX\nline three\n")

	req := EditRequest{
		Edits: []RawEdit{
			{Op: OpReplaceLine, Pos: tags[1], Lines: []string{"should fail"}},
		},
	}

	var buf bytes.Buffer
	origStdout := stdout
	stdout = &buf
	stdin = strings.NewReader(toJSON(req))
	cmdEdit([]string{f})
	stdout = origStdout

	var errResp EditError
	if err := json.Unmarshal(buf.Bytes(), &errResp); err != nil {
		t.Fatalf("could not parse error response: %v\nraw: %s", err, buf.String())
	}
	if errResp.OK {
		t.Error("expected ok=false for hash mismatch")
	}
	if errResp.Error != "mismatch" {
		t.Errorf("expected error='mismatch', got %q", errResp.Error)
	}
	if len(errResp.Remaps) == 0 {
		t.Error("expected non-empty remaps")
	}
	if !strings.Contains(errResp.Message, ">>>") {
		t.Errorf("expected '>>>' in mismatch message, got:\n%s", errResp.Message)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Bottom-up ordering
// ────────────────────────────────────────────────────────────────────────────

func TestEditBottomUp(t *testing.T) {
	// Two replace_line edits on different lines — applying top-down would shift
	// line numbers; bottom-up must produce the correct result.
	content := "a\nb\nc\nd\ne\n"
	f := writeTempFile(t, content)
	tags := extractTags(captureRead(t, f, nil))

	req := EditRequest{
		Edits: []RawEdit{
			{Op: OpReplaceLine, Pos: tags[0], Lines: []string{"A"}}, // line 1
			{Op: OpReplaceLine, Pos: tags[4], Lines: []string{"E"}}, // line 5
		},
	}
	result := runEdit(t, f, req)
	if !result.OK {
		t.Fatalf("edit failed: %s", editErrMsg(f, req))
	}

	got := readFile(t, f)
	want := "A\nb\nc\nd\nE\n"
	if got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Boundary duplication warning
// ────────────────────────────────────────────────────────────────────────────

func TestEditBoundaryDuplicationWarning(t *testing.T) {
	content := "func foo() {\n\tbody()\n}\n"
	f := writeTempFile(t, content)
	tags := extractTags(captureRead(t, f, nil))

	// Replace line 2 but include "}" as the last replacement line — the "}"
	// on line 3 is the next surviving line, so a warning should fire.
	req := EditRequest{
		Edits: []RawEdit{
			{Op: OpReplaceLine, Pos: tags[1], Lines: []string{"\tnewBody()", "}"}},
		},
	}

	var buf bytes.Buffer
	origStdout := stdout
	stdout = &buf
	stdin = strings.NewReader(toJSON(req))
	cmdEdit([]string{f})
	stdout = origStdout

	var res EditResult
	if err := json.Unmarshal(buf.Bytes(), &res); err != nil {
		t.Fatalf("parse result: %v\nraw: %s", err, buf.String())
	}
	if !res.OK {
		t.Errorf("expected ok=true")
	}
	if len(res.Warnings) == 0 {
		t.Error("expected boundary duplication warning")
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Helpers
// ────────────────────────────────────────────────────────────────────────────

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "hashline-test-*.txt")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	f.Close()
	return f.Name()
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	return string(data)
}

// captureRead runs cmdRead on path with extra args and returns stdout.
func captureRead(t *testing.T, path string, extra []string) string {
	t.Helper()
	var buf bytes.Buffer
	origStdout := stdout
	stdout = &buf
	args := []string{path}
	args = append(args, extra...)
	cmdRead(args)
	stdout = origStdout
	return buf.String()
}

// extractTags parses the hashline-annotated output and returns a slice of
// "LINE#HASH" strings in file order (0-indexed by file line).
func extractTags(out string) []string {
	var tags []string
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		sepIdx := strings.Index(line, ":")
		if sepIdx < 0 {
			continue
		}
		tag := line[:sepIdx]
		if strings.Contains(tag, "#") {
			tags = append(tags, tag)
		}
	}
	return tags
}

// runEdit runs cmdEdit on path with req, returning the parsed EditResult.
// On JSON parse failure it returns an EditResult with OK=false.
func runEdit(t *testing.T, path string, req EditRequest) EditResult {
	t.Helper()
	var buf bytes.Buffer
	origStdout := stdout
	stdout = &buf
	stdin = strings.NewReader(toJSON(req))
	cmdEdit([]string{path})
	stdout = origStdout

	var res EditResult
	if err := json.Unmarshal(buf.Bytes(), &res); err != nil {
		t.Logf("raw edit output: %s", buf.String())
		t.Fatalf("parse EditResult: %v", err)
	}
	return res
}

func editErrMsg(path string, req EditRequest) string {
	var buf bytes.Buffer
	origStdout := stdout
	stdout = &buf
	stdin = strings.NewReader(toJSON(req))
	cmdEdit([]string{path})
	stdout = origStdout
	return buf.String()
}

func toJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
