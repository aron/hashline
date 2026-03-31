package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"strings"
)

const ripgrepHelp = `hashline ripgrep — annotate rg --json output with LINE#HASH tags.

USAGE
  rg --json [rg-flags] <pattern> [path] | hashline ripgrep [--limit N]
  hashline ripgrep --help

  Reads ripgrep's NDJSON output from stdin and writes a human-readable,
  LINE#HASH-annotated listing to stdout.

FLAGS
  --limit N    Stop after N matches (default 100). Must be >= 1.
  --help, -h   Print this message and exit.

OUTPUT FORMAT
  Match lines are emitted with their LINE#HASH anchor:
    /path/to/file.go
    42#WS:func main() {
      43#NK:	// context line (indented 2 spaces)

  - Match lines:   LINE#HASH:content  (no indent)
  - Context lines: "  " LINE#HASH:content  (2-space indent, from rg -A/-B/-C)
  - File headers:  /absolute/path/to/file

  The LINE#HASH anchors can be used directly with "hashline edit" without a
  follow-up "hashline read".

TRUNCATION
  Output is capped at --limit matches (default 100). When truncated:
    [Truncated after 100 matches. Narrow your search or increase --limit.]

  Next steps:
    - Narrow the pattern to reduce matches.
    - Add a glob filter:  rg --json -g '*.go' <pattern>
    - Increase the limit: hashline ripgrep --limit 200

EXAMPLES
  # Search for a pattern in the workspace
  rg --json 'func main' /workspace | hashline ripgrep

  # Case-insensitive search with context
  rg --json -i -C 2 'error' /workspace/cmd | hashline ripgrep

  # Limit to Go files, cap at 50 matches
  rg --json -g '*.go' 'TODO' /workspace | hashline ripgrep --limit 50

  # Literal string search (no regex)
  rg --json -F 'fmt.Println' /workspace | hashline ripgrep

NEXT STEPS
  After finding lines with ripgrep, use "hashline edit" to modify them:
    rg --json 'pattern' /workspace | hashline ripgrep
    # note the LINE#HASH anchor, e.g. 42#WS
    printf '{"edits":[{"op":"replace_line","pos":"42#WS","lines":["new"]}]}' \
      | hashline edit /workspace/file.go

  You can also use "hashline read" to read the surrounding context of a match.
`

// cmdRipgrep implements the "hashline ripgrep" subcommand.
//
// Usage: rg --json [rg-args...] | hashline ripgrep [--limit N]
//
// Reads ripgrep's NDJSON output (rg --json) from stdin and writes a
// human-readable, hashline-annotated result to stdout.
//
// Match lines are emitted with their LINE#HASH anchor so they can be used
// directly as edit targets. Context lines (from -A/-B/-C) are indented with
// two spaces to visually distinguish them from match lines.
func cmdRipgrep(args []string) {
	// Handle --help / -h before touching other args.
	for _, a := range args {
		if a == "--help" || a == "-h" {
			fmt.Fprint(stdout, ripgrepHelp)
			return
		}
	}

	limit := 100

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--limit":
			if i+1 >= len(args) {
				fatalf("--limit requires a value (e.g. --limit 50).\nRun  hashline ripgrep --help  for usage.")
			}
			i++
			n, err := parsePositiveInt(args[i])
			if err != nil {
				fatalf("--limit: %v\nValue must be an integer >= 1 (e.g. --limit 50).", err)
			}
			limit = n
		default:
			fatalf("unknown flag %q.\nRun  hashline ripgrep --help  for valid flags.", args[i])
		}
	}

	// rg --json record shapes we care about
	type rgPath struct {
		Text string `json:"text"`
	}
	type rgLines struct {
		Text string `json:"text"`
	}
	type rgMatchData struct {
		Path       rgPath  `json:"path"`
		Lines      rgLines `json:"lines"`
		LineNumber int     `json:"line_number"`
	}
	type rgRecord struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}

	scanner := bufio.NewScanner(stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	matchCount := 0
	truncated := false
	currentFile := ""

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var rec rgRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue // skip malformed lines
		}

		switch rec.Type {
		case "begin":
			var d struct {
				Path rgPath `json:"path"`
			}
			if err := json.Unmarshal(rec.Data, &d); err != nil {
				continue
			}
			currentFile = d.Path.Text
			fmt.Fprintln(stdout, currentFile)

		case "match", "context":
			var d rgMatchData
			if err := json.Unmarshal(rec.Data, &d); err != nil {
				continue
			}

			if rec.Type == "match" {
				matchCount++
				if matchCount > limit {
					truncated = true
					// Drain remaining input so rg doesn't get SIGPIPE
					for scanner.Scan() {
					}
					break
				}
			}

			content := strings.TrimRight(d.Lines.Text, "\r\n")
			hash := computeLineHash(d.LineNumber, content)
			tag := intToStr(d.LineNumber) + "#" + hash

			if rec.Type == "context" {
				fmt.Fprintf(stdout, "  %s:%s\n", tag, content)
			} else {
				fmt.Fprintf(stdout, "%s:%s\n", tag, content)
			}

		case "end":
			fmt.Fprintln(stdout, "")
		}

		if truncated {
			break
		}
	}

	_ = currentFile // used via fmt.Fprintln above; suppress unused-variable lint in some editors

	if truncated {
		fmt.Fprintf(stdout, "[Truncated after %d matches. Narrow your search or increase --limit.]\n", limit)
	}
}
