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

## 4. Theme System ✅ DONE
Six built-in themes with reactive color system via React Context.

- **`ThemeProvider` + `useColors()` hook** (`ts-tui-ink/src/theme.tsx`): Wraps the app and provides reactive colors from a React Context. Components call `const colors = useColors()` and re-render when the theme changes. Both `theme.ts` was renamed to `theme.tsx` to support the JSX in `ThemeProvider`.
- **6 themes**: Default (magenta brand), Dark (green/black), Light (blue/light), Catppuccin (mocha), Dracula, Tokyo Night (storm). Each defines 11 color slots (brand, user, assistant, success, error, warning, running, text, textDim, textMuted, subtle).
- **Theme selection**: `/settings` → navigate to Theme section → press 1-6 to switch. Persisted to `localStorage` (`pux:theme`). The settings overlay uses `themeList` from theme.tsx so new themes auto-appear.
- **16 components updated**: All components that previously imported the static `colors` object now call `useColors()` at the component level, making them reactive to theme changes.
- **Figma/Chalk-compatible**: All color values are valid chalk color names or `#hex` values supported by Ink's `colorize()`.

## 5. File Picker Dialog (OpenCode)
`ctrl+f` → file picker overlay with fuzzy search. Useful for referencing files, attaching context, navigating. We have a files view but no interactive file picker from the composer.

## 6. Log Viewer / Diagnostics (OpenCode, Claude Code)
OpenCode has `ctrl+l` → dedicated log viewer with table + detail panels. Claude Code has `Doctor.tsx` diagnostics. No centralized log/event viewer.

## 7. Settings UI ✅ DONE
Full-screen settings overlay with `/settings` slash command.

- **`SettingsOverlay`** (`ts-tui-ink/src/components/settings-overlay.tsx`): Follows the same overlay pattern as `ProvidersOverlay` — takes over the content area, has up/down navigation, Escape to close.
- **Four sections**: Active Model (shows current model + provider, Enter to open model picker), Providers (lists configured providers with status, Enter to open providers panel), Theme (selectable radio group with checkmarks, stored in localStorage), System (project name, agent ID).
- **Theme storage**: `pux:theme` key in localStorage via `usePuxStore.setTheme()`. Initial value loaded on store creation. Three built-in themes defined: Default (magenta brand), Dark (green brand), Light (blue brand).
- **Slash command**: `/settings` toggles the overlay. No ctrl+keybinding — consistent with Pux conventions (use `/` for UI panels).
- **Keyboard**: ↑↓ to navigate between sections, Enter to activate (model→pick model, providers→providers panel), Escape to close.

## 8. Search/Find in Chat (Claude Code)
Search through message history with highlighted matching. No search functionality at all.

## 9. Vim/Emacs Input Modes (Claude Code)
`VimTextInput` and `BaseTextInput` with vim/emacs keybindings. We have standard `ink-text-input` with basic editing.

## 10. Image Display ✅ DONE
Kitty protocol image rendering in the terminal. Supports Kitty and iTerm2 terminals.

- **`TerminalImage` component** (`ts-tui-ink/src/components/terminal-image.tsx`): Renders base64-encoded PNG images inline using the Kitty escape sequence (`\x1b_Ga=T,f=100,...`) or iTerm2's OSC 1337 protocol. Auto-detects terminal support via `KITTY_WINDOW_ID`/`TERM` env vars.
- **Chunked transmission**: Splits large images into 4KB chunks with the Kitty protocol's `m=1` multiplexing to avoid terminal buffer overflows.
- **Non-Kitty fallback**: Shows image type, filename, and truncated data URI with "(use Kitty or iTerm2)" hint when terminal doesn't support inline images.
- **`ImageMessagePart` support**: `AssistantMessage` now handles `"image"` part type (case in parts switch).
- **`FileMessagePart` support**: Added `"file"` case that shows filename with `▎` blockquote.
- **Screenshot tool UIs** (`custom-tool-ui.tsx`): `ScreenshotRenderer` registered under all known screenshot tool names (`screenshot`, `desktop_screenshot`, `computer_screenshot`, `take_screenshot`, `browser_screenshot`, `web_screenshot`, `observe`, `desktop_observe`). Extracts data URIs from raw results or JSON `screenshot` fields (e.g. `PageContext.Screenshot` from browser tools).
- **Data URI detection**: `tryExtractImageDataURI()` scans result strings for `data:image/{png,jpeg,gif,webp};base64,` prefixes. `extractScreenshotURI()` also handles nested objects and raw base64 strings.

## 11. MCP Server Configuration UI (Claude Code)
In-TUI MCP server management (add, configure, test, remove). No MCP UI.

## 12. Autocomplete on File Paths ✅ DONE
Tab-triggered file path autocomplete in the composer, integrating with the slash command palette.

- **`PathAutocomplete`** (`ts-tui-ink/src/components/path-autocomplete.tsx`): Detects path patterns (`./`, `../`, `/`, `~/`) at word boundaries using regex. Reads directory contents via `fs.readdirSync` relative to the project root (`activeProjectPath` from the store).
- **`getCompletions()`** hook: Shared helper used by both the render component and the key handler. Sorts results with directories first, maps file extensions for type hints. Returns up to 20 matches.
- **Tab integration**: When a path prefix is detected (no command palette active), Tab replaces the prefix with the selected completion. Up/Down navigate completions.
- **Layered priority**: Command palette takes precedence → path autocomplete → history navigation. All three coexist without conflicts.
- **Directory markers**: Directories shown with cyan `dir` label and trailing `/` on display name.

## 13. Session Switching ✅ DONE
Quick conversation switching overlay with `/sessions` slash command.

- **`SessionSwitcher`** (`ts-tui-ink/src/components/session-switcher.tsx`): Compact overlay with live text filter — type to filter conversations by title, agent ID, or project. Arrow keys navigate, Enter switches, Escape closes.
- **Auto-loads**: Fetches conversations on open via `loadConversations()`. Resets filter and selection each time.
- **Smart filter**: Filters across `title`, `agentId`, and `project` fields. Shows conversation count, project name, and active indicator (←).
- **Store integration**: `toggleSessionSwitcher()` / `closeSessionSwitcher()` actions in Zustand store. Closes other overlays when opened.
- **Slash command**: `/sessions` toggles the overlay. Follows Pux conventions — no ctrl+keybinding.

## 14. Command History ✅ DONE
Up/down arrows in the composer navigate a 200-entry sent message history. Pressing up saves the current draft and replaces the input with the most recent history item; pressing down restores the draft (or goes newer). Deduplicates consecutive identical entries.
