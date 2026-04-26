# CUA (Computer Use Agent)

**Repo**: `reference/cua/` — Multi-model computer use with OpenAIs + Anthropic + Gemini + Qwen

## What It Is

A multi-model implementation of computer use agents. Handles the heterogeneity of 4 different model providers through separate agent_loop implementations, a callback system for cross-cutting concerns, and the `@register_tool` decorator pattern.

## Key Insights

### 1. Coordinate Normalization (0-1000)
- All models output coordinates in a [0, 1000] normalized range
- Denormalized at action time: `x = (x/1000) * screen_width`, `y = (y/1000) * screen_height`
- Resolution-independent — works on any screen size
- 5 lines of code. This is the single most impactful insight for desktop automation.
- Source: Google first, CUA adopted it exactly

### 2. Callback System
- **BudgetManagerCallback** — track and limit API costs
- **ImageRetentionCallback** — limit how many screenshots are kept in context
- **TelemetryCallback** — usage tracking and analytics
- **TrajectorySaverCallback** — save action sequences for replay/debugging
- **PIIAnonymizationCallback** — strip sensitive data from context
- Each callback is pluggable, independently configurable

### 3. Model-Specific Agent Loops
- Separate implementations for OpenAI, Anthropic, Gemini, Qwen
- Each handles provider-specific API quirks (tool format, image encoding, thinking)
- Common interface: `agent_loop(model, tools, callbacks) -> response`
- Shared tool registry across all loops

### 4. Tool Registry with Decorator Pattern
```python
@register_tool
def click(x: int, y: int, button: str = "left"):
    """Click at normalized coordinates (0-1000)"""
```
- Automatic tool discovery, schema generation, and documentation
- Type annotations -> JSON Schema conversion for LLM tool definitions

### 5. Smart Screenshot Resize
- Resize to multiples of 32 (model requirement for Qwen VL)
- Min pixels: 3,136, Max pixels: 12,845,056
- JPEG quality configurable, PNG for lossless when needed
- Base64 encoded for API transmission

### 6. Wait After Actions
- Explicit `wait()` tool — not just implicit delays
- Models learn to call it after page loads, form submissions, etc.
- 0.5-2 second typical wait, configurable

## What We've Implemented

| Feature | Where | Notes |
|---------|-------|-------|
| Coordinate normalization 0-1000 | `grounding.go:12` | `NormalizeCoords()` — "CUA/Google pattern: models output in 0-1000 space" |
| Desktop click with normalization | `agent_loop.go:691` | Applied to `desktop_click` |
| Wait tool | `agent_loop.go:731` | "from CUA/browser-use" |
| Separe agent instances | `agent_loop.go` | Parallel goroutines for web/code/desktop sub-agents |

## Gaps

| Priority | Feature | Effort | Why |
|----------|---------|--------|-----|
| P0 | Coordinate normalization for ALL desktop actions | Low | Currently only `desktop_click` normalizes. `desktop_type`, `desktop_scroll`, etc. still use raw pixels |
| P1 | Callback system for telemetry + image retention | Medium | Clean approach to cross-cutting concerns without polluting core loop |
| P1 | Image preprocessing (resize to multiples of 32) | Low | Better LLM compatibility, smaller payloads |
| P1 | Smart screenshots with min/max pixel limits | Low | Prevent tiny/bloated screenshots |
| P2 | Tool registry with decorator pattern | Medium | More elegant than our current string-based tool registration |
| P2 | Trajectory saver for session replay | Medium | Powerful debugging tool |
| P3 | Multi-model agent loops | High | We use single model (llama.cpp) by design |

### Key Architectural Insight
The callback system is CUA's most underappreciated idea. Instead of baking telemetry, image management, and budget tracking into the agent loop, they're independent plugins. This is the same pattern as pi-mono's extension hooks and Claude Code's hook system. We should adopt this for our own agent loop.
