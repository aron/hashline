package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

const editHelp = `hashline edit — apply atomic line-level edits using LINE#HASH anchors.

USAGE
  hashline edit <path>
  hashline edit --help

  JSON EditRequest is read from stdin. Always exits 0; check the "ok" field.

ARGUMENTS
  <path>   Absolute path to the file to edit. Use /workspace/... paths.
           The file must be readable and writable by the current user.

FLAGS
  --help, -h   Print this message and exit.

INPUT FORMAT (stdin)
  A JSON object with an "edits" array:

    {
      "edits": [
        { "op": "<operation>", "pos": "<LINE#HASH>", "lines": ["..."] },
        ...
      ]
    }

  All operations are validated and applied atomically. If any anchor is stale
  the entire batch is rejected — nothing is written to disk.

OPERATIONS
  replace_line    Replace a single line.
    Required: pos
    { "op": "replace_line", "pos": "5#WS", "lines": ["new content"] }

  replace_range   Replace a contiguous range of lines (inclusive).
    Required: pos (start), end (inclusive end)
    { "op": "replace_range", "pos": "5#WS", "end": "7#TX", "lines": ["a", "b"] }

  append_at       Insert lines immediately after the anchor line.
    Required: pos
    { "op": "append_at", "pos": "6#NK", "lines": ["new line"] }

  prepend_at      Insert lines immediately before the anchor line.
    Required: pos
    { "op": "prepend_at", "pos": "5#WS", "lines": ["// header"] }

  append_file     Append lines at the end of the file.
    { "op": "append_file", "lines": ["// EOF"] }

  prepend_file    Prepend lines at the start of the file.
    { "op": "prepend_file", "lines": ["// header"] }

FIELD REFERENCE
  op     string  (required) One of the operations above.
  pos    string  LINE#HASH anchor for the target line (e.g. "5#WS").
                 Required for: replace_line, replace_range, append_at, prepend_at.
  end    string  LINE#HASH anchor for the inclusive end of a replace_range.
                 Required for: replace_range.
  lines  array   Replacement or inserted strings.
                 Omit or set to null to delete (replace_line / replace_range only).
                 Empty array [] inserts a blank line.

SUCCESS RESPONSE
  { "ok": true, "firstChangedLine": 5 }

  firstChangedLine is the 1-indexed number of the first modified line.
  If warnings are present (e.g. possible boundary duplication), they appear as:
  { "ok": true, "firstChangedLine": 5, "warnings": ["Possible boundary duplication..."] }

  Next step: re-read the file with  hashline read <path>  to get fresh anchors.

ERROR RESPONSES
  Stale anchor ("error": "mismatch"):
    { "ok": false, "error": "mismatch",
      "message": "1 line has changed since last read. Use the updated LINE#HASH
        references shown below (>>> marks changed lines).\n\n    4#JT:...\n>>> 5#XK:...",
      "remaps": { "5#WS": "5#XK" } }

    Next step: update your anchors using the "remaps" map and retry. No need to
    re-read the whole file — the remaps provide the corrected anchors.

  Invalid request ("error": "invalid"):
    { "ok": false, "error": "invalid", "message": "edits array is empty" }

    Next step: fix the JSON request and retry.

  I/O error ("error": "io"):
    { "ok": false, "error": "io", "message": "cannot read /workspace/foo.go: ..." }

    Next step: verify the path is correct and accessible, then retry.

RULES
  1. Batch all edits for one file in a single call — edits are applied
     atomically bottom-up, so earlier line numbers are stable even when later
     insertions shift line counts.
  2. Re-read the file after every successful edit before editing again. Hashes
     change after every write, so anchors from the previous read are stale.
  3. Preserve exact indentation (tabs or spaces as found in the original file).
  4. When the last replacement line ends with a closing delimiter (} ) ]),
     verify "end" includes the original line carrying that delimiter to avoid
     duplicating it.
  5. On mismatch, use the "remaps" field to update your anchors directly.
     Only re-read the file if you need to see more context around the changes.

PIPING EXAMPLE
  printf '{"edits":[{"op":"replace_line","pos":"5#WS","lines":["func run() {"]}]}' \
    | hashline edit /workspace/main.go

  Or via base64 to avoid quoting issues:
  printf '%s' '<base64-json>' | base64 -d | hashline edit /workspace/main.go
`

// cmdEdit implements the "hashline edit" subcommand.
//
// Usage: hashline edit <path>
//
// Reads a JSON EditRequest from stdin, applies all operations atomically,
// and writes a JSON result to stdout. Always exits 0; check the "ok" field.
// On hash mismatch the response includes updated anchors for self-correction.
func cmdEdit(args []string) {
	// Handle --help / -h before touching other args.
	for _, a := range args {
		if a == "--help" || a == "-h" {
			io.WriteString(stdout, editHelp) //nolint
			return
		}
	}

	if len(args) == 0 {
		writeJSON(EditError{
			OK:      false,
			Error:   "invalid",
			Message: "hashline edit requires a file path.\n\nUsage: hashline edit <path>\nPipe a JSON EditRequest on stdin.\nRun  hashline edit --help  for the full operation reference.",
		})
		return
	}

	path := args[0]

	// Decode request from stdin
	var req EditRequest
	if err := json.NewDecoder(stdin).Decode(&req); err != nil {
		writeJSON(EditError{
			OK:      false,
			Error:   "invalid",
			Message: fmt.Sprintf("failed to decode JSON from stdin: %v\n\nExpected format: {\"edits\":[{\"op\":\"replace_line\",\"pos\":\"5#WS\",\"lines\":[\"new\"]}]}\nRun  hashline edit --help  for the full schema.", err),
		})
		return
	}

	if len(req.Edits) == 0 {
		writeJSON(EditError{
			OK:      false,
			Error:   "invalid",
			Message: "edits array is empty.\n\nProvide at least one operation in the \"edits\" array.\nRun  hashline edit --help  for the full operation reference.",
		})
		return
	}

	// Read the file
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(EditError{
				OK:      false,
				Error:   "io",
				Message: fmt.Sprintf("file not found: %s\n\nCheck the path is correct.", path),
			})
		} else if os.IsPermission(err) {
			writeJSON(EditError{
				OK:      false,
				Error:   "io",
				Message: fmt.Sprintf("permission denied reading %s\n\nThe file exists but is not readable by the current user.", path),
			})
		} else {
			writeJSON(EditError{OK: false, Error: "io", Message: fmt.Sprintf("cannot read %s: %v", path, err)})
		}
		return
	}

	// Preserve original file permissions (e.g. executable bit)
	fi, err := os.Stat(path)
	if err != nil {
		writeJSON(EditError{OK: false, Error: "io", Message: fmt.Sprintf("cannot stat %s: %v", path, err)})
		return
	}
	origMode := fi.Mode().Perm()

	// Split into lines preserving the original content (no trailing newline strip)
	content := string(data)
	fileLines := strings.Split(content, "\n")
	// If the file ends with a newline, Split produces a trailing empty string —
	// that's correct: the last line is empty and is a real line in our model.

	// Parse and validate anchors
	edits, mismatches := parseAndValidate(req.Edits, fileLines)
	if len(mismatches) > 0 {
		writeJSON(buildMismatchError(mismatches, fileLines))
		return
	}

	// Apply edits bottom-up
	fileLines, warnings, firstChanged := applyEdits(edits, fileLines)

	// Write result atomically: temp file in same directory, then rename
	newContent := strings.Join(fileLines, "\n")
	tmpPath := path + ".hashline-tmp"
	if err := os.WriteFile(tmpPath, []byte(newContent), origMode); err != nil {
		writeJSON(EditError{
			OK:      false,
			Error:   "io",
			Message: fmt.Sprintf("cannot write temp file %s: %v\n\nCheck that the directory is writable.", tmpPath, err),
		})
		return
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		writeJSON(EditError{
			OK:      false,
			Error:   "io",
			Message: fmt.Sprintf("cannot rename temp file to %s: %v\n\nThe temp file has been removed. Retry the edit.", path, err),
		})
		return
	}

	result := EditResult{OK: true, FirstChangedLine: firstChanged}
	if len(warnings) > 0 {
		result.Warnings = warnings
	}
	writeJSON(result)
}

// parseAndValidate parses all raw edits, validates their anchors against the
// current file content, and collects all mismatches before returning. This
// ensures the entire batch is rejected atomically if any anchor is stale.
func parseAndValidate(rawEdits []RawEdit, fileLines []string) ([]Edit, []mismatch) {
	var edits []Edit
	var mismatches []mismatch

	// Track which lines we've already found as mismatched to avoid duplicates.
	seenMismatch := map[int]bool{}

	validateAnchor := func(s string) (Anchor, bool) {
		a, err := parseAnchor(s)
		if err != nil {
			// Treat unparseable anchors as a special mismatch with empty hash
			mismatches = append(mismatches, mismatch{line: 0, expected: s, actual: ""})
			return Anchor{}, false
		}
		if a.Line < 1 || a.Line > len(fileLines) {
			mismatches = append(mismatches, mismatch{
				line:     a.Line,
				expected: a.Hash,
				actual:   fmt.Sprintf("(line %d out of range; file has %d lines)", a.Line, len(fileLines)),
			})
			return a, false
		}
		actual := computeLineHash(a.Line, fileLines[a.Line-1])
		if actual != a.Hash && !seenMismatch[a.Line] {
			seenMismatch[a.Line] = true
			mismatches = append(mismatches, mismatch{line: a.Line, expected: a.Hash, actual: actual})
		}
		return a, actual == a.Hash
	}

	for _, raw := range rawEdits {
		switch raw.Op {
		case OpReplaceLine:
			pos, ok := validateAnchor(raw.Pos)
			if !ok {
				continue
			}
			edits = append(edits, Edit{Op: raw.Op, Pos: pos, Lines: raw.Lines})

		case OpReplaceRange:
			pos, okPos := validateAnchor(raw.Pos)
			end, okEnd := validateAnchor(raw.End)
			if !okPos || !okEnd {
				continue
			}
			if pos.Line > end.Line {
				mismatches = append(mismatches, mismatch{
					line:     pos.Line,
					expected: pos.Hash,
					actual:   fmt.Sprintf("(range start %d > end %d)", pos.Line, end.Line),
				})
				continue
			}
			edits = append(edits, Edit{Op: raw.Op, Pos: pos, End: end, Lines: raw.Lines})

		case OpAppendAt, OpPrependAt:
			pos, ok := validateAnchor(raw.Pos)
			if !ok {
				continue
			}
			lines := raw.Lines
			if len(lines) == 0 {
				lines = []string{""} // insert a blank line
			}
			edits = append(edits, Edit{Op: raw.Op, Pos: pos, Lines: lines})

		case OpAppendFile, OpPrependFile:
			lines := raw.Lines
			if len(lines) == 0 {
				lines = []string{""} // insert a blank line
			}
			edits = append(edits, Edit{Op: raw.Op, Lines: lines})

		default:
			// Unknown op: surface as an invalid error immediately
			writeJSON(EditError{
				OK:      false,
				Error:   "invalid",
				Message: fmt.Sprintf("unknown op %q.\n\nValid ops: replace_line, replace_range, append_at, prepend_at, append_file, prepend_file\nRun  hashline edit --help  for the full operation reference.", raw.Op),
			})
			os.Exit(0)
		}
	}

	return edits, mismatches
}

// annotated wraps an Edit with its computed sort key.
type annotated struct {
	edit       Edit
	idx        int
	sortLine   int
	precedence int
}

// applyEdits applies validated edits to fileLines bottom-up to avoid
// line-number drift. Returns any warnings and the 1-indexed first changed line.
func applyEdits(edits []Edit, fileLines []string) ([]string, []string, int) {
	ann := make([]annotated, len(edits))
	for i, e := range edits {
		var sortLine, prec int
		switch e.Op {
		case OpReplaceLine:
			sortLine, prec = e.Pos.Line, 0
		case OpReplaceRange:
			sortLine, prec = e.End.Line, 0
		case OpAppendAt:
			sortLine, prec = e.Pos.Line, 1
		case OpPrependAt:
			sortLine, prec = e.Pos.Line, 2
		case OpAppendFile:
			sortLine, prec = len(fileLines)+1, 1
		case OpPrependFile:
			sortLine, prec = 0, 2
		}
		ann[i] = annotated{edit: e, idx: i, sortLine: sortLine, precedence: prec}
	}

	// Sort descending by sortLine, then ascending by precedence and original index.
	sortAnnotated(ann)

	var warnings []string
	firstChanged := 0
	upd := func(line int) {
		if firstChanged == 0 || line < firstChanged {
			firstChanged = line
		}
	}

	// Snapshot original lines for boundary-duplication warning.
	original := make([]string, len(fileLines))
	copy(original, fileLines)

	for _, a := range ann {
		e := a.edit
		switch e.Op {
		case OpReplaceLine:
			// Warn if last inserted line == next surviving line (boundary overreach)
			if len(e.Lines) > 0 && e.Pos.Line < len(original) {
				next := strings.TrimSpace(original[e.Pos.Line]) // 0-indexed next
				last := strings.TrimSpace(e.Lines[len(e.Lines)-1])
				if last != "" && last == next {
					nextTag := formatTag(e.Pos.Line+1, original[e.Pos.Line])
					warnings = append(warnings, fmt.Sprintf(
						"Possible boundary duplication: last replacement line %q matches next surviving line %s. "+
							"If you meant to replace the whole block, set end to %s.",
						last, nextTag, nextTag))
				}
			}
			fileLines = splice(fileLines, e.Pos.Line-1, 1, e.Lines)
			upd(e.Pos.Line)

		case OpReplaceRange:
			count := e.End.Line - e.Pos.Line + 1
			// Warn on boundary overreach
			if len(e.Lines) > 0 && e.End.Line < len(original) {
				next := strings.TrimSpace(original[e.End.Line]) // 0-indexed next after end
				last := strings.TrimSpace(e.Lines[len(e.Lines)-1])
				if last != "" && last == next {
					nextTag := formatTag(e.End.Line+1, original[e.End.Line])
					warnings = append(warnings, fmt.Sprintf(
						"Possible boundary duplication: last replacement line %q matches next surviving line %s. "+
							"If you meant to replace the whole block, set end to %s.",
						last, nextTag, nextTag))
				}
			}
			fileLines = splice(fileLines, e.Pos.Line-1, count, e.Lines)
			upd(e.Pos.Line)

		case OpAppendAt:
			fileLines = splice(fileLines, e.Pos.Line, 0, e.Lines)
			upd(e.Pos.Line + 1)

		case OpPrependAt:
			fileLines = splice(fileLines, e.Pos.Line-1, 0, e.Lines)
			upd(e.Pos.Line)

		case OpAppendFile:
			if len(fileLines) == 1 && fileLines[0] == "" {
				// Truly empty file — replace the single empty element
				fileLines = append([]string{}, e.Lines...)
				upd(1)
			} else if len(fileLines) > 0 && fileLines[len(fileLines)-1] == "" {
				// File ends with newline — insert before the trailing empty element
				ins := len(fileLines) - 1
				fileLines = append(fileLines[:ins], append(e.Lines, "")...)
				upd(ins + 1)
			} else {
				fileLines = append(fileLines, e.Lines...)
				upd(len(fileLines) - len(e.Lines) + 1)
			}

		case OpPrependFile:
			if len(fileLines) == 1 && fileLines[0] == "" {
				fileLines = append([]string{}, e.Lines...)
			} else {
				fileLines = append(e.Lines, fileLines...)
			}
			upd(1)
		}
	}

	return fileLines, warnings, firstChanged
}

// splice inserts `ins` into `s` at position `at`, replacing `del` elements.
// Returns the new slice.
func splice(s []string, at, del int, ins []string) []string {
	n := len(s) - del + len(ins)
	result := make([]string, n)
	copy(result, s[:at])
	copy(result[at:], ins)
	copy(result[at+len(ins):], s[at+del:])
	return result
}

// sortAnnotated sorts the annotated edits bottom-up: descending by sortLine,
// then ascending by precedence, then ascending by original index.
func sortAnnotated(ann []annotated) {
	// Simple insertion sort — edit counts are small.
	for i := 1; i < len(ann); i++ {
		for j := i; j > 0; j-- {
			a, b := ann[j], ann[j-1]
			less := a.sortLine > b.sortLine ||
				(a.sortLine == b.sortLine && a.precedence < b.precedence) ||
				(a.sortLine == b.sortLine && a.precedence == b.precedence && a.idx < b.idx)
			if !less {
				break
			}
			ann[j], ann[j-1] = ann[j-1], ann[j]
		}
	}
}
