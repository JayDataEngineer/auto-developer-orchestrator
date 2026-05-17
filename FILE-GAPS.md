# FILE-GAPS.md — File Read & Bash Output Truncation

Research: 2026-05-17. Compared Pi-Mono, Claude Code, OpenCode against our Go backend.

## Current State

| Component | What happens now | Problem |
|-----------|-----------------|---------|
| `file_read` tool | `SimpleSandboxOps.ReadFile()` → `os.ReadFile()` → full content | Schema has offset/limit params but **they're ignored**. Entire file dumped into context. |
| `bash` tool | Returns full stdout string | No truncation at tool level |
| `loop.go:540` | `ToolResultProcessor` or hard `result[:6000]` | Blind character slice — cuts mid-line, mid-JSON, no continuation info |
| `offloading.go` | Spills results >4096 bytes to disk, keeps preview | Good system, but upstream tools still produce raw full content |
| `base.go:59` | Same 6000-char hard truncation as fallback | Same problem — blind slice |
| `parallel_runner.go:1097` | Sub-agents get 12000-char truncation | Slightly better but still blind slice |

## What the References Do

### Pi-Mono (`truncate.ts`)
- **File reads**: `truncateHead()` — 2000 lines OR 50KB, whichever hits first
- **Bash output**: `truncateTail()` — keeps the END (errors, results), same limits
- Actionable messages: `"Showing lines 1-42 of 380. Use offset=43 to continue."`
- If first line > 50KB: `"Use bash: sed -n 'Xp' file | head -c 50000"`
- Per-line truncation for grep: 500 chars + `... [truncated]`

### Claude Code (`FileReadTool.ts` + `limits.ts`)
- **File reads**: 256KB file-size gate + 25,000 token post-read count
- Two-stage: stat check → read → token count (rough estimate, then exact API call)
- Dedup: re-reading same file/offset with same mtime → `file_unchanged` stub
- Streaming path for files >10MB (only accumulates lines in range)
- Line numbers added to output

### OpenCode (`view.go` + `bash.go`)
- **File reads**: 250KB size gate, 2000 line default, 2000 char per-line truncation
- **Bash output**: 30,000 chars, **middle-out** truncation (keep first half + last half)
- `bufio.Scanner` — reads line-by-line, never loads full file

## The Gaps

### Gap 1: No offset/limit in file_read (Critical)
The schema advertises `offset` and `limit` but `SimpleSandboxOps.ReadFile()` ignores them.
Every file read = entire file content → context.

### Gap 2: Blind character-slice truncation (Critical)
`result[:6000] + "...[truncated]"` cuts mid-line, mid-JSON. The model gets a broken
tool result and has no idea how to get the rest. No continuation hints.

### Gap 3: No line-level awareness (High)
No per-line truncation for absurdly long lines (minified JSON, base64, etc.).
A single 50KB line fills context.

### Gap 4: Bash output has no smart truncation (Medium)
Bash just dumps full stdout. Should use tail-truncation (keep the end — errors matter most)
or middle-out (OpenCode's approach).

### Gap 5: No file size pre-check (Medium)
`os.ReadFile()` loads the entire file into memory before any truncation happens.
A 10MB file = 10MB allocation even if we only need 2000 lines.

## Solution: Truncate Package

Create `go-backend/internal/tools/truncate/truncate.go` with three functions:

1. **`TruncateHead(content string, maxLines, maxBytes int) TruncationResult`**
   - For file reads. Keeps first N lines/bytes.
   - Returns: content, truncated flag, which limit hit, total lines, output lines.
   - Never splits mid-line.

2. **`TruncateTail(content string, maxLines, maxBytes int) TruncationResult`**
   - For bash output. Keeps last N lines/bytes.
   - May partial-truncate first line (same as Pi-Mono).

3. **`TruncateLine(line string, maxChars int) string`**
   - For grep/individual lines. Caps at maxChars + `... [truncated]`.

### Constants
```go
const (
    FileMaxLines = 2000    // max lines for file reads
    FileMaxBytes = 50 * 1024 // 50KB for file reads
    BashMaxChars = 30000   // ~30KB for bash output
    LineMaxChars = 2000    // per-line truncation
)
```

### file_read changes
- `ReadTool.Execute()` applies offset/limit from args
- Runs result through `TruncateHead()`
- Appends continuation message: `"Showing lines X-Y of Z. Use offset=N to continue."`
- First line > 50KB → error with bash fallback hint

### bash changes
- `Tool.Execute()` runs result through `TruncateTail()`
- Appends: `"... [%d lines truncated, showing last %d] ..."`

### ToolResultProcessor changes
- Replace blind `result[:6000]` with line-aware truncation
- Different strategies per tool: `file_read` → head, `bash` → tail, everything else → tail
