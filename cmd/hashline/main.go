// hashline — line-addressable file reader and editor for AI coding agents.
//
// Subcommands:
//
//	hashline read    <path> [--offset N] [--limit N]
//	hashline edit    <path>
//	hashline ripgrep [--limit N]
//
// Global flags (must appear before the subcommand):
//
//	--help, -h   Print this usage message and exit 0.
//	--skill      Print the SKILL.md reference for use in agent system prompts and exit 0.
//
// See read.go, edit.go, and ripgrep.go for subcommand documentation,
// or run:  hashline <subcommand> --help
package main

import (
	"fmt"
	"io"
	"os"
)

// stdin and stdout are package-level so tests can redirect them.
var stdin io.Reader = os.Stdin
var stdout io.Writer = os.Stdout

const topLevelHelp = `hashline — line-addressable file reader and editor for AI agents.

USAGE
  hashline [--help | --skill]
  hashline read    <path> [--offset N] [--limit N]
  hashline edit    <path>
  hashline ripgrep [--limit N]

GLOBAL FLAGS
  --help, -h   Print this message and exit.
  --skill      Print the full SKILL.md agent reference and exit.

SUBCOMMANDS

  read <path> [--offset N] [--limit N]
    Read a file and return every line annotated with a LINE#HASH tag:

      5#WS:func main() {
      6#NK:    fmt.Println("hello")
      7#TX:}

    Flags:
      --offset N   First line to output (1-indexed, default 1).
      --limit  N   Maximum lines to output (default 2000).

    Output is capped at 50 KB. When truncated, a notice is appended:
      [Output truncated. File has N lines total. Use --offset M to continue.]

    Next step after truncation: run  hashline read <path> --offset M

  edit <path>
    Read a JSON EditRequest from stdin, validate all LINE#HASH anchors, and
    apply the operations atomically (bottom-up). Always exits 0; check "ok".

    Input (JSON on stdin):
      { "edits": [ <operation>, ... ] }

    Operations:
      replace_line   Replace one line.          Requires: pos
      replace_range  Replace a range.           Requires: pos, end
      append_at      Insert after a line.       Requires: pos
      prepend_at     Insert before a line.      Requires: pos
      append_file    Append at end of file.
      prepend_file   Prepend at start of file.

    Each operation object:
      { "op": "replace_line", "pos": "5#WS", "lines": ["new content"] }

    Success response:
      { "ok": true, "firstChangedLine": 5 }

    Stale-anchor response (re-use the updated anchors shown in "remaps"):
      { "ok": false, "error": "mismatch", "message": "...", "remaps": { "5#WS": "5#XK" } }

    Rules:
      - Batch all edits for one file in one call (atomic, bottom-up).
      - Re-read the file after every successful edit before editing again.
      - Preserve exact indentation from the original.
      - When the last replacement line ends with } ) ], verify "end" includes
        the original closing-delimiter line to avoid duplication.

    Run  hashline edit --help  for the full operation reference.

  ripgrep [--limit N]
    Annotate rg --json output with LINE#HASH tags. Pipe rg into this command:

      rg --json 'pattern' /path | hashline ripgrep [--limit N]

    Match lines carry their LINE#HASH anchor. Context lines (from rg -C/-A/-B)
    are indented with two spaces.

    Flags:
      --limit N   Stop after N matches (default 100).

    Run  hashline ripgrep --help  for examples.

QUICK-START
  1. Read a file:       hashline read /path/to/file.go
  2. Edit it:           printf '{"edits":[...]}' | hashline edit /path/to/file.go
  3. Re-read to verify: hashline read /path/to/file.go
  4. Search:            rg --json 'pattern' /path/to/dir | hashline ripgrep

  For the full reference including common mistakes, run:  hashline --skill
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, "hashline: missing subcommand.\n\n")
		fmt.Fprint(os.Stderr, topLevelHelp)
		fmt.Fprintln(os.Stderr, "\nNext step: run  hashline --help  for full usage.")
		os.Exit(1)
	}

	sub := os.Args[1]
	rest := os.Args[2:]

	switch sub {
	case "--help", "-h", "help":
		fmt.Fprint(stdout, topLevelHelp)
		return
	case "--skill", "skill":
		fmt.Fprint(stdout, skillDoc)
		return
	case "read":
		cmdRead(rest)
	case "edit":
		cmdEdit(rest)
	case "ripgrep":
		cmdRipgrep(rest)
	default:
		fmt.Fprintf(os.Stderr, "hashline: unknown subcommand %q.\n\n", sub)
		fmt.Fprint(os.Stderr, "Valid subcommands: read, edit, ripgrep\n")
		fmt.Fprintln(os.Stderr, "Next step: run  hashline --help  to see all subcommands and usage.")
		os.Exit(1)
	}
}

// fatalf writes an error message to stderr and exits 1.
func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "hashline: "+format+"\n", args...)
	os.Exit(1)
}
