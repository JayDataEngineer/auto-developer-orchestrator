# Feature Parity Gaps — browser-use vs Our Agent

Documented from deep codebase analysis of browser-use (reference/repos/browser-use/) and live testing of our orchestrator against real image-search tasks. Updated 2026-05-06.

---

## Resolved

### ~~1. bash tool cannot handle binary output~~ — FALSE ALARM
- **Resolution**: Tested `curl -sL -o /tmp/file URL` via `docker exec` and `sh -c` — both work. The "exit code 23" was from specific image URLs returning 403/404, not from a binary output bug. The TTY-mode `io.Copy` + `buf.String()` handles text output correctly, and `curl -o` writes to disk inside the container, not through stdout.

### ~~2. chromedp BrowserDriver never wired~~ — DEFERRED
- **Status**: Still true, but no longer blocking. sb_server.py provides stateful browser via HTTP inside the sandbox. Go chromedp tools would add a second (faster) path but sb_server covers all browsing needs.
- **Deferral reason**: sb_server has SoM labeling, page stats, screenshots, tab management, and DuckDuckGo search — feature parity with what the Go browser tools would provide.

### ~~7. sandbox image is stale~~ — RESOLVED
- sb_server now runs under supervisord in the sandbox. Confirmed working on running sandbox.

---

## Critical Gaps (break real tasks)

### 3. analyze_image cannot reach local files
- **Where**: MCP `analyze_image` tool implementation
- **Symptom**: `file:///tmp/file.png` → "Request URL is missing an 'http://' or 'https://' protocol." `http://localhost:9999/file.png` → "All connection attempts failed"
- **Impact**: Vision-in-the-loop flow breaks. Agent downloads a file to sandbox `/tmp/` but can't get vision description of it. Must use the remote URL instead.
- **Workaround**: Use the remote URL directly with `analyze_image`, or use the MCP `process` tool which accepts remote URLs and routes to vision.

### 4. Vision model fails on certain PNG/WebP files
- **Where**: MCP vision inference pipeline
- **Symptom**: "Unable to infer channel dimension format" on some PNGs
- **Impact**: ~40% of scraped anime art images fail vision analysis.
- **Likely cause**: Files served as `.png` but actually WebP or unusual color channels.

---

## Architecture Gaps

### 5. Prompt system is entirely hard-coded
- **Where**: `go-backend/internal/agents/common/common.go:28` — `BuildOrchestratorPrompt()`
- **Nature**: All 160 lines are raw `b.WriteString()` calls. No external template files, no environment variable overrides, no `--system-prompt` flag, no per-project customization.
- **Why it matters**: Can't iterate on prompt quality without recompiling. Every behavior tweak is a Go code change. Contrast with browser-use which has `system_prompt.md` (269 lines, external template with `{max_actions}` substitution).

### 6. No PageInfo → PageContext adapter exists
- **Where**: Between `internal/browser/browser.go` and `internal/tools/browser/browser.go`
- **Gap**: `SandboxBrowserClient.Navigate()` returns `*browser.PageInfo`. `tools/browser.Driver.Navigate()` must return `*tools/browser.PageContext`. No conversion function exists.
- **Impact**: Low priority since sb_server handles browsing now. Only needed if we want to resurrect the Go chromedp path.

### 8. No per-project prompt override
- **Where**: `BuildOrchestratorPrompt` signature
- **Gap**: Function takes `projectContext string` and `examples string`, but these only come from memory/MEMORY.md. No mechanism for a project to supply custom tool tips, behavior rules, or prompt templates.

---

## sb_server.py vs browser-use Gaps

### 9. No occlusion-aware clicking
- **browser-use**: Checks if element is hidden behind other elements before clicking. Falls back to JS `element.click()` if occluded.
- **sb_server**: `sb.click(selector)` — blind trust. No occlusion check, no JS fallback.

### 10. No character-by-character typing
- **browser-use**: CDP `keyDown`/`char`/`keyUp` per character with Shift modifier. Dispatches `input` and `change` events.
- **sb_server**: `sb.type(selector, text)` — Selenium send_keys. Works for most sites but fails on React-controlled inputs with validation.

### 11. No new tab auto-detection after clicks
- **browser-use**: Captures tab IDs before click, detects new ones, auto-switches, informs agent.
- **sb_server**: `_detect_new_tabs()` logs but doesn't auto-switch or inform the agent in the response.

### 12. No page change detection (PageFingerprint)
- **browser-use**: Computes `PageFingerprint` (URL + element count + text hash). Detects when a page hasn't changed after an action.
- **sb_server**: No fingerprinting. Agent has no signal that a click didn't work.

### 13. CDP mouse events vs Selenium click
- **browser-use**: Uses raw CDP `Input.dispatchMouseEvent` (mouseMoved → mousePressed → sleep 80ms → mouseReleased). More stealthy, more reliable on complex pages.
- **sb_server**: Uses SeleniumBase's `sb.click()` which wraps WebDriver click. Less stealthy.

### 14. No `extract` endpoint (LLM-powered page extraction)
- **browser-use**: `extract` action uses a separate LLM to extract structured data from the page.
- **sb_server**: No equivalent. Agent must use `/run` with custom Python.

### 15. No `dropdown_options` / `select_dropdown`
- **browser-use**: Dedicated endpoints for dropdown interaction with option listing and selection.
- **sb_server**: No dropdown-specific endpoints. Agent must use `/run` or `/evaluate` with custom JS.

### 16. No `wait` action
- **browser-use**: Explicit `wait(seconds)` action (max 30).
- **sb_server**: No dedicated wait. Agent would need `sleep` in `/run` code.

---

## Agent Behavior Gaps (from live testing)

### 17. Agent prefers MCP search over sb_server browser
- **Observed**: Agent used `search` (MCP tool) + `scrape` (MCP tool) + `curl` instead of sb_server. Never touched the persistent browser.
- **Why**: The MCP search returns structured JSON immediately. sb_server requires curl commands. Agent picks the shortest path.
- **Fix**: Teach agent that sb_server handles images and anti-bot sites that MCP tools can't. MCP scrape strips `<img>` tags.

### 18. Agent retries failed tools too aggressively
- **Observed**: 5 consecutive failed `curl -o` calls. System prompt says "Do NOT repeat the same action if it failed" but the agent retried anyway.
- **Root cause**: The loop detection is soft (escalating nudges, never blocks). Contrast with browser-use's hard fail cap at `max_failures=5`.

### 19. 5-minute timeout insufficient for image tasks
- **Observed**: Makima search + scrape 20 URLs + analyze 10 images = exceeded 300s timeout. Agent was mid-analysis when killed.
- **Impact**: Complex multi-step visual tasks never complete.

---

## What Works Now (verified 2026-05-06)

| Feature | Status | How |
|---------|--------|-----|
| Stateful browser session | Works | sb_server on localhost:9876, supervised |
| SoM visual labeling | Works | JS injection, 50-element cap, numbered boxes |
| Page text + images + links | Works | Every response includes page_data |
| Screenshot capture | Works | Auto-screenshot on every page change |
| Page stats | Works | Viewport, scroll position, element counts |
| Index-based click/type | Works | Use SoM label index from element_map |
| Tab management | Works | new_tab, switch_tab, close_tab, tabs |
| DuckDuckGo search | Works | Default search engine (Google blocks) |
| File download | Works | curl -sL -o /tmp/file URL |
| JavaScript evaluation | Works | /evaluate endpoint |
| Python code execution | Works | /run endpoint with pre-loaded sb |
| find_text | Works | Scrolls to and highlights text |
| CDP download tracking | Works | check_downloads endpoint |

---

## Priority Ranking (updated)

1. **analyze_image local files** — would unblock full vision-in-the-loop
2. **page change detection** — agent feedback loop (did my click work?)
3. **occlusion-aware clicking** — sb_server reliability
4. **external prompt templates** — iterate on behavior without recompile
5. **new tab auto-detection** — agent doesn't know when clicks open new tabs
6. **dropdown/select endpoints** — form interaction
7. **hard fail cap** — prevent infinite retry loops
8. **timeout increase for multi-step tasks** — 5min is too short
