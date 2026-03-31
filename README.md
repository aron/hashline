# hashline

`hashline` is a line-addressable file reader and editor for AI coding agents.
It annotates every line with a short `LINE#HASH` tag and lets agents edit files
by anchoring operations to those tags rather than to exact text. Stale anchors
are detected before any write occurs, and the updated anchors are returned in the
error so the agent can self-correct without re-reading the whole file.

It is the Go binary that backs the **read**, **edit**, and **grep** pi tools —
both inside the sandbox container and on the host when the container is not active.

---

## Why LINE#HASH anchors?

When an LLM edits a file it typically targets lines by reproducing the original
text verbatim. This breaks if the model misquotes whitespace, trims a trailing
space, or references a line that has changed since the last read. `hashline`
fixes this by:

1. Prefixing every line with `LINE#HASH` on read (e.g. `5#WS:func main() {`).
2. Accepting those tags as edit targets on write.
3. Rejecting the whole edit batch — atomically — if any anchor is stale, and
   returning the correct updated anchors so the model can self-correct without
   fetching the file again.

The two-character hash is computed from the line content using FNV-1a 32-bit
with a custom nibble alphabet (`ZPMQVRWSNKTXJBYH`). Lines without letters or
digits (blank lines, `{`, `}`, etc.) additionally mix in the 1-indexed line
number so that structurally identical lines at different positions always get
different hashes.

---

## Hash algorithm

```
hash(lineNum, line):
  1. strip trailing CR and trailing whitespace
  2. if line has no letter/digit: mix lineNum into FNV32a state
  3. FNV-1a 32-bit hash of normalised UTF-8 bytes
  4. take lowest byte → split into two 4-bit nibbles
  5. map each nibble through ZPMQVRWSNKTXJBYH
```

The algorithm lives only in the Go source. There is no TypeScript reimplementation.

---

## Subcommands

### `hashline read <path> [--offset N] [--limit N]`

Reads a file and prints every line as `LINE#HASH:content`. Supports pagination
via `--offset` and `--limit`. Stops at 50 KB of output or 2 000 lines,
whichever comes first, and appends a truncation notice with the next `--offset`
value.

Images are detected by MIME type and returned as base64 data URIs so the agent
can view them inline.

### `hashline edit <path>`

Reads a JSON `EditRequest` from stdin, validates all anchors, and applies the
operations bottom-up so earlier line-number references are not shifted. The
result is written atomically (temp file + rename). Always exits 0; check the
`ok` field of the JSON response.

Operations: `replace_line`, `replace_range`, `append_at`, `prepend_at`,
`append_file`, `prepend_file`.

On hash mismatch the response includes `remaps` (stale→current anchor mapping)
and a visual diff with `>>>` markers so the model can correct its request.

### `hashline ripgrep [--limit N]`

Reads `rg --json` NDJSON from stdin and emits a human-readable, hash-annotated
listing. Match lines carry their `LINE#HASH` anchor; context lines (from
`-A/-B/-C`) are indented with two spaces. Stops after `--limit` matches
(default 100).

---

## Install

### Pre-built binaries (recommended)

Download the latest release for your platform from the
[Releases page](https://github.com/aron/hashline/releases).
Archives contain the `hashline` binary and the `README.md`.

Checksums are published in `checksums.txt` alongside each release.

### Install with `go install`

```bash
go install github.com/aron/hashline@latest
```

## Build from source

```bash
git clone https://github.com/aron/hashline
cd hashline
go build -o hashline ./cmd/hashline
```

### For the container image

```bash
go build -o hashline ./cmd/hashline
# then COPY hashline /usr/local/bin/hashline in your Containerfile/Dockerfile
```

The binary has no external dependencies — only the Go standard library.

---

## Development

Three scripts handle the common dev tasks. They work from any directory.

| Script | What it does |
|---|---|
| `script/format` | `gofmt -w .` — rewrite all files in place |
| `script/lint` | `gofmt` check, `go vet`, `staticcheck` (auto-installs if missing) |
| `script/test` | `go test -v -race ./...` |

---

## Project layout

```
cmd/hashline/    Binary source — main package
  main.go        Entry point; subcommand dispatch, --help and --skill
  read.go        hashline read subcommand
  edit.go        hashline edit subcommand
  ripgrep.go     hashline ripgrep subcommand
  types.go       Shared types: Anchor, EditOp, EditRequest, EditResult, ...
  hash.go        FNV-1a hash algorithm and intToStr helper
  skill.go       skillDoc constant; serves --skill
  *_test.go      Test suite
script/          Dev scripts (format, lint, test)
.github/
  workflows/     CI (ci.yml) and release (release.yml) pipelines
```

---

## Wire format

### `hashline edit` stdin

```json
{
  "edits": [
    { "op": "replace_line",  "pos": "5#WS",  "lines": ["replacement"] },
    { "op": "replace_range", "pos": "5#WS",  "end": "7#TX", "lines": ["a", "b"] },
    { "op": "append_at",     "pos": "6#NK",  "lines": ["new line"] },
    { "op": "prepend_at",    "pos": "5#WS",  "lines": ["header"] },
    { "op": "append_file",                   "lines": ["// EOF"] },
    { "op": "prepend_file",                  "lines": ["// header"] }
  ]
}
```

### Success response

```json
{ "ok": true, "firstChangedLine": 5 }
```

### Mismatch response

```json
{
  "ok": false,
  "error": "mismatch",
  "message": "1 line has changed since last read. Use the updated LINE#HASH references shown below...",
  "remaps": { "5#WS": "5#XK" }
}
```

### Other error responses

```json
{ "ok": false, "error": "invalid", "message": "edits array is empty" }
{ "ok": false, "error": "io",      "message": "cannot read /workspace/foo.go: ..." }
```

---

## Related

- [The Harness Problem](https://blog.can.ac/2026/02/12/the-harness-problem/) — motivation
- [oh-my-pi](https://github.com/can1357/oh-my-pi) — inspiration
