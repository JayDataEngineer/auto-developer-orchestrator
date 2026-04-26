# Google Computer Use Preview

**Repo**: `reference/computer-use-preview/` — Minimal, coordinate-based computer use

## What It Is

Google's simplest possible computer use demo. Pure coordinate-based interaction (0-1000 range), no DOM parsing, no accessibility tree, no vision model. Exists as a reference for the minimal viable approach to computer use agents.

## Key Insights

### 1. Pure Coordinate-Based (Simplest Approach)
- No element detection of any kind
- Model sees a screenshot, outputs raw coordinates in [0, 1000] range
- Denormalized at action time: `x = (x / 1000) * screen_width`
- 5 lines of denormalization code

### 2. 3-Turn Screenshot Limit
- Keep only the last 3 turns with screenshots in context
- Prevents context window bloat
- Simple but effective — model doesn't need to see every past screenshot

### 3. Smart Text Clearing
- Clear existing text before typing (not append)
- Click field -> Ctrl+A -> type new text
- Prevents garbage text accumulation from partial edits

### 4. Wait After Navigation
- 0.5 second wait after navigating to a new URL
- Ensures page state stabilization
- Prevents "element not found" from race conditions

### 5. Thinking Config
- `thinking_config` with `include_thoughts=True`
- Temperature=1.0 for maximum creativity
- Max 8192 output tokens

## What We've Implemented

| Feature | Where | Notes |
|---------|-------|-------|
| Coordinate normalization | `grounding.go:12` | 0-1000 -> screen pixels |
| Desktop click normalization | `agent_loop.go:691` | Applied to `desktop_click` |
| Wait tool | `agent_loop.go:731` | Explicit wait between actions |

## Gaps

| Priority | Feature | Effort | Why |
|----------|---------|--------|-----|
| P0 | Limit screenshot history to last 3 turns | Low | Prevents context bloat, easy to implement |
| P1 | Smart text clearing before typing | Low | Ctrl+A before typing would fix partial-edit issues |
| P1 | Wait after navigation (0.5s) | Low | Race condition prevention |
| P2 | Thinking config (temperature, max tokens) | Low | Tune model parameters for agent use |
