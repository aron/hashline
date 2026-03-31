package main

// skillDoc is the content of skills/hashline/SKILL.md, embedded verbatim so
// that `hashline --skill` can print it without requiring access to the source
// tree at runtime.
//
// To update: edit skills/hashline/SKILL.md, then paste the new content here.
const skillDoc = `# hashline skill

` + "`hashline`" + ` provides LINE#HASH-annotated file I/O for AI agents. Every line of a
file is prefixed with a ` + "`LINE#HASH`" + ` tag (e.g. ` + "`5#WS:func main() {`" + `). Edit
operations target lines by these anchors rather than by exact text, making edits
resilient to whitespace differences and safe against stale references.

The skill is exposed to the agent as three pi tools: **read**, **edit**, and
**grep**.

---

## Core concept: LINE#HASH anchors

Every line returned by ` + "`read`" + ` or ` + "`grep`" + ` is prefixed with a tag of the form:

` + "```" + `
LINE#HASH:content
` + "```" + `

- ` + "`LINE`" + ` — 1-indexed line number
- ` + "`HASH`" + ` — 2-character content hash (alphabet: ` + "`ZPMQVRWSNKTXJBYH`" + `)
- ` + "`content`" + ` — the original line text

Example output:

` + "```" + `
5#WS:func main() {
6#NK:    fmt.Println("hello")
7#TX:}
` + "```" + `

Copy these tags **exactly** into edit operations. If the file changed since the
last read, stale hashes are rejected and the correct updated anchors are returned
so you can self-correct without re-reading.

---

## Tool: ` + "`read`" + `

Read a file, returning every line with its LINE#HASH anchor.

### Parameters

| Parameter | Type     | Required | Default | Description                        |
|-----------|----------|----------|---------|------------------------------------|
| ` + "`path`" + `    | string   | ✅        | —       | Path to the file                   |
| ` + "`offset`" + `  | number   |          | 1       | First line to read (1-indexed)     |
| ` + "`limit`" + `   | number   |          | 2000    | Max lines to return                |

### Rules

- Always re-read a file after editing it before editing again — hashes change.
- Use ` + "`offset`" + ` + ` + "`limit`" + ` for large files. The truncation notice tells you the
  next ` + "`offset`" + ` value: ` + "`[Output truncated. File has N lines. Use --offset M ...]`" + `
- Images are returned as base64 attachments.

---

## Tool: ` + "`edit`" + `

Apply one or more line-level edits atomically using LINE#HASH anchors.

### Parameters

| Parameter | Type   | Required | Description                          |
|-----------|--------|----------|--------------------------------------|
| ` + "`path`" + `    | string | ✅        | Absolute path to the file            |
| ` + "`edits`" + `   | array  | ✅        | Ordered list of edit operations      |

### Operations

| Op              | Required fields | Description                                  |
|-----------------|-----------------|----------------------------------------------|
| ` + "`replace_line`" + `  | ` + "`pos`" + `           | Replace a single line                        |
| ` + "`replace_range`" + ` | ` + "`pos`" + `, ` + "`end`" + `    | Replace a range of lines (inclusive)         |
| ` + "`append_at`" + `     | ` + "`pos`" + `           | Insert lines immediately **after** pos       |
| ` + "`prepend_at`" + `    | ` + "`pos`" + `           | Insert lines immediately **before** pos      |
| ` + "`append_file`" + `   | —               | Append lines at end of file                  |
| ` + "`prepend_file`" + `  | —               | Prepend lines at start of file               |

Each operation object:

` + "```" + `json
{
  "op": "replace_line",
  "pos": "5#WS",
  "lines": ["func run() {"]
}
` + "```" + `

- ` + "`lines`" + `: array of replacement/inserted lines. Omit or set to ` + "`null`" + ` to delete.
- ` + "`end`" + `: required for ` + "`replace_range`" + ` — the inclusive end anchor (e.g. ` + "`\"7#TX\"`" + `).

### Rules

- Batch all edits for one file in a single call — they are applied atomically
  bottom-up so earlier line numbers are not shifted when you reference them.
- Re-read the file after a successful edit before making further edits.
- Preserve exact indentation (tabs or spaces as found in the file).
- When your last replacement line ends with a closing delimiter (` + "`}`" + `, ` + "`)`" + `, ` + "`]`" + `),
  verify ` + "`end`" + ` includes the original line carrying that delimiter to avoid
  duplication.
- On hash mismatch the error response includes updated anchors in ` + "`remaps`" + ` — use
  them to correct the request without re-reading.

### Example

` + "```" + `json
{
  "path": "/workspace/main.go",
  "edits": [
    { "op": "replace_line", "pos": "5#WS", "lines": ["func run() {"] },
    { "op": "append_at",    "pos": "7#TX", "lines": ["", "// end of file"] }
  ]
}
` + "```" + `

### Success response

` + "```" + `json
{ "ok": true, "firstChangedLine": 5 }
` + "```" + `

### Mismatch response (stale anchor)

` + "```" + `json
{
  "ok": false,
  "error": "mismatch",
  "message": "1 line has changed since last read. Use the updated LINE#HASH references shown below (>>> marks changed lines).\n\n    4#JT:    x := 1\n>>> 5#XK:func run() {\n    6#NK:    fmt.Println(\"hello\")\n",
  "remaps": { "5#WS": "5#XK" }
}
` + "```" + `

---

## Tool: ` + "`grep`" + `

Search file contents for a pattern. Returns matching lines with LINE#HASH
anchors usable directly as edit targets — no follow-up read required.

### Parameters

| Parameter     | Type    | Required | Default | Description                               |
|---------------|---------|----------|---------|-------------------------------------------|
| ` + "`pattern`" + `     | string  | ✅        | —       | Search pattern (regex by default)         |
| ` + "`path`" + `        | string  |          | cwd     | Directory or file to search               |
| ` + "`glob`" + `        | string  |          | —       | File filter, e.g. ` + "`'*.ts'`" + ` or ` + "`'**/*.go'`" + ` |
| ` + "`ignoreCase`" + `  | boolean |          | false   | Case-insensitive search                   |
| ` + "`literal`" + `     | boolean |          | false   | Treat pattern as a literal string         |
| ` + "`context`" + `     | number  |          | 0       | Lines of context around each match        |
| ` + "`limit`" + `       | number  |          | 100     | Max matches to return                     |

Output is truncated to 100 matches. Long lines are truncated to 500 chars.

---

## Workflow summary

` + "```" + `
1. grep / read   →  obtain LINE#HASH anchors
2. edit          →  apply changes using those anchors
3. read          →  re-read to verify (hashes have changed)
4. edit again    →  use fresh anchors from step 3
` + "```" + `

---

## Common mistakes

| Mistake                                             | Fix                                                          |
|-----------------------------------------------------|--------------------------------------------------------------|
| Using a stale anchor after an edit                  | Re-read the file; hashes change after every write            |
| Forgetting ` + "`end`" + ` on ` + "`replace_range`" + `                 | Add the closing anchor, e.g. ` + "`\"end\": \"7#TX\"`" + `                  |
| Sending multiple edit calls for one file            | Batch all edits in a single call                             |
| Not preserving indentation                          | Match the exact whitespace (tabs vs spaces) from the original|
| Last replacement line duplicates the following line | Include the closing-delimiter line in the range (` + "`end`" + `)      |
`
