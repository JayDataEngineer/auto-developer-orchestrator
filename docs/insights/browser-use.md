# browser-use

**Repo**: `reference/browser-use/` — Python async CDP browser automation library

## What It Is

The best-in-class browser automation library for AI agents. Hybrid element detection (accessibility tree + DOM + JS event listeners), event-driven watchdog architecture, and robust CDP session management.

## Key Insights

### 1. Hybrid Element Detection (Best-in-Class)
- **Accessibility tree** as primary source (most reliable for interactive elements)
- **DOM snapshot** for positioning and element IDs
- **JS event listeners** detected for React/Vue/Angular (not just tag-based heuristics)
- Element format: `frameId-backendNodeId` — stable across page reflows

### 2. Paint-Order Filtering
- Removes visually occluded elements before presenting to the LLM
- Significantly reduces noise in element lists
- Uses `checkVisibility()` API in Chrome

### 3. Shadow DOM Piercing
- Handles Web Components natively
- Critical for modern web apps (React, Lit, etc.)

### 4. Event-Driven Watchdog System
- **CrashWatchdog** — detects page crashes via CDP events
- **DOMWatchdog** — monitors DOM stability after actions
- **SecurityWatchdog** — validates navigation/redirect safety
- **PopupWatchdog** — auto-dismisses dialogs/popups
- Clean separation of concerns via event bus (`bubus`)

### 5. Structured Memory & Context
- JSON format with explicit `memory` field for chain-of-thought
- Page statistics injected into context: `{ links: 5, iframes: 1, interactive: 23 }`

## What We've Implemented

| Feature | Where | Notes |
|---------|-------|-------|
| Paint-order visibility check | `labeler.js:90` | `isVisible()` using `checkVisibility()` |
| `scroll_page` tool | `agent_loop.go:742` | "from browser-use" |
| `wait` tool | `agent_loop.go:731` | Explicit wait between actions |

## Gaps

| Priority | Feature | Effort | Why |
|----------|---------|--------|-----|
| P0 | Accessibility tree extraction via CDP | Medium | Most reliable element detection method. Every browser-use benchmark shows AX > DOM > vision |
| P0 | JS event listener detection | Low | React/Vue click handlers aren't on `<div>` tags. browser-use reads them from CDP's `DOMDebugger.getEventListeners` |
| P1 | Paint-order filtering for occluded elements | Medium | We have the `isVisible()` check but not full paint-order filtering |
| P1 | Watchdog goroutines (crash/popup/timeout) | Medium | Would make browser sessions much more resilient |
| P1 | Popup/dialog auto-dismiss | Low | `window.alert()`/`window.confirm()` blockers |
| P2 | Page statistics in LLM context | Low | Help the model understand page complexity |
| P2 | Shadow DOM piercing | Medium | Web components are increasingly common |
