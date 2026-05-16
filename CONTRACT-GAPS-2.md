# CONTRACT.md Compliance Audit — Gaps Found

Date: 2026-05-16

## Overall: Largely Compliant, 4 Gaps

| Contract | Status | Issues |
|---|---|---|
| 1. Prompt Request | OK | `autoMerge` field not in contract (additive, harmless) |
| 2. SSE Event Stream | 3 gaps | `decision_request` replaces `user_question`/`approval_request`; `plan_created` missing; `source` undocumented |
| 3. Tool Interface | 1 gap | `ParallelRunner` holds direct subscriber reference |
| 4. Client State | OK | ChatState is sole accumulator |
| 5. Extensions | OK | MCP-only, no kernel access |
| 6. Organizations | OK | Kernel roles immutable |

---

## Gap 1: ParallelRunner Direct Subscriber Reference (HIGH)

**File**: `go-backend/internal/tools/orchestration/parallel_runner.go`
**Contract violated**: 3.4 — "Tools MUST NOT hold direct references to the SSE subscriber channel"

The `ParallelRunner` struct stores `subscriber chan<- core.AgentEvent` as a struct field and
calls `core.SendEvent(r.subscriber, ...)` directly in ~15 locations (lines 255, 378, 495, 655, 805, etc.).

Other tools correctly use context injection:
- `ask.go:66` — `ctx.Value(core.SubscriberKey{})` with explicit "Contract 3 compliance" comment
- `plan.go:80` — same pattern

**Fix**: Remove `subscriber` field and `SetSubscriber()` method. Extract subscriber from
`ctx.Value(core.SubscriberKey{})` at each call site, same as ask.go/plan.go.

---

## Gap 2: `decision_request` Replaces `user_question` + `approval_request` (MEDIUM)

**Contract section**: 2.6 User Interaction
**File**: `go-backend/internal/handlers/pux_sse.go:165`

Contract specifies two separate events:
- `user_question` with `{questionId, question, options, allowFreeText}`
- `approval_request` with `{requestId, title, description, toolName, args}`
- Response endpoint: `POST /api/pux/respond` with `{requestId, response}`

Implementation uses unified `decision_request` with `{decisionId, sourceTool, title,
description, hint, options, allowFreeText, metadata}` and response endpoint
`POST /api/pux/decision` with `{decisionId, action, value}`.

**Fix**: Update CONTRACT.md to document the unified decision system (cleaner design).

---

## Gap 3: `plan_created` Not Emitted (LOW)

**Contract section**: 2.7 Artifacts & Plans
**File**: `go-backend/internal/handlers/pux_sse.go:179`

Contract defines `plan_created` with `{planId, name, content, filePath}`. Only `plan_updated`
exists in the implementation. The plan tool (`go-backend/internal/tools/plan/plan.go`) never
emits the creation event.

**Fix**: Add `plan_created` emission in the plan tool when a new plan is created.

---

## Gap 4: Undocumented `source` Event (LOW)

**Contract section**: 2 (no section for it)
**File**: `go-backend/internal/handlers/pux_sse.go:214`

The kernel emits a `source` event for citations/references with payload
`{sourceType, id, url?, title?}`. This is not in the contract, violating the
"no ad-hoc event types" rule.

**Fix**: Add `source` to CONTRACT.md section 2.7.

---

## Fix Status

- [x] Gap 1: ParallelRunner refactored to use context injection
- [x] Gap 2: CONTRACT.md updated with unified decision_request
- [x] Gap 3: plan_created event implemented
- [x] Gap 4: source event documented in CONTRACT.md
