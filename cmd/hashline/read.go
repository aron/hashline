package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// maxReadBytes is the soft byte ceiling for read output (matches pi's default).
const maxReadBytes = 50 * 1024

const readHelp = `hashline read — annotate a file with LINE#HASH tags.

USAGE
  hashline read <path> [--offset N] [--limit N]
  hashline read --help

ARGUMENTS
  <path>       Path to the file to read. The file must be readable by the current user.

FLAGS
  --offset N   First line to output (1-indexed, default 1).
               Use this to paginate through large files.
  --limit  N   Maximum number of lines to output (default 2000).
               N must be >= 1.
  --help, -h   Print this message and exit.

OUTPUT FORMAT
  Every line is printed as:  LINE#HASH:content

  Example:
    5#WS:func main() {
    6#NK:    fmt.Println("hello")
    7#TX:}

  The LINE#HASH tag (e.g. "5#WS") is the anchor used by "hashline edit".

TRUNCATION
  Output is capped at 50 KB or --limit lines, whichever comes first.
  When truncated, a notice is appended:
    [Output truncated. File has N lines total. Use --offset M to continue.]

  Next step: re-run with  --offset M  to read the next page.

EXAMPLES
  hashline read /workspace/main.go
  hashline read /workspace/main.go --offset 100 --limit 50
  hashline read /workspace/main.go --limit 500

NEXT STEPS
  After reading, use "hashline edit" to modify lines by their LINE#HASH anchor.
  Use "rg --json ... | hashline ripgrep" to search without reading the whole file.
`

// cmdRead implements the "hashline read" subcommand.
func cmdRead(args []string) {
	if len(args) == 0 {
		fatalf("read requires a file path.\n\nUsage: hashline read <path> [--offset N] [--limit N]\nRun  hashline read --help  for details.")
	}

	// Handle --help / -h before touching other args.
	for _, a := range args {
		if a == "--help" || a == "-h" {
			fmt.Fprint(stdout, readHelp)
			return
		}
	}

	path := args[0]
	offset := 1
	limit := 2000

	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--offset":
			if i+1 >= len(args) {
				fatalf("--offset requires a value (e.g. --offset 100).\nRun  hashline read --help  for usage.")
			}
			i++
			n, err := parsePositiveInt(args[i])
			if err != nil {
				fatalf("--offset: %v\nValue must be an integer >= 1 (e.g. --offset 100).", err)
			}
			offset = n
		case "--limit":
			if i+1 >= len(args) {
				fatalf("--limit requires a value (e.g. --limit 500).\nRun  hashline read --help  for usage.")
			}
			i++
			n, err := parsePositiveInt(args[i])
			if err != nil {
				fatalf("--limit: %v\nValue must be an integer >= 1 (e.g. --limit 500).", err)
			}
			limit = n
		default:
			fatalf("unknown flag %q.\nRun  hashline read --help  for valid flags.", args[i])
		}
	}

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			fatalf("file not found: %s\nCheck the path is correct.", path)
		}
		if os.IsPermission(err) {
			fatalf("permission denied reading %s\nThe file exists but is not readable by the current user.", path)
		}
		fatalf("cannot open %s: %v", path, err)
	}
	defer f.Close()

	// Buffer the output so we can enforce the byte limit cleanly.
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // handle long lines

	lineNum := 0       // current file line number (1-indexed)
	printed := 0       // lines written to output
	bytesOut := 0      // approximate bytes written
	truncated := false // hit limit before EOF

	for scanner.Scan() {
		lineNum++

		if lineNum < offset {
			continue
		}
		if printed >= limit {
			truncated = true
			// Drain the scanner to count remaining lines
			for scanner.Scan() {
				lineNum++
			}
			break
		}
		if bytesOut >= maxReadBytes {
			truncated = true
			for scanner.Scan() {
				lineNum++
			}
			break
		}

		line := scanner.Text()
		tag := intToStr(lineNum) + "#" + computeLineHash(lineNum, line)
		formatted := tag + ":" + line + "\n"

		fmt.Fprint(stdout, formatted)
		bytesOut += len(formatted)
		printed++
	}

	if err := scanner.Err(); err != nil {
		fatalf("read error on %s: %v\nThe file may have been modified or deleted while reading.", path, err)
	}

	if truncated {
		remaining := lineNum - (offset + printed - 1)
		fmt.Fprintf(stdout, "\n[Output truncated. File has %d lines total. Use --offset %d to continue.]\n",
			lineNum, offset+printed)
		_ = remaining
	}
}

// parsePositiveInt parses a string as an integer >= 1.
func parsePositiveInt(s string) (int, error) {
	s = strings.TrimSpace(s)
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("%q is not a valid integer", s)
		}
		n = n*10 + int(c-'0')
	}
	if n < 1 {
		return 0, fmt.Errorf("value must be >= 1, got %d", n)
	}
	return n, nil
}
