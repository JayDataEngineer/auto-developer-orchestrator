# Feature Parity Gaps — browser-use vs Our Agent

Documented from deep codebase analysis of browser-use (reference/repos/browser-use/) and live testing. Updated 2026-05-06.

---

## Feature Comparison Matrix

| Feature | browser-use | sb_server | Status |
|---------|-------------|-----------|--------|
| Stateful browser sessions | Playwright (persistent) | SeleniumBase HTTP server | **Done** |
| SoM visual labeling | DOM tree with indices | JS injection, numbered boxes, 50-cap | **Done** |
| Auto-screenshot on page change | Yes | Every response includes screenshot_path | **Done** |
| Page fingerprinting | URL + element count + text hash | URL + element count + text hash | **Done** |
| Occlusion-aware clicking | elementFromPoint check + JS fallback | elementFromPoint check + JS fallback | **Done** |
| CDP character-by-character typing | keyDown/char/keyUp per char | Native input setter + React event dispatch | **Done** |
| New tab auto-detection | Capture handles before/after | Auto-switch to new tab + inform agent | **Done** |
| Dropdown/select | Dedicated endpoints | dropdown_options + select_dropdown | **Done** |
| Explicit wait | wait(seconds, max 30) | wait(seconds, max 30) | **Done** |
| Extract structured data | LLM-powered extraction | JS-based headings/paragraphs/tables/forms | **Done** |
| Tab management | new/switch/close/list | new_tab/switch_tab/close_tab/tabs | **Done** |
| File download tracking | DownloadsWatchdog | CDP download behavior + check_downloads | **Done** |
| File serving for vision | N/A | GET /file/<path> → base64 data URI | **Done** |
| Search | Google (via Playwright) | DuckDuckGo (Google blocks) | **Done** |
| Cookie persistence | Across actions | Across actions (persistent browser) | **Done** |
| JavaScript evaluation | executeScript | /evaluate endpoint | **Done** |
| Python code execution | N/A | /run endpoint with pre-loaded sb | **Done** |
| Anti-bot bypass | Undetected-chromedriver | sb_agent.py --stealth (UC Mode) | **Done** |
| Hard fail cap | max_failures=5 | MaxConsecutiveFails=5 circuit breaker | **Done** |
| Per-tool retry limit | N/A | MaxRetriesPerTool=3 | **Done** |
| External prompt template | system_prompt.md | config/prompt.md with {{tools}} | **Done** |
| Page stats | N/A | viewport, scroll, element counts | **Done** |
| Find text on page | N/A | /find_text scrolls to and highlights | **Done** |
| Image extraction | N/A | /extract_images with alt text | **Done** |

---

## Architecture Differences (intentional)

| Aspect | browser-use | Our approach | Why |
|--------|-------------|-------------|-----|
| Browser engine | Playwright (Python) | SeleniumBase CDP (Python HTTP server) | Anti-bot bypass, UC Mode |
| Agent loop | Python async | Go agent loop + SSE | Unified with code/desktop tools |
| Communication | Direct function calls | HTTP via curl (localhost:9876) | Works through bash tool, no new Go code needed |
| LLM | OpenAI/GPT-4 | llama.cpp/OpenRouter/Gemini | Local-first, multi-provider |
| Vision | Screenshot → GPT-4V | Screenshot → MCP analyze_image | Decoupled, can use any vision model |

---

## Remaining Minor Gaps

### 1. LLM-powered extraction (vs rule-based)
- **browser-use**: Uses a separate LLM call to extract structured data from pages
- **sb_server**: Uses JS-based extraction (headings, paragraphs, tables, forms)
- **Impact**: Low. Agent can use /run with custom extraction logic when needed.
- **Priority**: Low

### 2. Drag and drop
- **browser-use**: CDP mouse events for drag-and-drop interactions
- **sb_server**: Not implemented. Agent can use /evaluate with custom JS.
- **Priority**: Low

### 3. Iframe support
- **browser-use**: Cross-iframe element detection
- **sb_server**: No iframe traversal
- **Priority**: Low (most modern sites don't use iframes for interactive content)

### 4. Multi-select dropdowns
- **browser-use**: Support for <select multiple>
- **sb_server**: dropdown_options reports `multiple` flag, but select_dropdown only selects one option
- **Priority**: Low

---

## Known Issues

- **Sandbox image stale**: Running sandboxes use old image. Need `docker build` + recreate to get latest sb_server.
- **analyze_image local files**: MCP vision can't reach sandbox filesystem. Workaround: GET /file/<path> returns data URI.
- **Vision model fails on some PNGs**: "Unable to infer channel dimension format" on unusual color channels.
- **DeepSeek stream errors**: Transient INTERNAL_ERROR from DeepSeek API causes agent loop to terminate. Retry at caller level needed.
- **Project path jules://**: Projects imported from Jules have `jules://` paths that don't resolve. Manual DB fix needed.

## End-to-End Test Results (2026-05-06)

**Test**: "show me makima from chainsaw man" via POST /api/pux/prompt
- **Search**: 3 search queries returned 30 results (Zerochan, Fandom, MyAnimeList, AlphaCoders, etc.)
- **Scrape**: 4 pages scraped (Fandom gallery, Zerochan, MyAnimeList, AlphaCoders)
- **Browser**: sb_server navigate + extract_images on Zerochan
- **Download**: 5 Makima images saved to /tmp/ (7-10KB avif thumbnails)
- **Issue**: DeepSeek API stream error on round 9 (transient, not code bug)
- **Dedup fix**: Tool call ID deduplication added — no more duplicate tool_call_id errors
