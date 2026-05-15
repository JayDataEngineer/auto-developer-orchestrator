# GAPD-INK: TUI Integration Gaps & Implementation Plan

## Implementation Status (Updated 2026-05-15)

### DONE - Phase 1: Fill assistant-ui Primitive Gaps
- [x] GAP-1.1: `ComposerPrimitive.Cancel` — added next to input when running
- [x] GAP-1.2: `ActionBarPrimitive.Edit` — added to user message action bar
- [x] GAP-1.3: `ActionBarPrimitive.FeedbackPositive/Negative` — added to assistant action bar with isSubmitted state
- [x] GAP-1.4: `ThreadPrimitive.Suggestion` — added SuggestionChips on empty thread
- [x] GAP-1.5: MessageTiming — status bar now shows agent count and active view
- [x] GAP-1.6: `useAuiEvent` — subscribed to runStart/runEnd in thread.tsx
- [x] GAP-1.7: `useAuiState` — already partially used; kept Zustand for Pux-specific state

### DONE - Phase 2: Multi-Conversation Support
- [x] GAP-2.1: ConversationsView — built with up/down/enter/d navigation
- [x] GAP-2.2: Per-conversation actions — open (enter), delete (D+confirm)
- [x] GAP-2.3: Falls back to Zustand store (not useRemoteThreadListRuntime yet)
- [x] GAP-2.4: History adapter loads from backend, `/conversations` command

### DONE - Phase 3: Subagent Monitoring
- [x] GAP-3.1: Subagent SSE events parsed into Zustand agent store
- [x] GAP-3.2: Agent state store with addAgent, updateAgentStatus, addAgentToolCall, clearAgents
- [x] GAP-3.3: AgentsView with live agent tree, status icons, duration, collapsible detail
- [x] GAP-3.4: Nested tool calls rendered in DelegateToolUI via store lookup

### DONE - Phase 4: Multiple Views
- [x] GAP-4.1: View router in app.tsx — 5 views (Chat, Agents, Tools, Files, Conversations)
- [x] GAP-4.2: ToolsView — aggregated tool call list from runtime state
- [x] GAP-4.3: FilesView — files modified/read in session
- [x] GAP-4.4: TabBar with active highlight and Ctrl+T cycling

### DONE - Phase 5: Composer Queue & Native Adapter Rework
- [x] GAP-5.1: ComposerQueue component — reads from runtime state
- [x] GAP-5.3: Adapter reworked with native assistant-ui status types
  - `requires-action` status when HITL events arrive
  - `interrupt` field on tool calls for human interrupts
  - Sub-agent tracking into Zustand from SSE events

---

## Package Versions (Current)
- `@assistant-ui/react-ink` 0.0.16
- `@assistant-ui/react-ink-markdown` 0.0.15
- `@assistant-ui/react` 0.14.5
- `@assistant-ui/store` 0.2.10

---

## Phase 1: Fill assistant-ui Primitive Gaps

### GAP-1.1: ComposerPrimitive.Cancel
- **Status**: Missing
- **What**: No cancel button in the composer. Ctrl+C is handled manually via useInput.
- **Available**: `ComposerPrimitive.Cancel` renders a Pressable that cancels the current run.
- **Fix**: Add a Cancel pressable next to the input when `ThreadPrimitive.If running={true}`.

### GAP-1.2: ActionBarPrimitive.Edit
- **Status**: Missing
- **What**: No ability to edit a previously sent user message.
- **Available**: `ActionBarPrimitive.Edit` is a Pressable that enters edit mode on a user message.
- **Fix**: Add Edit action to user message action bar. Requires `EditComposer` component.

### GAP-1.3: ActionBarPrimitive.FeedbackPositive/Negative
- **Status**: Missing
- **What**: No thumbs up/down feedback on assistant messages.
- **Available**: `ActionBarPrimitive.FeedbackPositive` and `FeedbackNegative` with `isSubmitted` state.
- **Fix**: Add feedback buttons to assistant message action bar. Wire to adapter `adapters.feedback`.

### GAP-1.4: ThreadPrimitive.Suggestion
- **Status**: Missing
- **What**: No suggestion chips after assistant messages or on empty thread.
- **Available**: `ThreadPrimitive.Suggestion` renders a clickable prompt suggestion with `send` and `clearComposer` props.
- **Fix**: Register suggestions via adapter. Render `ThreadPrimitive.Suggestion` components.

### GAP-1.5: MessageTiming in Status Bar
- **Status**: Missing
- **What**: Status bar shows token count but not stream timing (TTFB, tokens/sec, duration).
- **Available**: `MessageTiming` on message state: `streamStartTime`, `firstTokenTime`, `totalStreamTime`, `tokenCount`, `tokensPerSecond`, `totalChunks`, `toolCallCount`.
- **Fix**: Read timing from `useAuiState((s) => s.message)` for the last assistant message. Display in status bar.

### GAP-1.6: useAuiEvent Lifecycle Hooks
- **Status**: Missing
- **What**: No event subscriptions for run start/end, thread init, etc.
- **Available**: `useAuiEvent` subscribes to `thread.runStart`, `thread.runEnd`, `thread.initialize`, `thread.modelContextUpdate`, `composer.send`.
- **Fix**: Subscribe to events for auto-scroll triggers, status updates, notification sounds.

### GAP-1.7: useAuiState for State Reads
- **Status**: Partial (uses Zustand @pux/shared instead)
- **What**: Branch state, running state, composer state read through custom Zustand selectors.
- **Available**: `useAuiState((s) => s.thread.isRunning)`, `s.message.branchNumber`, `s.composer.canSend`, etc.
- **Fix**: Migrate state reads to `useAuiState` where possible. Keep Zustand for Pux-specific state (projects, metrics).

---

## Phase 2: Multi-Conversation Support

### GAP-2.1: ThreadListPrimitive View
- **Status**: Missing
- **What**: No way to view, switch between, or manage multiple conversations.
- **Available**: `ThreadListPrimitive.Root`, `ThreadListPrimitive.Items` (renders `renderItem` per thread), `ThreadListPrimitive.New` (create new thread).
- **Fix**: Build a conversation list view toggled by keybinding (e.g., Alt+H or `/history`).

### GAP-2.2: ThreadListItemPrimitive
- **Status**: Missing
- **What**: No per-conversation actions (open, rename, archive, delete).
- **Available**: `ThreadListItemPrimitive.Root`, `.Title`, `.Trigger` (switch to), `.Delete`, `.Archive`, `.Unarchive`.
- **Fix**: Each list item renders title, pressable trigger, and optional delete/archive.

### GAP-2.3: Remote Thread List Runtime
- **Status**: Missing
- **What**: Currently using `useLocalRuntime` which is single-thread.
- **Available**: `useRemoteThreadListRuntime(adapter)` for persistent multi-thread conversations.
- **Fix**: Implement `ThreadListRuntimeAdapter` backed by Pux API. Switch from `useLocalRuntime` to `useRemoteThreadListRuntime` or use `RuntimeAdapterProvider` with `adapters.history`.

### GAP-2.4: Thread History Adapter
- **Status**: Partial (pux-history-adapter.ts exists but minimal)
- **What**: History adapter loads thread list but doesn't support full CRUD.
- **Available**: `ThreadHistoryAdapter` with `load()`, `persist()`, `delete()`, `rename()`.
- **Fix**: Extend shared history adapter to support list, delete, rename, archive operations.

---

## Phase 3: Subagent Monitoring (Custom Build)

### GAP-3.1: Subagent SSE Event Parsing
- **Status**: Partial
- **What**: Backend sends `subagent_*` SSE events but they're only tracked, not structured for display.
- **Fix**: Parse `subagent_started`, `subagent_tool_call`, `subagent_completed`, `subagent_error` into a structured agent tree with: agentId, agentName, task, status, startTime, endTime, toolCalls[], result.

### GAP-3.2: Subagent State Store
- **Status**: Missing
- **What**: No dedicated store for subagent tracking data.
- **Fix**: Create a Zustand store (or extend pux-store) with:
  - `agents: Map<agentId, AgentState>`
  - `activeAgentIds: string[]`
  - Actions: addAgent, updateAgentStatus, addAgentToolCall, clearAgents
  - Derived: runningAgents, completedAgents, failedAgents

### GAP-3.3: Subagent Panel Component
- **Status**: Missing
- **What**: No dedicated view for monitoring running subagents.
- **Fix**: Build `AgentsView` Ink component:
  - Header: "Active Agents (N running, M completed)"
  - Per-agent block: name, task preview, status icon, duration, tool count
  - Collapsible detail: full tool call history, result summary
  - Color coding: running=yellow, complete=green, error=red

### GAP-3.4: Nested Message Rendering
- **Status**: Missing
- **What**: `ToolCallMessagePart.messages` field exists in types but is never populated.
- **Available**: The `messages?: ThreadMessage[]` field on tool call parts.
- **Fix**: Populate `messages` on delegate tool calls from subagent SSE events. Render nested thread within the tool call UI.

---

## Phase 4: Multiple Views

### GAP-4.1: View Router
- **Status**: Missing
- **What**: TUI only shows the chat thread. No way to switch to other views.
- **Fix**: Build a view router in app.tsx:
  - State: `currentView: 'chat' | 'agents' | 'tools' | 'files'`
  - Tab bar at top: [Chat] [Agents] [Tools] [Files]
  - Keybindings: Alt+1 (Chat), Alt+2 (Agents), Alt+3 (Tools), Alt+4 (Files)
  - Each view is a separate Ink component tree

### GAP-4.2: Tools Dashboard View
- **Status**: Missing
- **What**: No aggregated view of recent tool calls.
- **Fix**: Build `ToolsView` component:
  - Scrollable list of recent tool calls across all messages
  - Each entry: tool name, args preview, status, duration
  - Expand to see full args/result
  - Filter by tool type (bash, file, delegate)

### GAP-4.3: Files Modified View
- **Status**: Missing
- **What**: No view showing all files modified in the session.
- **Fix**: Build `FilesView` component:
  - List of files read/written with diff previews
  - Status icons (created, modified, deleted)
  - Expandable diff using existing DiffView component
  - File path grouping by directory

### GAP-4.4: Tab Bar Component
- **Status**: Missing
- **What**: No visual tab bar for view switching.
- **Fix**: Build `TabBar` component:
  - Renders tab names with active highlight
  - Shows keyboard shortcuts (Alt+N)
  - Status counts per tab (e.g., "Agents (3)")
  - Single line at top of screen

---

## Phase 5: Composer Queue & Steer

### GAP-5.1: Queued Message Display
- **Status**: Missing
- **What**: No way to see queued messages waiting to be processed.
- **Available**: `ComposerPrimitiveQueue` renders queued items. Each `QueueItemState` has `{ id, prompt }`.
- **Fix**: Render queue above the composer. Show prompt text and position.

### GAP-5.2: Queue Item Management
- **Status**: Missing
- **What**: Can't remove or reorder queued messages.
- **Available**: `QueueItemMethods.remove()` and `steer()` (interrupt current, process this next).
- **Fix**: Each queue item is a Pressable with remove (X) and steer (!) actions.

### GAP-5.3: Steer Support in Adapter
- **Status**: Missing
- **What**: Backend doesn't support message steering/interruption.
- **Fix**: Add steer endpoint or use existing cancel + re-prompt mechanism. Wire `composer.send({ steer: true })`.

---

## Implementation Priority

1. **Phase 1** (primitive gaps) — Low risk, high compatibility improvement
2. **Phase 4.1 + 4.4** (view router + tab bar) — Foundation for new views
3. **Phase 3** (subagent monitoring) — Custom build, highest user value
4. **Phase 4.2 + 4.3** (tools + files views) — Completes multi-view
5. **Phase 2** (multi-conversation) — Requires backend API work
6. **Phase 5** (composer queue) — Depends on backend support

---

## Files to Create/Modify

### New Files
- `ts-tui-ink/src/components/tab-bar.tsx`
- `ts-tui-ink/src/components/agents-view.tsx`
- `ts-tui-ink/src/components/tools-view.tsx`
- `ts-tui-ink/src/components/files-view.tsx`
- `ts-tui-ink/src/components/composer-queue.tsx`
- `ts-tui-ink/src/components/suggestion-chips.tsx`
- `ts-tui-ink/src/stores/agent-store.ts`

### Modified Files
- `ts-tui-ink/src/app.tsx` — View router, tab bar
- `ts-tui-ink/src/main.tsx` — New keybindings (Alt+1-4)
- `ts-tui-ink/src/components/thread.tsx` — Cancel button, suggestions
- `ts-tui-ink/src/components/assistant-message.tsx` — Feedback buttons, timing
- `ts-tui-ink/src/components/user-message.tsx` — Edit action
- `ts-tui-ink/src/components/custom-tool-ui.tsx` — Nested agent messages
- `ts-tui-ink/src/components/action-bar.tsx` — Edit, feedback additions
- `ts-tui-ink/src/components/status-bar.tsx` — Timing display
- `shared/src/pux-chat-adapter.ts` — Subagent event parsing
- `shared/src/pux-store.ts` — Agent state
- `shared/src/types.ts` — Agent types
