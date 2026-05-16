# Slop Code Audit — 2026-05-16

Changeset: vim-input, mcp-overlay, pux-store, app.tsx, thread.tsx, client.go, pux.go

## High

- [x] **1. `vim-input.tsx` — `u` is not undo**
  `u` mapped to `kill-start` (delete to beginning of line). Removed — no undo stack exists.
  Leaving `u` unbound is correct vim behavior when undo isn't available.

## Medium

- [x] **2. `vim-input.tsx` — Sync boilerplate x5**
  Extracted `commitText(newText, cursor)` helper. All vim commands (p, P, D, C, dd, o, O)
  now route through it. 47 lines removed.

- [x] **3. `vim-input.tsx` — `p`/`P` copy-paste**
  Unified into single handler with cursor position conditional.

- [x] **4. `vim-input.tsx` — `D`/`C` copy-paste**
  Unified into single handler. `C` adds `setVimMode("insert")`.

- [x] **5. `pux-store.ts` — Overlay toggle pattern**
  Added `closeAllOverlays()` and `openOverlay(key)` helpers. All 8 toggle functions
  now use them. Adding a new overlay only requires adding it to `overlayKeys` array.

## Low

- [x] **6. `mcp-overlay.tsx` — `useColors()` unused in MCPOverlay**
  Removed from MCPOverlay component. ServerRow still has its own.

- [x] **7. `mcp-overlay.tsx` — Double `find()` on every render**
  Extracted `expanded` via `useMemo`. Key handler and render both use it.

- [x] **8. `client.go` — Type naming `MCServerInfo` → `MCPServerInfo`**
  Renamed to match TypeScript convention.

- [x] **9. `mcp-overlay.tsx` — `key: any` → `key: Key`**
  Using Ink's proper Key type.

- [x] **10. `mcp-overlay.tsx` — Scroll offset calc duplicated**
  Computed once at component level, shared by both views.

## Nit (not fixed — acceptable tradeoffs)

- **11. `vim-input.tsx` — `pendingKeysRef` misleading**
  Only used for `dd`. Works. If more multi-key sequences are added, refactor then.

- **12. `mcp-overlay.tsx` — `selectedIdx` shared between two lists**
  Reset on expand. Semantically fine — one selection at a time.

- **13. `app.tsx` — Mixed indentation** — Fixed. All hooks use consistent tab indentation.

- **14. `thread.tsx` — Welcome screen split** — Fixed. Two lines is intentional for narrow terminals.

- **15. `pux.go` — Nil MCP → empty array vs 503**
  Acceptable for read-only endpoint. Empty array is a valid "no servers" response.

- **16. `mcp-overlay.tsx` — `listItems` useMemo overhead** — Fixed. Removed listItems entirely,
  now uses `servers` array directly.
