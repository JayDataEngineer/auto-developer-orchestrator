# GAPS-CONTRACT: SSE → @assistant-ui Translation Gaps

## Architecture

```
Go Backend → SSE events → pux-chat-adapter.ts → ChatModelRunResult → useLocalRuntime → Ink/React
```

The adapter is a **translation layer**. It maps raw SSE events into `ChatModelRunResult` snapshots
that assistant-ui consumes natively. The goal: make every assistant-ui primitive work by populating
every field the runtime expects.

---

## Status: ALL GAPS FIXED

All 12 gaps have been implemented. The contract between the Go backend SSE stream and the
@assistant-ui runtime is now complete and native.

---

## Gap 1: Timing metadata (FIXED — adapter)

**What:** `metadata.timing` with streamStartTime, firstTokenTime, totalStreamTime, tokenCount,
tokensPerSecond, totalChunks, toolCallCount.

**Implementation:** `TimingAccum` accumulator tracked in the generator loop, built in `buildSnapshot`.

---

## Gap 2: Per-step usage (FIXED — adapter)

**What:** `metadata.steps` array of `{ usage: { inputTokens, outputTokens } }`.

**Implementation:** `stepsRef` accumulator populated from `agent_end` events.

---

## Gap 3: Feedback adapter (FIXED — adapter)

**What:** `adapters.feedback` implementing `FeedbackAdapter`.

**Implementation:** POSTs to `/api/pux/feedback` in app.tsx runtime setup.

---

## Gap 4: Custom metadata (FIXED — adapter)

**What:** `metadata.custom: Record<string,unknown>` — model, agentId, project.

**Implementation:** Built in `buildSnapshot` from Zustand store state.

---

## Gap 5: Cancel yields incomplete (FIXED — adapter)

**What:** `{ type: "incomplete", reason: "cancelled" }` on abort.

**Implementation:** Catch AbortError, yield incomplete/cancelled before returning.

---

## Gap 6: Tool interrupt (FIXED — adapter)

**What:** `ToolCallMessagePart.interrupt = { type: "human", payload }` on decision-triggering tools.

**Implementation:** `decision_request.sourceTool` used to find and mark the specific running tool.

---

## Gap 7: Suggestion adapter (FIXED — adapter)

**What:** `adapters.suggestion` implementing `SuggestionAdapter`.

**Implementation:** `generate()` fetches from `/api/pux/suggestions` or falls back to defaults.
Wired in app.tsx runtime setup.

---

## Gap 8: runConfig usage (FIXED — adapter)

**What:** `run({ runConfig })` carries user preferences (model, temperature).

**Implementation:** Reads `runConfig.custom.model` and `runConfig.custom.temperature` first,
falls back to Zustand store.

---

## Gap 9: Sub-agent messages (FIXED — backend + adapter)

**What:** `ToolCallMessagePart.messages` — nested conversation threads inside delegate tool calls.

**Implementation:** Sub-agent `text_delta` and `thinking_delta` events (with `agentName`) are
collected into `subAgentMessageAccum[]`. When the delegate tool's `tool_execution_end` fires,
the accumulated messages are flushed into the tool's `messages` field.

---

## Gap 10: Source/citation parts (FIXED — backend + adapter)

**What:** `SourceMessagePart` with URL, title, sourceType.

**Implementation:** New `source` SSE event type in Go backend (`EventTypeSource`). Adapter creates
`SourceMessagePart` objects and adds them to the content array (between tools and text).

Backend fields: `AgentEventData.SourceType`, `SourceURL`, `SourceID`, `Text` (title).

---

## Gap 11: Artifact data (FIXED — backend + adapter)

**What:** `ToolCallMessagePart.artifact` — structured data for rich rendering.

**Implementation:** `AgentEventData.Artifact` field on `tool_execution_end`. Go backend extracts
artifacts from tool results via `extractArtifact()` (detects `artifact` key, `diff` key, `changes`
key in result maps). Adapter reads `parsed.artifact` and sets it on the tool part.

---

## Gap 12: Model content separation (FIXED — backend + adapter)

**What:** `ToolCallMessagePart.modelContent` — content fed back to the model separately.

**Implementation:** `AgentEventData.ModelContent` field on `tool_execution_end`. Go backend
produces shortened model-visible content via `extractModelContent()` (delegates get summary,
other tools use same content). Adapter reads `parsed.modelContent` and wraps in
`ToolModelContentPart[]`.

---

## Not Applicable (TUI context)

- `speech`, `dictation`, `voice` adapters — terminal context
- `attachments` adapter — Ink has no File API
- `parentId` on parts — branching not used
