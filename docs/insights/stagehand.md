# Stagehand

**Repo**: `reference/stagehand/` — Cleanest browser automation toolkit architecture (Browserbase)

## What It Is

A browser automation framework with three core operations: `act(instruction)` to execute, `extract(schema)` for structured data, and `observe()` to return element lists. Known for action caching, self-healing stale elements, and a unified Page abstraction over Playwright/Puppeteer/CDP.

## Key Insights

### 1. Three Core Operations
- **`act(instruction)`** — interpret natural language instruction, find element, execute action
- **`extract(schema)`** — Zod-validated structured data extraction from page content
- **`observe()`** — return structured element list **without acting** (exploration mode)
- This is the cleanest mental model for browser agents

### 2. Action Caching (SHA-256)
- Cache key: `SHA256(instruction + URL + variables)`
- Successful element selectors stored and reused
- Dramatically reduces re-detection on repeated actions
- Cache invalidation on URL change or element change

### 3. Self-Healing Elements
- Stale element references automatically re-detected
- Re-snapshot page, match by tag + text similarity + position
- Structured error types for different failure modes
- Much better than "retry once on reconnect"

### 4. Hybrid Snapshot (DOM + AX Tree)
- `captureHybridSnapshot()` merges DOM and accessibility tree
- Single unified view of all interactive elements
- XPath + element ID dual selection for robustness

### 5. Zod-Validated Structured Output
- Element format: `{ elementId, description, method, arguments }`
- Type-safe, schema-validated
- Methods: click, type, scroll, select option, drag & drop

### 6. Frame Registry
- Full iframe support with frame-specific element IDs
- Cross-frame navigation and element interaction

## What We've Implemented

| Feature | Where | Notes |
|---------|-------|-------|
| `selfHealElement()` | `agent_loop.go:432` | "Stagehand pattern: re-snapshot + match by tag/text similarity" |
| `observe` tool | `agent_loop.go:1803` | Screenshot + DOM + vision description |
| `macroObserve()` | `agent_loop.go:726` | "Stagehand pattern: screenshot + DOM + vision in one call" |

## Gaps

| Priority | Feature | Effort | Why |
|----------|---------|--------|-----|
| P0 | Action caching (instruction + URL -> element) | Medium | Reduces LLM calls for repeated interactions. Stagehand's best feature |
| P0 | Full `observe()` — structured element list without acting | Low | We have the tool but could make output more structured |
| P0 | Self-healing stale elements (full implementation) | Medium | Current `selfHealElement` is basic similarity matching |
| P2 | `extract(schema)` for structured data extraction | Medium | Enable "get all prices", "extract table as JSON" etc. |
| P2 | Zod-like schema validation for tool outputs | Low | Type safety for element selection |
| P2 | Frame/iframe registry | Medium | We don't handle iframes well currently |

### Key Architectural Insight
Stagehand's **observe/act/extract** mental model is cleaner than our current "browse + click + type + read" sprawl. Consolidating into fewer, higher-level tools with structured output would reduce LLM decision complexity. The action caching is elegant — it's essentially "learn from what worked before" implemented as a simple hash lookup.
