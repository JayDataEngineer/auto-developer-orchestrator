# GAPS-CONTRACT: SSE → @assistant-ui Translation Gaps

## Architecture

```
Go Backend → SSE events → pux-chat-adapter.ts → ChatModelRunResult → useLocalRuntime → Ink/React
```

The adapter is a **translation layer**. It maps raw SSE events into `ChatModelRunResult` snapshots
that assistant-ui consumes natively. The goal: make every assistant-ui primitive work by populating
every field the runtime expects.

---

## Gap 1: No timing metadata (HIGH — adapter only)

**What assistant-ui expects:** `metadata.timing` with 7 fields:
- `streamStartTime` (number) — ms when first chunk arrives
- `firstTokenTime` (number?) — ms when first text/thinking delta arrives
- `totalStreamTime` (number?) — ms from start to completion
- `tokenCount` (number?) — total tokens generated
- `tokensPerSecond` (number?) — derived
- `totalChunks` (number) — count of SSE events
- `toolCallCount` (number) — count of tool calls

**What we do:** Never built. All data is trivially available during streaming.

**Fix:** Track timestamps in the generator loop, build `metadata.timing` in `buildSnapshot`.

---

## Gap 2: No per-step usage (HIGH — adapter only)

**What assistant-ui expects:** `metadata.steps` array of `{ usage: { inputTokens, outputTokens } }`.

**What we do:** Backend sends `agent_end` with token counts. We write it to Zustand but never to the snapshot.

**Fix:** Wire `agent_end` usage into a `steps` array.

---

## Gap 3: No feedback adapter (HIGH — adapter only)

**What assistant-ui expects:** `adapters.feedback` implementing `FeedbackAdapter`.
Enables `ActionBarPrimitive.FeedbackPositive/Negative`. Without it, `capabilities.feedback` is `false`.

**What we do:** Not provided. Buttons render but are no-ops.

**Fix:** Implement adapter that POSTs to backend.

---

## Gap 4: No custom metadata (HIGH — adapter only)

**What assistant-ui expects:** `metadata.custom: Record<string,unknown>` — arbitrary per-message data.

**What we do:** Never built.

**Fix:** Add `custom` with model, contextUtil, agentId.

---

## Gap 5: Cancel yields nothing (HIGH — adapter only)

**What assistant-ui expects:** `{ type: "incomplete", reason: "cancelled" }` on abort.

**What we do:** Silently `return` on AbortError.

**Fix:** Yield incomplete status before returning.

---

## Gap 6: interrupt not set on tools (HIGH — adapter only)

**What assistant-ui expects:** `ToolCallMessagePart.interrupt = { type: "human", payload }` on the
specific tool that triggered the decision request.

**What we do:** `decision_request` sets global `requires-action` but never marks which tool.
Tool-level interrupt UI (`requires-action` with reason `interrupt`) can't render.

**Fix:** Track which tool triggered the decision, set its `interrupt` field.

---

## Gap 7: No suggestion adapter (MEDIUM — adapter only)

**What assistant-ui expects:** `adapters.suggestion` implementing `SuggestionAdapter`.

**What we do:** Not provided. `ThreadPrimitive.Suggestion` renders static chips from our component,
not from the runtime's suggestion system.

**Fix:** Implement adapter or leave as static chips (lower priority).

---

## Gap 8: No runConfig usage (MEDIUM — adapter only)

**What assistant-ui expects:** `run({ runConfig })` carries user preferences (model, temperature).

**What we do:** Read model from Zustand store instead of from the runtime's config.

**Fix:** Read model from `runConfig.custom` or keep Zustand (lower priority, works fine).

---

## Gap 9: No sub-agent messages (LOW — requires backend)

**What assistant-ui expects:** `ToolCallMessagePart.messages: ThreadMessage[]` — nested conversation
threads inside tool calls.

**What we do:** Not populated. Sub-agents tracked in Zustand store separately.

**Fix:** Backend would need to emit structured sub-conversation data per delegate call.

---

## Gap 10: No source/citation parts (LOW — requires backend)

**What assistant-ui expects:** `SourceMessagePart` with URL, title, providerMetadata.

**What we do:** Not produced.

**Fix:** Backend would need to emit source events.

---

## Gap 11: No artifact data (LOW — requires backend)

**What assistant-ui expects:** `ToolCallMessagePart.artifact` — structured data for rich rendering
(e.g., diff previews, rendered HTML).

**What we do:** Not populated.

**Fix:** Backend would need to emit artifact payloads in `tool_execution_end`.

---

## Gap 12: No model content separation (LOW — requires backend)

**What assistant-ui expects:** `ToolCallMessagePart.modelContent` — content fed back to the model
in subsequent turns, separate from what's shown to the user.

**What we do:** Backend doesn't separate display content from model-visible content.

**Fix:** Backend would need to emit both display and model-visible results.

---

## Not Applicable (TUI context)

- `speech`, `dictation`, `voice` adapters — terminal context
- `attachments` adapter — Ink has no File API
- `parentId` on parts — branching not used

---

## Implementation Priority

1. **Gaps 1-6** — adapter only, high impact, no backend changes
2. **Gap 7-8** — adapter only, medium impact
3. **Gaps 9-12** — require backend changes, plan separately
