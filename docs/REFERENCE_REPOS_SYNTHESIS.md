# Reference Repos Synthesis — Lessons for Auto-Developer-Orchestrator

Generated: 2026-04-20

Repos studied: OmniParser, Stagehand, CUA, open-computer-use, Agent-S, browser-use, google/computer-use-preview, gemini-cli, OS-Symphony

---

## Executive Summary

After analyzing 9 leading computer-use / browser-automation frameworks, the most impactful improvements for our system cluster around **7 themes**:

1. **Desktop grounding** — Vision model + OCR + coordinate normalization (not just xdotool)
2. **Screenshot pipeline** — Multi-method fallback + LLM-optimized preprocessing
3. **Better element detection** — Accessibility tree > DOM-only > vision-only
4. **Simpler agent architecture** — Flat loops beat deep hierarchies
5. **Action caching & self-healing** — Learn from successful interactions
6. **Watchdog / event-driven resilience** — Separate crash/popup/timeout handling
7. **Hierarchical memory** — Project-scoped, not just session-scoped

---

## 1. Element Detection & Grounding

### Current State (Our System)
- JS `labelerJS` draws red numbered boxes via CDP
- Flat 25-element list with spatial zones (top/mid/center/bottom)
- Scoring-based `resolveElement()` for ID/description/CSS matching
- No accessibility tree usage, no OCR, no visual grounding model

### Best Practices from Reference Repos

#### browser-use (Best-in-class element detection)
- **Hybrid approach**: Accessibility tree (primary) + DOM snapshot (positioning) + JS event listeners
- Detects JS click handlers for React/Vue/Angular (not just tag-based)
- **Paint-order filtering**: Removes visually occluded elements (reduces noise)
- **Shadow DOM piercing**: Handles web components natively
- Element format: `0-76` (frameId-backendNodeId) — stable across reflows
- Source: `browser_use/dom/service.py`, `browser_use/dom/serializer/clickable_elements.py`

#### Stagehand (Cleanest architecture)
- `captureHybridSnapshot()` merges DOM + AX tree into single view
- Zod-validated structured output: `{ elementId, description, method, arguments }`
- XPath + element ID dual selection
- Source: `stagehand/lib/handler/agentHandler.ts`, `stagehand/lib/dom/hybridSnapshot.ts`

#### OmniParser (Best vision-only detection)
- YOLO v8 for icon detection + Florence-2 for icon captioning + EasyOCR/PaddleOCR
- **Batch icon captioning** (128 at a time) — fast
- **Smart overlap resolution**: IoU threshold 0.7, OCR text prioritized over icon detection
- Coordinates normalized to [0,1] ratios
- Source: `OmniParser/util/omniparser.py`, `OmniParser/util/box_annotator.py`

#### Agent-S (SOTA grounding)
- **UI-TARS-1.5-7B** visual grounding model → coordinates from descriptions
- OCR + coordinate scaling from model space (1920x1080) to actual screen
- S3 dropped accessibility tree for pure visual grounding (simpler, cross-platform)
- Source: `Agent-S/agent_s3/agents/worker/grounding.py`

#### Google computer-use-preview (Simplest approach)
- Pure **coordinate-based** (0-1000 normalized range → screen dimensions)
- No DOM parsing at all — relies entirely on vision
- 5 lines of denormalization code
- Source: `computer-use-preview/python/demo.py`

### Recommended Improvements

| Priority | Improvement | Source | Effort |
|----------|-------------|--------|--------|
| P0 | Add accessibility tree extraction via CDP | browser-use, Stagehand | Medium |
| P0 | JS event listener detection for React/Vue | browser-use | Low |
| P1 | Paint-order filtering to remove occluded elements | browser-use | Medium |
| P1 | IoU-based overlap deduplication | OmniParser | Low |
| P2 | Batch icon captioning with vision model | OmniParser | High |
| P2 | Shadow DOM piercing | Stagehand, browser-use | Medium |
| P3 | Visual grounding model (UI-TARS) for desktop mode | Agent-S | High |

---

## 2. Agent Architecture

### Current State
- Orchestrator + ephemeral sub-agents (web, code, desktop)
- 3-minute timeout, 30 max tool rounds
- Compaction: 4 turns → keep 2

### Best Practices from Reference Repos

#### Agent-S: Simplicity Wins
- **S3 achieved 72.6% on OSWorld by REMOVING hierarchy** (S1/S2 had DAG-based planning)
- Single Worker agent handles all decisions — faster, simpler
- No external knowledge base or RAG in S3
- **Reflection agent** for cycle detection (key innovation): detects repetitive actions without suggesting specific fixes
- Source: `Agent-S/agent_s3/`

#### CUA: Model-Specific Loops
- Separate `agent_loop` implementations per model (OpenAI, Anthropic, Gemini, Qwen)
- **Callback system**: BudgetManager, ImageRetention, Telemetry, TrajectorySaver, PIIAnonymization
- `@register_tool` decorator pattern for extensible tool registry
- Source: `cua/cua-agent/agent.py`, `cua/cua-agent/loops/`

#### gemini-cli: Event-Driven Scheduler
- **Scheduler class** coordinates tool execution with policy checks
- Tool confirmation system for risky operations
- Async tool execution with abort signals
- Source: `gemini-cli/packages/core/src/scheduler/`

#### OS-Symphony: Specialized Agent Routing
- **Intelligent task routing**: GUI agent for visual, Code agent for bulk, Search agent for knowledge gaps
- Verification pattern: Code results must be verified via GUI before completion
- Source: `OS-Symphony/agent/`

### Recommended Improvements

| Priority | Improvement | Source | Effort |
|----------|-------------|--------|--------|
| P0 | Add reflection/cycle detection to agent loop | Agent-S | Low |
| P1 | Callback system for telemetry + image retention | CUA | Medium |
| P1 | Code/GUI verification pattern | OS-Symphony | Low |
| P2 | Tool registry with decorator pattern | CUA, gemini-cli | Medium |
| P2 | Consider flattening sub-agent hierarchy | Agent-S insight | High |
| P3 | Policy-based tool confirmation | gemini-cli | Medium |

---

## 3. Memory & Context Management

### Current State
- Session-scoped only, no persistence
- Compaction: keep last 2 turns from 4
- No project-scoped or user-scoped memory

### Best Practices from Reference Repos

#### gemini-cli (Best memory system)
- **Hierarchical memory**: global → extension → project → user-project
- Automatic skill extraction from conversation patterns
- Memory patches for incremental updates
- Lock coordination for concurrent access
- Source: `gemini-cli/packages/core/src/config/memory.ts`

#### Agent-S (Reflection Memory)
- Trajectory storage with perceptual hashing (SSIM)
- Loop detection via visual similarity between screenshots
- Milestone-based state tracking
- Source: `Agent-S/agent_s3/agents/worker/reflection.py`

#### Google computer-use-preview (Context optimization)
- Keep only **last 3 turns with screenshots**
- Screenshot history management to prevent context bloat
- Simple but effective
- Source: `computer-use-preview/python/demo.py`

#### browser-use (Structured memory)
- JSON format with explicit `memory` field for reasoning
- Page statistics injected into context (links, iframes, interactive count)
- Source: `browser_use/agent/prompts.py`

### Recommended Improvements

| Priority | Improvement | Source | Effort |
|----------|-------------|--------|--------|
| P0 | Limit screenshot history to last 3 turns | Google | Low |
| P1 | Add page statistics to LLM context | browser-use | Low |
| P1 | Visual similarity loop detection | Agent-S | Medium |
| P2 | Project-scoped memory persistence | gemini-cli | High |
| P2 | Skill extraction from repeated patterns | gemini-cli | High |
| P3 | Milestone-based state tracking | Agent-S | Medium |

---

## 4. Error Recovery & Resilience

### Current State
- Retry once after reconnect for Click/Type/Scroll/Navigate
- `closeStaleTabs()` for orphaned Chrome tabs
- Chrome profile lock cleanup via `/tmp` bind mount
- 5 consecutive failures → force yield_artifact

### Best Practices from Reference Repos

#### browser-use (Best watchdog architecture)
- **Event-driven watchdog system**: CrashWatchdog, DOMWatchdog, SecurityWatchdog, PopupWatchdog
- Each watchdog listens to specific events on event bus (`bubus`)
- Network request tracking with timeouts (10s default)
- Clean separation of concerns
- Source: `browser_use/browser/watchdogs/`

#### open-computer-use (Best fallback chain)
- **4-tier screenshot fallback**: mss → pyautogui → scrot → ImageMagick
- X server recovery with `recover_display()` for Docker environments
- Command-specific timeouts (click: 30s, screenshot: 60s)
- Anti-detection: random viewport sizes, user agent rotation
- Source: `open-computer-use/ai_agent_server.py`

#### Stagehand (Best self-healing)
- **Action caching**: Successful actions cached by instruction + URL + variables (SHA-256 key)
- Self-healing for stale element references — automatically re-detects
- Structured error types for different failure modes
- Source: `stagehand/lib/handler/actHandler.ts`

#### CUA (Best callback system)
- BudgetManagerCallback — track costs
- ImageRetentionCallback — limit screenshot storage
- TelemetryCallback — usage tracking
- TrajectorySaverCallback — session replay
- PIIAnonymizationCallback — privacy
- Source: `cua/cua-agent/agent.py`

### Recommended Improvements

| Priority | Improvement | Source | Effort |
|----------|-------------|--------|--------|
| P0 | Action caching (instruction + URL → element) | Stagehand | Medium |
| P1 | Separate watchdog goroutines for crash/popup/timeout | browser-use | Medium |
| P1 | Multi-method screenshot fallback | open-computer-use | Low |
| P1 | Self-healing stale element references | Stagehand | Medium |
| P2 | Command-specific timeout configuration | open-computer-use | Low |
| P2 | Trajectory/session replay for debugging | CUA | Medium |

---

## 5. Desktop / Computer-Use Automation

### Current State (Our System)
- X11 xdotool for mouse/keyboard/screenshot (Go backend)
- Single screenshot method: `xdotool` + `import` (ImageMagick)
- No visual grounding model — relies on SoM labels from JS (browser only)
- No OCR for desktop apps — can only detect elements in Chrome
- Coordinate system: raw pixels, no normalization
- No desktop element detection outside of browser context
- No multi-app workflow support (app switching, window management)

### Desktop Screenshot Pipeline

#### open-computer-use (Best fallback chain)
**4-tier screenshot fallback with auto-recovery**:
```
1. mss (fastest, primary) → 2. pyautogui → 3. scrot → 4. ImageMagick import
```
- Each method checks for X server errors and triggers `recover_display()` if needed
- XAUTHORITY and DISPLAY env vars handled per method
- 5-second timeout per method, auto-retry with recovery
- **Image preprocessing for OCR**: denoise (fastNlMeansDenoising) → CLAHE contrast → adaptive threshold → dilation → sharpen
- Source: `open-computer-use/docker/ai-desktop/ai_agent_server.py`

#### CUA (Cleanest screenshot API)
```python
# PIL.ImageGrab with format options
async def screenshot(format="png", quality=95):
    screenshot = ImageGrab.grab()
    # PNG (lossless) or JPEG (lossy, configurable quality)
    # Smart resizing: factor=32, min_pixels=3136, max_pixels=12,845,056
    # Base64 encoded for transmission
```
- `smart_resize()` from qwen-vl-utils ensures dimensions are multiples of 32 (model requirement)
- Min/max pixel limits prevent both tiny and huge images
- Source: `cua/libs/python/computer-server/computer_server/handlers/linux.py`

#### Agent-S (Screenshot for grounding model)
```python
# pyautogui.screenshot() → resize to max 2400px → PNG → base64
# Optional compression: resize to half + WEBP format
screenshot = pyautogui.screenshot()
screenshot = screenshot.resize((scaled_w, scaled_h))
buffer = BytesIO(); screenshot.save(buffer, format="PNG")
```
- Resizes to fit UI-TARS model context (max 2400px dimension)
- Optional WEBP compression for smaller payloads
- Source: `Agent-S/gui_agents/s3/cli_app.py`

### Desktop Element Grounding (Detecting What's On Screen)

#### OmniParser (Best general-purpose screen parser)
**Pipeline**: YOLO detection → Florence-2 captioning → OCR → merge → annotate

```
1. OCR (EasyOCR + PaddleOCR): Detect text regions with bounding boxes
2. YOLO v8: Detect interactive elements (buttons, icons, inputs, checkboxes)
3. Florence-2: Caption each detected icon ("search icon", "close button")
   - Batch size 128, ~0.25s for 128 icons on GPU
4. Merge: IoU threshold 0.7, OCR text prioritized over icon detection
5. Annotate: Numbered red boxes with smart text positioning (4 fallback positions)
```

**Output format**:
```json
[
  {"type": "text", "bbox": [0.1, 0.2, 0.3, 0.1], "interactivity": false, "content": "Search"},
  {"type": "icon", "bbox": [0.4, 0.2, 0.05, 0.05], "interactivity": true, "content": "search icon"}
]
```
- All coordinates normalized to [0,1] ratios (relative to image size)
- Box overlay ratio scales text/thickness based on `max(image.size) / 3200`
- Configurable: BOX_TRESHOLD=0.05, iou_threshold=0.7, batch_size=128
- ~4GB GPU VRAM for Florence-2 alongside llama.cpp on RTX 4090
- Source: `OmniParser/util/omniparser.py`, `OmniParser/util/box_annotator.py`, `OmniParser/util/utils.py`

#### Agent-S / OS-Symphony (VLM-based grounding)
**UI-TARS visual grounding model** converts descriptions → coordinates:
```python
def generate_coords(ref_expr, obs):
    # Prompt: "Query: {ref_expr}\nOutput only the coordinate of one point."
    # Model sees screenshot + query
    # Returns [x, y] in model coordinate space (e.g., 1920x1080)
    coords = re.findall(r"\d+", response)[:2]
    return [resize_x(coords[0]), resize_y(coords[1])]  # Scale to actual screen
```
- Separate grounding model (UI-TARS-1.5-7B) from decision model
- Coordinate scaling: `x_actual = x_model * actual_width / model_width`
- Temperature=0.05 for deterministic coordinate output
- Source: `Agent-S/gui_agents/s3/agents/grounding.py`, `OS-Symphony/mm_agents/os_symphony/agents/grounder_agent.py`

#### OS-Symphony (Text Span Agent for precise text targeting)
**OCR + LLM for word-level coordinate mapping**:
```python
def generate_text_coords(phrase, obs, alignment=""):
    # 1. Get OCR elements from screenshot (pytesseract/EasyOCR)
    ocr_table, ocr_elements = ocr_processor.get_ocr_elements(screenshot, "easyocr")
    # 2. Ask LLM: "Which word ID matches '{phrase}'?"
    # 3. Return precise coordinates with alignment (start/end of word)
    if alignment == "start":
        return [elem["left"], elem["top"] + elem["height"] // 2]
    elif alignment == "end":
        return [elem["left"] + elem["width"] + 0.15 * elem["height"], ...]
```
- Handles text selection (click between specific words)
- Alignment-aware: "start" vs "end" of phrase
- Source: `OS-Symphony/mm_agents/os_symphony/agents/os_aci.py`

#### open-computer-use (CV-only element detection)
**3-method detection with priority merging**:
```
Priority 3 (highest): OCR text detection (pytesseract with preprocessing)
Priority 2: Color-based clickable detection (HSV ranges for blue/green/red/gray buttons)
Priority 1 (lowest): Contour-based UI element detection (Canny edge + findContours)
```
- Merge: Sort by priority, overlap >50% → keep higher priority
- Element format: `"{method}_{type}_{index}"` with center coordinates
- Source: `open-computer-use/docker/ai-desktop/ai_agent_server.py`

### Desktop Mouse/Keyboard Interaction

#### All repos use pyautogui or platform-native APIs

| Repo | Library | Platform | Notes |
|------|---------|----------|-------|
| Agent-S | pyautogui | Linux/macOS | Unicode via pyperclip+paste |
| OS-Symphony | pyautogui via HTTP | Linux/macOS/Windows | HTTP server in VM, pyautogui on server |
| CUA | pynput | Linux/macOS | Async, WebSocket-based |
| open-computer-use | pyautogui + xdotool | Linux | Platform-specific: xdotool (Linux), PowerShell user32.dll (Windows), Swift CoreGraphics (macOS) |
| Our system | xdotool via docker exec | Linux (Docker) | Go backend, TTY mode exec |

**Key insight**: pyautogui/pynput are more reliable than xdotool for complex operations (Unicode text, multi-key combos, drag). xdotool is fine for basic click/type/scroll.

**Unicode text pattern** (Agent-S):
```python
if has_unicode(text):
    pyperclip.copy(text)
    pyautogui.hotkey('ctrl', 'v')  # Paste instead of type
else:
    pyautogui.write(text)
```

### Coordinate Normalization

| Repo | Coordinate Space | Normalization | Scaling |
|------|-----------------|---------------|---------|
| Google | 0-1000 | `x / 1000 * width` | Simple linear |
| CUA | 0-1000 | Same as Google | Model outputs normalized coords |
| OmniParser | 0.0-1.0 | `x_ratio * width` | Ratio-based |
| Agent-S | Model resolution | `x * actual / model` | Resize from model space |
| Our system | Raw pixels | None | Direct xdotool coords |

**Recommendation**: Adopt 0-1000 normalization (Google/CUA pattern). Simple, works with any screen resolution, easy to implement in Go.

### Multi-App / Window Management

#### Agent-S
```python
# Linux: wmctrl for window management
switch_applications(app_code):
    return "pyautogui.hotkey('win'); pyautogui.write(app_code); pyautogui.press('enter')"

open(app_or_filename):
    return "pyautogui.hotkey('win'); pyautogui.write(app); pyautogui.press('enter')"
```

#### OS-Symphony
- Same pattern: Super key → type app name → Enter
- `wmctrl` for direct window focus by name

#### CUA
- `pywinctl` for cross-platform window control
- `webbrowser` module for URL opening
- `DioramaComputer` for app-specific workflows

### Recommended Desktop Improvements

| Priority | Improvement | Source | Effort |
|----------|-------------|--------|--------|
| P0 | Add OmniParser-style screen parsing (YOLO+OCR) for desktop mode | OmniParser | High |
| P0 | Coordinate normalization 0-1000 for all desktop actions | Google, CUA | Low |
| P0 | Multi-method screenshot fallback (xdotool → import → pyautogui) | open-computer-use | Low |
| P1 | OCR text detection (pytesseract) for desktop apps | OS-Symphony, open-computer-use | Medium |
| P1 | Unicode text input via clipboard paste | Agent-S | Low |
| P1 | Image preprocessing for LLM (resize, compress, optimize) | CUA, Agent-S | Low |
| P2 | Text span agent for precise text selection | OS-Symphony | Medium |
| P2 | VLM grounding model (UI-TARS) for description→coordinate | Agent-S | High |
| P2 | Window/app management (wmctrl, app switching) | Agent-S, OS-Symphony | Medium |
| P3 | Florence-2 icon captioning for desktop element descriptions | OmniParser | High |

---

## 6. Browser Automation Specifics (See Section 1 for Element Detection)

### Current State
- chromedp (Go) with CDP
- HTTP tab creation via PUT /json/new
- JS-based click/type via chromedp.Evaluate
- Fresh Tab Architecture (keep-alive context)

### Best Practices from Reference Repos

#### Stagehand (Most complete browser toolkit)
- **Three operations**: `act(instruction)` → execute, `extract(schema)` → structured data, `observe()` → element list
- Unified Page abstraction over Playwright/Puppeteer/CDP
- Frame registry for iframe support
- Source: `stagehand/lib/`

#### browser-use (Most robust)
- HTTP API tab creation (same as ours — PUT /json/new)
- Shared keep-alive context (same pattern as ours)
- **Additional**: Crash detection via CDP events, popup handling, download tracking
- Source: `browser_use/browser/session.py`

#### Google (Cleanest coordinate mapping)
- Coordinate normalization 0-1000 → screen dimensions
- Smart text clearing before typing
- Wait after navigation (0.5s)
- Source: `computer-use-preview/python/demo.py`

### Recommended Improvements

| Priority | Improvement | Source | Effort |
|----------|-------------|--------|--------|
| P0 | `observe()` operation — return structured element list without acting | Stagehand | Low |
| P1 | Popup/dialog auto-dismiss | browser-use | Low |
| P1 | Download tracking and file management | browser-use | Medium |
| P1 | Wait-after-navigation (0.5s) for state stabilization | Google | Low |
| P2 | `extract(schema)` for structured data extraction | Stagehand | Medium |

---

## 7. Prompt Engineering

### Best Practices Across Repos

#### open-computer-use (Most aggressive prompts)
- "BE PROACTIVE: Complete tasks without asking permission"
- "MAKE DECISIONS: Use context and defaults when details missing"
- "NO HEDGING: State facts clearly without unnecessary qualifiers"
- "Understand → Act → Answer, skip explanations"

#### Agent-S (Reflection prompts)
- Post-action reflection: "Did this action make progress? Am I stuck in a loop?"
- **Critical**: Reflection detects repetition without suggesting specific fixes (prevents bias)

#### browser-use (Context optimization)
- Page statistics: `{ links: 5, iframes: 1, interactive: 23, total: 156 }`
- Memory field in structured output for chain-of-thought
- Constraint enforcement for hard rules

#### Google (Minimal but effective)
- Temperature=1.0 for maximum creativity
- Max 8192 output tokens
- `thinking_config` with `include_thoughts=True`

---

## 8. Tool Definitions Comparison

| Tool | Our System | browser-use | CUA | OS-Symphony |
|------|-----------|-------------|-----|-------------|
| Click | ID + coords | ID + coords | Coords only | Coords only |
| Type | ID + text | ID + text + clear | Text only | Text + paste |
| Scroll | Direction | Pages + target | x,y delta | Direction |
| Screenshot | On-demand | Auto + cached | Auto + resized | Auto |
| Navigate | URL | URL + tab | N/A | App launch |
| Bash | Yes | No | Via sandbox | Via code agent |
| Search | No | Yes (page) | No | Via search agent |
| Extract | No | Yes (query) | No | No |
| Wait | No | Yes | Yes | Yes |
| Hotkey | Via xdotool | No | Yes | Yes |

### Missing Tools We Should Add
1. **`wait(duration)`** — Explicit wait between actions
2. **`extract(query)`** — Structured data extraction from page
3. **`search_page(text)`** — Find text on current page
4. **`scroll_to(element)`** — Scroll element into view
5. **`drag(from, to)`** — Drag and drop support

---

## 9. Top 15 Actionable Improvements (Ranked by Impact/Effort)

### Desktop / Computer-Use Improvements

| # | Improvement | Impact | Effort | Source |
|---|-------------|--------|--------|--------|
| 1 | OmniParser-style screen parsing (YOLO+OCR+captioning) for desktop mode | Very High | High | OmniParser |
| 2 | Coordinate normalization 0-1000 for all desktop actions | High | Low | Google, CUA |
| 3 | Multi-method screenshot fallback (xdotool → import → pyautogui) | High | Low | open-computer-use |
| 4 | OCR text detection for desktop apps (tesseract in sandbox) | High | Medium | OS-Symphony, open-computer-use |
| 5 | Image preprocessing for LLM (resize to multiples of 32, JPEG compression) | Medium | Low | CUA, Agent-S |
| 6 | Unicode text input via clipboard paste (pyperclip pattern) | Medium | Low | Agent-S |
| 7 | Window/app management (wmctrl in sandbox) | Medium | Low | Agent-S, OS-Symphony |

### Browser Improvements

| # | Improvement | Impact | Effort | Source |
|---|-------------|--------|--------|--------|
| 8 | Add accessibility tree extraction via CDP `Accessibility.getFullAXTree()` | High | Medium | browser-use, Stagehand |
| 9 | Cycle detection via screenshot similarity (SSIM/perceptual hash) | High | Low | Agent-S |
| 10 | Action caching: hash(instruction + URL) → cached element selector | High | Medium | Stagehand |

### Agent Architecture Improvements

| # | Improvement | Impact | Effort | Source |
|---|-------------|--------|--------|--------|
| 11 | Limit screenshot history to last 3 turns in context | Medium | Low | Google |
| 12 | Reflection prompt after each action ("Did I make progress?") | Medium | Low | Agent-S |
| 13 | Separate watchdog goroutines (crash, popup, timeout) | Medium | Medium | browser-use |
| 14 | Add `wait()`, `extract()`, `search_page()` tools | Medium | Low | Multiple |
| 15 | Page statistics in LLM context | Low | Low | browser-use |

---

## 10. Architecture Anti-Patterns to Avoid

Based on what the reference repos learned the hard way:

1. **Don't use deep hierarchical planning** — Agent-S S3 beat S1/S2 by removing DAG planning entirely. Flat loops with reflection > deep trees.
2. **Don't rely on vision-only for web** — Google's coordinate-only approach fails on dynamic pages. Always combine with DOM/AX data.
3. **Don't skip accessibility tree** — browser-use proves AX tree is more reliable than DOM-only for element detection.
4. **Don't use single screenshot method** — open-computer-use's 4-tier fallback prevents failures.
5. **Don't keep all screenshots in context** — Google's 3-turn limit prevents context bloat.
6. **Don't suggest fixes in reflection prompts** — Agent-S found that suggesting specific fixes biases the model. Just detect the loop.
7. **Don't use raw pixel coordinates** — Normalize to 0-1000 (Google/CUA) or 0-1 (OmniParser). Raw pixels break on resolution changes.
8. **Don't send full-resolution screenshots to LLM** — CUA resizes to multiples of 32 with pixel limits. Agent-S caps at 2400px. Full-res wastes context tokens.
9. **Don't type Unicode characters directly** — Agent-S uses clipboard paste (pyperclip+ctrl+v) instead of character-by-character typing. Direct typing fails on special characters.
10. **Don't assume desktop elements are clickable** — OmniParser separates text (interactivity=false) from icons (interactivity=true). Not everything with a bounding box should be clicked.
