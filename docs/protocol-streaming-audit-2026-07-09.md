# Protocol Conformance Audit — ACP + Agent Protocol (incl. streaming)

**Date:** 2026-07-09 · **Scope:** full conformance audit of the two named protocols —
**Agent Client Protocol (ACP)** and **Agent Protocol (AP, the LangGraph wire)** —
with explicit focus on streaming. AG-UI noted as adjacent.

> **Post-fold disposition (2026-08):** everything this audit examined is
> **retired with the pre-fold harness** — the custom ACP server class
> (`agent/acp.py`), the hand-rolled Agent Protocol REST surface (`server.py`),
> and the Aegra prod deployment are all gone; the repo serves no wire protocol
> of its own. The ACP surface today is upstream `deepagents-acp` alone (plus a
> JSON adapter file), and the editor/TUI surface is dcode's `run_textual_app`.
> The audit is kept as the **record of the streaming analysis** (the
> `end`-sentinel semantics in §S1 are a durable upstream fact) so nobody
> re-researches it. Every finding below carries its disposition.

**Method (as performed):** evidence-led. Normative bar = the installed UPSTREAM SDKs
(the pivot binds to upstream, so "what the SDK emits/consumes" *is* the spec).
Versions introspected from the pre-fold harness venv (2026-07-09):

| Package | Version | Role |
|---|---|---|
| `agent-client-protocol` (acp) | **0.11.0** | ACP wire (JSON-RPC, SSE elicit) |
| `deepagents-acp` | 0.0.8 | langgraph→ACP adapter (our ACP server base) |
| `langgraph-sdk` | 0.4.2 | AP SSE consumer (SSEDecoder, RunClient) |
| `langgraph` | 1.2.7 | graph runtime |
| `langgraph-api`, `aegra` | **not in this venv** | Aegra runs externally (prod) |

---

## Verdict (as of the audit) → disposition

**Both protocols CONFORMED. Streaming worked and was SDK-proven.** The live
prod lane (Aegra) was conformant *by upstream construction*. The only streaming
divergences sat on `server.py`, the **disabled fallback**, were both LOW
severity, both pinned by contract, and neither broke an SDK consumer.

**Post-fold:** moot. There is no repo server code to audit; conformance is
entirely upstream's property now (`deepagents-acp` stdio). Nothing below is
open work.

---

## 1. ACP lane — CONFORMANT (by delegation) → RETIRED WITH THE HARNESS

**Pre-fold file:** `agent/acp.py` · **Class:** `_RegisteringAgentServerACP`
(subclassed `deepagents_acp.server.AgentServerACP`).

The wire was **100% delegated upstream**. We hand-emitted zero `session/update`
notifications and hand-rolled zero JSON-RPC framing:

- `run_acp_agent(...)` (`acp.py:291`) → upstream `acp` stdio transport.
- All `session/update` Part emission was the base class's job
  (`deepagents-acp` 0.0.8 + `acp` 0.11.0).

**The 4 overrides — none touched the streaming path:**

| Override | super()? | What it added | Streaming risk |
|---|---|---|---|
| `new_session` | ✅ | `register_thread` indexing | none |
| `initialize` | ✅ | truthful capability adj. (image gate, list/load) | none — only *adds* caps, never clobbers |
| `load_session` | new method (base lacked it) | re-hydrate per-session state | none |
| `list_sessions` | new method (base lacked it) | enumerate org threads | none |

We did **not** override `prompt()` or `cancel()` → streaming + cancellation were
upstream-owned.

**Streaming proven LIVE** (real stdio subprocess, `tests/integration/test_acp_e2e.py`):
- `agent_message_chunk` text → `_agent_text` joins streamed chunks (line 89).
- `tool_call` → `test_acp_live_streams_tool_call` asserts `"tool_call" in kinds` (line 242).
- Cancellation → `PromptResponse(stop_reason="cancelled")`, **not an error**
  (`test_acp_cancel_flips_cancellation_flag`; upstream sets `_cancelled`, prompt loop checks it).

**Coverage gap (was NOT a conformance gap):** `plan`, `agent_thought`, `task_*`,
`interrupt`-as-stream Part types were not live-asserted. Whether they emit is
`deepagents-acp`'s decision (upstream responsibility); we lacked live proof.

**ACP-0.11 drift watch (NEW at the audit):** the bump 0.10.1→0.11.0 added
fields: `McpCapabilities.acp` (bool), `PromptCapabilities.audio`,
`PromptCapabilities.embedded_context`; `InitializeResult` is no longer exposed by
that name in `acp.schema` (we mutated the upstream-returned object, not
constructed it — so safe). The drift guard (R3 below) landed pre-fold.

**Disposition:** the class, its tests, and the drift guard retired with the
harness. `deepagents-acp`'s own `AgentServerACP` remains the upstream surface if
an ACP client is ever needed again.

---

## 2. Agent Protocol lane (LangGraph wire) — CONFORMANT → RETIRED

`AP` here = the `langgraph_sdk` wire: HTTP REST
(`/threads`, `/runs`, `/store`, `/assistants`) + `/runs/stream` SSE. Both
runtimes below are gone post-fold.

### 2a. Aegra — PROD — CONFORMANT (by construction) → RETIRED
Aegra was an OSS `langgraph-api` drop-in. By construction it emitted the
canonical upstream SSE set **including the terminal `event: end`**; pux added
**no transform/wrapper**. `custom_app.py` mounted **only** pux-unique surfaces
(`/events/*`, `/jobs/*`) via the `http.app` seam. Not deployed post-fold.

### 2b. server.py — FALLBACK (disabled) — 2 documented streaming divergences → RETIRED

`_stream_run` (`server.py:1041`) hand-rolled frames via `_sse()`:
`f"event: {e}\ndata: {json}\n\n"` — exactly what `langgraph_sdk`'s `SSEDecoder`
parses (`sse.py:78`). Emitted: `metadata` → `messages`/`updates`/`values` →
`error` (on exception). SDK-consumability was **proven** by a real
`SSEDecoder` in `tests/server/test_runs_stream_contract.py`.

| ID | Finding | Severity | Status (pre-fold) |
|---|---|---|---|
| **S1** | No terminal `event: end` frame | **LOW** | pinned + documented; later closed (R1) |
| **S2** | `stream_mode` hardcoded (`server.py:1060`); `RunCreate`/`EphemeralRun` (111-132) carry no `stream_mode`/`config` field → clients can't select modes | **LOW-MED** | unpinned; later closed (R2) |
| **S3** | `interrupt`/`cancel` surface as `__interrupt__` inside `values`/`updates`, not a dedicated SSE event; resume via `command={"resume":…}` | INFO | spec-acceptable |

**S1 was NON-breaking — proven from source.** `langgraph_sdk/_shared/utilities.py:115`:

```python
def _sse_to_v2_dict(event: str, data: Any) -> dict[str, Any] | None:
    ...
    if event == "end":        # line 120
        return None           # line 121 — the SDK SKIPS the end sentinel
```

So the SDK treats `end` as a no-op; the run iterator terminates when bytes stop.
A stream without `end` is fully consumable (the SDK-consumability test passed
with a real `SSEDecoder`). `end` is a **wire-parity** nicety, not a consumer
contract. **This remains the durable upstream fact** for anyone consuming
`deepagents-acp` or langgraph streams today.

**S2** diverged from the langgraph wire (which honors a `stream_mode` query
param). Non-breaking for tolerant consumers.

---

## 3. AG-UI (adjacent, not requested) — known token-translation gap → RETIRED

Per prior-session memory (not re-verified at the audit): incremental
`TEXT_MESSAGE_*` token events were not emitted — langgraph chat-model deltas
streamed through as `RAW` and the reply landed in `MESSAGES_SNAPSHOT`. The
AG-UI lane (CopilotKit mount) retired with the server lane; the gap is
historical.

---

## Severity-ranked findings (as of the audit) → dispositions

1. **S2 — `stream_mode` unparsed in server.py** (LOW-MED, fallback lane) → closed pre-fold (R2); lane retired
2. **ACP-0.11 capability drift watch** (LOW) → closed pre-fold (R3); class retired
3. **S1 — missing terminal `event: end`** (LOW, pinned, proven non-breaking) → closed pre-fold (R1); lane retired
4. **ACP streaming coverage gap** — plan/thought/task not live-asserted (LOW, coverage only) → moot (upstream-owned)

**Nothing was HIGH.** Nothing broke an upstream SDK client. Nothing is live
anymore.

---

## Recommended actions (from the audit) — all resolved or moot

- **R1:** terminal `event: end` in `server.py:_stream_run` + contract flip →
  **landed 2026-07-09**, then retired with the lane.
- **R2:** honor `stream_mode` → **landed 2026-07-09**, then retired with the lane.
- **R3:** acp-0.11 drift guard (field-level capability asserts) → **landed
  2026-07-09**, then retired with the lane.
- **R4 (optional):** re-confirm Aegra `/runs/stream` emits `end` → **moot** —
  Aegra is not deployed post-fold.

---

## Evidence proven vs inferred (as of the audit)

- **PROVEN (SDK source + tests):** `end`-sentinel semantics; server.py event set
  + hardcoded `stream_mode`; prod lane = Aegra live / server.py down; ACP
  delegation + override non-interference; all package versions.
- **INFERRED (by construction):** Aegra emits full set incl. `end` (it *is*
  langgraph-api). **INFERRED (memory, not re-verified):** AG-UI token gap.

---

## Update 2026-07-09 — gaps CLOSED (shipped + proven, pre-fold)

All recommended fixes landed and were proven green (verify-or-die), before the
lane's retirement:

- **R1 ✅** — `server.py:_stream_run` emitted the terminal `event: end`
  (`data: null`) as an unconditional trailing yield (fires on success + caught
  exception; skips on client disconnect/`CancelledError`). The pinned contract
  flipped to `{"metadata","messages","updates","values","end"}`.
- **R2 ✅** — `stream_mode` honored: `RunCreate`/`EphemeralRun` carry a
  `stream_mode` field; both routes accept a `?stream_mode=` query param;
  `_resolve_stream_modes` normalizes single/comma/list → default triple; the
  emission loop generalizes (event name = mode name).
- **R3 ✅** — acp-0.11 drift guard: the capability contract field-level-pins
  `McpCapabilities.acp`, `PromptCapabilities.audio`, `embedded_context` all stay
  unadvertised (verified falsy under 0.11). No `acp.py` change needed (schema
  defaults are already falsy).

**Test proof (pre-fold suites, retired with the lane):**
- `tests/server/test_runs_stream_contract.py` — **8/8** (incl. 3 new:
  `test_runs_stream_emits_terminal_end_frame`,
  `test_runs_stream_end_frame_after_error`,
  `test_runs_stream_honors_requested_stream_mode`).
- `tests/server/test_acp.py::test_acp_advertises_session_load_and_list` — **PASS**
  (real stdio subprocess).
- `tests/server/test_server_interactive.py` — **6/6** (no `end`-frame
  regression; the suite uses subset assertions).

**Remaining (as of the audit):** R4 (Aegra re-confirm) and the AG-UI
token-translation gap — both moot post-fold.
