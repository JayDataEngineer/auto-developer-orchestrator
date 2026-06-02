# TUI Diagnosis — Where It Falls Short

Last audited: 2026-06-02

## ✅ FIXED

### 1. ~~Tool UIs registered but never rendered~~ — FIXED
`AssistantMessage` now uses `MessagePrimitive.Parts` with `part.toolUI` resolution. All 18 registered tool UIs (BashToolUI, DelegateToolUI, FileEditToolUI, etc.) render correctly via the library pipeline.

### 2. ~~Window size = 1 destroys scrollback~~ — FIXED
Now `windowSize={2} windowOverscan={4}` — 6 live messages, older ones graduate to `<Static>` (terminal scrollback).

### 3. ~~Anti-flash hack fights Ink's rendering~~ — FIXED
No terminal clear on Enter. Composer uses `ComposerPrimitive.Input` with `onSubmit` callback. Flash was caused by `console.log` writes while Ink controlled stdout, not by the library.

### 5. ~~Double `useInput` registration~~ — FIXED
Composer-bar has no `useInput` hooks. Uses `ComposerPrimitive.Input` with `onSubmit` for all input handling.

### 8. ~~No `LoadingPrimitive.*`~~ — FIXED
Both message-level (`LoadingPrimitive.Root` in `AssistantMessage`) and thread-level (in `Thread`) use library primitives.

### 11. ~~`ApprovalDialog` is dead~~ — FIXED
File deleted. HITL routes through `QuestionDialog` and `DecisionDialog`.

### 12. ~~`ReasoningBlock` and `ReasoningAccordion` are dead~~ — FIXED
Files deleted. Reasoning rendered as collapsed single-line via `BLOCKQUOTE_BAR` + last reasoning line.

### 14. ~~Kitty keyboard protocol disabled~~ — FIXED
Now enabled conditionally via `supportsKittyKeyboard()` — kitty, WezTerm, and foot terminals get enhanced keybindings (Shift+Enter, Alt+B/F/D word navigation, CSI-u protocol). Other terminals fall back to disabled.

---

## 🔴 BUGS (causing visible breakage)

### 4. Stdin monkey-patching for Linux backspace
`main.tsx:34-52`: Monkey-patches `process.stdin.read` to rewrite `\x7f→\b` (Linux backspace) and `\n→\r` (Ctrl+J → Enter). Well-documented and functional, but fragile — it mutates a built-in stream before Ink processes it. If Ink ever handles `\x7f` natively on Linux, this should be removed.

**Severity:** Low — works correctly, just not the ideal approach.

### 6. SSE events are completely untyped
`pux-chat-adapter.ts`: `parsed` is `Record<string, unknown>` with unchecked casts throughout the switch statement. Every `data.something as string` silently produces `undefined` if the backend changes field names. No schema validation, no type guards, no error recovery for malformed events.

**Severity:** Medium — maintenance risk, no runtime breakage.

---

## 🟡 ARCHITECTURE NOTES (intentional or low priority)

### 7. ~~No `useNotification()`~~ — N/A
The library (`@assistant-ui/react-ink` v0.0.18) does **not** export a `useNotification` hook. This was a misdiagnosis. Terminal bell on completion would be nice but requires a custom implementation via `useAuiEvent`.

### 9. Custom status bar instead of `StatusBarPrimitive`
The library provides `StatusBarPrimitive.{Root, ModelName, MessageCount, TokenCount, Latency, Status}`. Our `status-bar.tsx` reads from Zustand (SSE-backed) instead. This is **intentional** — the status bar displays data not available through the library's runtime (model defaults, context metrics, project info, agent states).

### 10. ~~No `useAuiEvent` for lifecycle hooks~~ — N/A
No `useAuiEvent` calls exist in the codebase. This was a misdiagnosis.

---

## 🟠 DANGEROUS CONFIG / PATTERNS

### 13. GitHub main-branch dependency — can break at any time
`package.json`: `"@assistant-ui/monorepo": "github:assistant-ui/assistant-ui#main"`. This pins to the ever-moving `main` branch of the monorepo. Combined with `^0.0.18` for `react-ink`, versioning is unstable. Should pin to a specific commit hash once stable.

### 15. `ink-spinner` with Ink 6 — untested compatibility
`ink-spinner@^5.0.0` is a third-party package. Ink 6 introduced breaking changes. Works in practice but could have subtle rendering bugs on edge cases.

### 16. `createRequire` in ESM — Bun-only pattern
`thread.tsx:22-23`: Uses `createRequire(import.meta.url)` to read `package.json` for version display. Only works in Bun, not in Node ESM. Low priority since TUI only runs under Bun.

---

## 🔵 PERFORMANCE / ARCHITECTURE

### 17. No yield throttling — rendering backpressure
Every single SSE event (including individual `text_delta` chars) calls `buildSnapshot()` and `yield`s. For fast models, this means Ink re-renders on every character. Should batch deltas with `setTimeout(0)` or `requestAnimationFrame`-equivalent before yielding.

### 18. Sub-agent matching is heuristic
`custom-tool-ui.tsx` matches tool calls to Zustand agents by priority: `__agentId` injection > name+task prefix > running fallback. Concurrent agents with the same role and no `__agentId` will alias. The `__agentId` injection covers the common case; the heuristic is a known fallback.

### 19. 30s stream stall timeout
`pux-chat-adapter.ts`: `STREAM_STALL_TIMEOUT = 30_000`. Backend sends keepalives every 15s. 30s = 2x gap. Reasonable for local connections but could be too aggressive for remote/cluster routes.

---

## 📋 Remaining Priority

| Priority | Issue | Status |
|----------|-------|--------|
| Medium | #4 Stdin monkey-patch | Functional, low risk |
| Medium | #6 Untyped SSE | Maintenance risk |
| Medium | #13 Moving GitHub dep | Can break on upstream changes |
| Low | #17 No yield throttling | Perf optimization |
| Low | #16 createRequire | Bun-only, documented |
| Low | #18 Sub-agent heuristic | Known fallback, `__agentId` covers main case |
