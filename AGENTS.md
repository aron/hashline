# AGENTS.md — hashline

This file is a quick-start guide for AI agents working inside the hashline
source tree. It answers the most common questions an agent will have.

---

## What is this repository?

`hashline` is a standalone Go binary that provides LINE#HASH-annotated file I/O
to AI coding agents via three subcommands: `read`, `edit`, and `ripgrep`.

You only need to work directly with the Go source when modifying the binary itself.

---

## Getting help from the binary

```bash
hashline --help              # top-level overview and all subcommand summaries
hashline read    --help      # read subcommand: flags, output format, examples
hashline edit    --help      # edit subcommand: JSON schema, all operations, examples
hashline ripgrep --help      # ripgrep subcommand: flags, output format, examples
hashline --skill             # print the full SKILL.md reference for this tool
```

---

## Development scripts

Three scripts handle the common dev tasks and work from any directory:

```bash
script/format   # gofmt -w . (rewrite files in place)
script/lint     # gofmt check, go vet, staticcheck
script/test     # go test -v -race ./...
```

---

## File map

```
cmd/hashline/
  main.go          Entry point. Parses --help / --skill flags, dispatches subcommands.
  read.go          hashline read    — annotated file reader with pagination.
  edit.go          hashline edit    — atomic batch editor with anchor validation.
  ripgrep.go       hashline ripgrep — annotates rg --json output with LINE#HASH tags.
  types.go         Shared types: Anchor, EditOp, EditRequest, EditResult, ...
  hash.go          FNV-1a hash algorithm; nibble encoding; intToStr helper.
  skill.go         skillDoc constant; serves hashline --skill.
  hashline_test.go Test suite.
script/
  format           gofmt -w .
  lint             gofmt check, go vet, staticcheck
  test             go test -v -race ./...
```

The skill documentation is embedded as the `skillDoc` constant in `skill.go`.
Edit that constant directly when updating it — there is no separate source file.

---

## Key invariants — do not break these

1. **Hash stability**: `computeLineHash` in `hash.go` is the single canonical
   implementation. Do not introduce a second implementation without keeping them
   byte-for-byte compatible.

2. **Atomic writes**: `cmdEdit` always writes to a temp file first and renames.
   Never change it to write in place.

3. **Always exit 0 from `cmdEdit`**: Callers check the `ok` field of the JSON
   response; a non-zero exit code would cause a different error path. Only
   `fatalf` (called by `cmdRead` and `cmdRipgrep`) exits non-zero.

4. **Bottom-up application order**: Edits are sorted by line number descending
   before application so line numbers in earlier operations are not invalidated
   by later insertions.

5. **All anchors validated before any write**: `parseAndValidate` collects all
   mismatches from the entire batch before returning, so either all edits land or
   none do.

---

## Making changes

### Modifying the hash algorithm

**Do not.** Changing the algorithm invalidates all existing anchors that agents
have already read. If a change is truly necessary, bump a version suffix in the
output format and update `hash.go`.

### Adding a new subcommand

1. Create `newcmd.go` in `cmd/hashline/`.
2. Add a `case "newcmd":` branch in `main.go`'s `switch`.
3. Add `--help` handling at the top of the new function (see `cmdRead` for the
   pattern).
4. Update the top-level help block in `main.go`.
5. Update `SKILL.md` and `README.md`.

### Adding a new edit operation

1. Add the `EditOp` constant in `types.go`.
2. Handle it in `parseAndValidate` in `edit.go`.
3. Handle it in `applyEdits` in `edit.go`.
4. Update the help text in `edit.go` and `skillDoc` in `skill.go`.

### Updating the embedded skill documentation

Edit the `skillDoc` constant in `skill.go` directly. There is no build-time
embedding step — the source is the single source of truth.

---

## Error handling conventions

- `cmdRead` and `cmdRipgrep` call `fatalf(...)` for unrecoverable errors, which
  writes to stderr and exits 1.
- `cmdEdit` **never** calls `fatalf`. All errors are returned as JSON on stdout
  with `"ok": false` so callers can surface them programmatically.
- Error messages should tell the agent **what went wrong** and **what to do
  next**. Prefer: *"anchor 5#WS not found — re-read the file to get updated
  anchors"* over *"hash mismatch"*.

---

## Testing

```bash
script/test                      # all tests, verbose, with race detector
go test -run TestEdit ./cmd/hashline/   # run a targeted subset
```

The test suite exercises the hash algorithm, the read/edit/ripgrep command
functions directly (via the package-level `stdin`/`stdout` variables), boundary
duplication warnings, and hash-mismatch error formatting.
