# TUI Gaps — Our TUI vs Claude Code & OpenCode

## 1. Mouse Support & Text Selection ✅ DONE
SGR mouse tracking (modes 1000 + 1006) enabled on startup, disabled on exit. Parses `ESC[<btn;col;rowM/m` sequences from stdin before Ink processes them.

- **Native selection preserved**: Mode 1000 (press/release only) does NOT consume drag events, so terminal native text selection still works. Shift+click bypasses mouse reporting entirely.
- **Wheel → keyboard**: Mouse wheel events (button & 0x40) are converted to `\x1b[A`/`\x1b[B` (arrow up/down) so Ink handles scroll naturally.
- **`useMouseEvent(cb)` hook**: React components subscribe to click events.
- **`useLastClick()` hook**: Returns most recent click position for hit testing.
- **Infrastructure ready**: `onMouseEvent()` and `filterAndEmitMouseEvents()` available for future OSC 52 copy, hit testing, click-on-message.

## 2. Inline File Diff Viewing ✅ DONE
- **`write_file`**: Now uses `DiffView` from `@assistant-ui/react-ink` with `newFile={{ content, name }}` — shows every line as a green addition with line numbers, 50-line cap with fold support.
- **`file_edit` / `edit_file`**: New `FileEditPatchToolUI` renders old_string in red and new_string in green with the `▎` blockquote bar, Claude Code-style. Truncated at 8 lines per block.
- `DiffView` component already wrapped standalone in `diff-view.tsx` for reuse.
- Registered under both canonical (`file_edit`) and alias (`edit_file`) tool names.
- Future: Could enhance with full unified diff parsing if the backend returns a proper patch in the artifact.

## 3. Permission System ✅ DONE
New `PermissionHook` (`go-backend/internal/hooks/permission.go`) implements `ToolCallWrapper` and checks configured permission levels before tool execution.

- **Three levels**: `auto` (pass through), `confirm` (ask user), `deny` (block with error). Configured via `ToolPermissionConfig` defaults: bash/write/edit/web_fetch=auto, delete/git_push/git_reset=confirm.
- **Uses existing `DecisionRegistry`**: Confirm triggers a `decision_request` SSE event with `hint: "approval"`, tool name in the header, and args in the description. Blocks until user responds.
- **Always allow (session)**: Users press `A` in the TUI dialog → backend caches the grant in a per-session map. Subsequent calls to the same tool bypass permission checks.
- **Reject**: Returns a tool error to the agent loop.
- **DecisionDialog enhanced**: Detects tool permission requests via `metadata.toolName`, shows args in monospace, offers 3-button layout (Y=once, A=always, N=reject).
- **API endpoints**: `GET/PUT /api/pux/tool-permissions` already existed — change levels at runtime.

## 4. Theme System (OpenCode)
One hardcoded theme (8 colors). OpenCode ships 10 themes (catppuccin, dracula, tokyonight, etc.) with a theme manager. Claude Code has dynamic runtime themes.

## 5. File Picker Dialog (OpenCode)
`ctrl+f` → file picker overlay with fuzzy search. Useful for referencing files, attaching context, navigating. We have a files view but no interactive file picker from the composer.

## 6. Log Viewer / Diagnostics (OpenCode, Claude Code)
OpenCode has `ctrl+l` → dedicated log viewer with table + detail panels. Claude Code has `Doctor.tsx` diagnostics. No centralized log/event viewer.

## 7. Settings UI (Claude Code, OpenCode)
Settings screens for API keys, model defaults, theme, etc. We have a provider overlay but no general settings panel.

## 8. Search/Find in Chat (Claude Code)
Search through message history with highlighted matching. No search functionality at all.

## 9. Vim/Emacs Input Modes (Claude Code)
`VimTextInput` and `BaseTextInput` with vim/emacs keybindings. We have standard `ink-text-input` with basic editing.

## 10. Image Display (OpenCode, Pi TUI)
Render images in-terminal via Kitty protocol. No image display capability.

## 11. MCP Server Configuration UI (Claude Code)
In-TUI MCP server management (add, configure, test, remove). No MCP UI.

## 12. Autocomplete on File Paths (Claude Code, OpenCode)
Context-aware file path autocomplete in the composer/editor. We have slash command palette but no path autocomplete.

## 13. Session Switching (OpenCode)
`ctrl+s` for instant session switch overlay. Our conversations view requires tab navigation.

## 14. Command History ✅ DONE
Up/down arrows in the composer navigate a 200-entry sent message history. Pressing up saves the current draft and replaces the input with the most recent history item; pressing down restores the draft (or goes newer). Deduplicates consecutive identical entries.
