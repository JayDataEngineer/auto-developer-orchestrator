# Protocol Conformance Audit — ACP + Agent Protocol (incl. streaming)

**Date:** 2026-07-09 · **Scope:** full conformance audit of the two named protocols —
**Agent Client Protocol (ACP)** and **Agent Protocol (AP, the LangGraph wire)** —
with explicit focus on streaming. AG-UI noted as adjacent.

**Method:** evidence-led. Normative bar = the installed UPSTREAM SDKs themselves
(the pivot binds to upstream, so "what the SDK emits/consumes" *is* the spec).
Versions introspected from `pux-harness/.venv`:

| Package | Version | Role |
|---|---|---|
| `agent-client-protocol` (acp) | **0.11.0** | ACP wire (JSON-RPC, SSE elicit) |
| `deepagents-acp` | 0.0.8 | langgraph→ACP adapter (our ACP server base) |
| `langgraph-sdk` | 0.4.2 | AP SSE consumer (SSEDecoder, RunClient) |
| `langgraph` | 1.2.7 | graph runtime |
| `langgraph-api`, `aegra` | **not in this venv** | Aegra runs externally (prod) |

---

## Verdict

**Both protocols CONFORM. Streaming works and is SDK-proven.** The live prod lane
(Aegra) is conformant *by upstream construction*. The only streaming divergences
sit on `server.py`, the **disabled fallback** (`pux-prod.service` = disabled +
inactive), are **both LOW severity, both pinned by contract, and neither breaks
an SDK consumer**. There is no conformance emergency.

```
pux-aegra.service : enabled=enabled  active=active   ← PROD AP runtime (Aegra)
pux-prod.service  : enabled=disabled active=inactive ← server.py FALLBACK (down)
```

---

## 1. ACP lane — CONFORMANT (by delegation)

**File:** `pux-harness/pux_harness/acp.py` · **Class:** `_RegisteringAgentServerACP`
(subclasses `deepagents_acp.server.AgentServerACP`).

The wire is **100% delegated upstream**. We hand-emit zero `session/update`
notifications and hand-roll zero JSON-RPC framing:

- `run_acp_agent(...)` (`acp.py:291`) → upstream `acp` stdio transport.
- All `session/update` Part emission is the base class's job
  (`deepagents-acp` 0.0.8 + `acp` 0.11.0).

**Our 4 overrides — none touch the streaming path:**

| Override | super()? | What it adds | Streaming risk |
|---|---|---|---|
| `new_session` | ✅ | `register_thread` indexing | none |
| `initialize` | ✅ | truthful capability adj. (image gate, list/load) | none — only *adds* caps, never clobbers |
| `load_session` | new method (base lacked it) | re-hydrate per-session state | none |
| `list_sessions` | new method (base lacked it) | enumerate org threads | none |

We do **not** override `prompt()` or `cancel()` → streaming + cancellation are
upstream-owned.

**Streaming proven LIVE (real stdio subprocess, `tests/integration/test_acp_e2e.py`):**
- `agent_message_chunk` text → `_agent_text` joins streamed chunks (line 89).
- `tool_call` → `test_acp_live_streams_tool_call` asserts `"tool_call" in kinds` (line 242).
- Cancellation → `PromptResponse(stop_reason="cancelled")`, **not an error**
  (`test_acp_cancel_flips_cancellation_flag`; upstream sets `_cancelled`, prompt loop checks it).

**Coverage gap (NOT a conformance gap):** `plan`, `agent_thought`, `task_*`,
`interrupt`-as-stream Part types are not live-asserted. Whether they emit is
`deepagents-acp`'s decision (upstream responsibility); we simply lack live proof.

**ACP-0.11 drift watch (NEW, from this audit):** the bump 0.10.1→0.11.0 added
fields: `McpCapabilities.acp` (bool), `PromptCapabilities.audio`,
`PromptCapabilities.embedded_context`; `InitializeResult` is no longer exposed by
that name in `acp.schema` (we mutate the upstream-returned object, not construct
it — so safe). Our `initialize` leaves the new fields unset = truthful
non-advertise. **Action item:** confirm the mcp-capability contract asserts at
the field level (`http is False`, `sse is False`) and not a whole-dict equality
that the new `acp` key would silently drift.

---

## 2. Agent Protocol lane (LangGraph wire) — CONFORMANT

Two runtimes. `AP` here = the `langgraph_sdk` wire: HTTP REST
(`/threads`, `/runs`, `/store`, `/assistants`) + `/runs/stream` SSE.

### 2a. Aegra — PROD — CONFORMANT (by construction)

Aegra is an OSS `langgraph-api` drop-in (runs in its own env, not this venv).
By construction it emits the canonical upstream SSE set **including the terminal
`event: end`**. pux adds **no transform/wrapper** — pure passthrough.
`custom_app.py` mounts **only** pux-unique surfaces (`/events/*`, `/jobs/*`) via
the `http.app` seam; it does **not** serve `/runs/stream`.

### 2b. server.py — FALLBACK (disabled) — 2 documented streaming divergences

`_stream_run` (`server.py:1041`) hand-rolls frames via `_sse()`:
`f"event: {e}\ndata: {json}\n\n"` — exactly what `langgraph_sdk`'s `SSEDecoder`
parses (`sse.py:78`). Emits: `metadata` → `messages`/`updates`/`values` →
`error` (on exception). SDK-consumability is **proven** by a real
`SSEDecoder` in `tests/server/test_runs_stream_contract.py`.

| ID | Finding | Severity | Status |
|---|---|---|---|
| **S1** | No terminal `event: end` frame | **LOW** | pinned + documented |
| **S2** | `stream_mode` hardcoded (`server.py:1060`); `RunCreate`/`EphemeralRun` (111-132) carry no `stream_mode`/`config` field → clients can't select modes | **LOW-MED** | unpinned |
| **S3** | `interrupt`/`cancel` surface as `__interrupt__` inside `values`/`updates`, not a dedicated SSE event; resume via `command={"resume":…}` | INFO | spec-acceptable |

**S1 is NON-breaking — proven from source.** `langgraph_sdk/_shared/utilities.py:115`:

```python
def _sse_to_v2_dict(event: str, data: Any) -> dict[str, Any] | None:
    ...
    if event == "end":        # line 120
        return None           # line 121 — the SDK SKIPS the end sentinel
```

So the SDK treats `end` as a no-op; the run iterator terminates when bytes stop.
A stream without `end` is fully consumable (the SDK-consumability test passes
with a real `SSEDecoder`). `end` is a **wire-parity** nicety, not a consumer
contract. The pinned contract (`test_runs_stream_event_name_surface_is_pinned`)
documents this exact reasoning and asserts `{"metadata","messages","updates","values"}`,
flipping to include `end` when the upstream cutover lands.

**S2** diverges from the langgraph wire (which honors a `stream_mode` query
param). Non-breaking for tolerant consumers (they select/merge), but it is a
spec-fidelity gap on the fallback lane.

---

## 3. AG-UI (adjacent, not requested) — known token-translation gap

Per prior-session memory (not re-verified today): incremental `TEXT_MESSAGE_*`
token events are not emitted — langgraph chat-model deltas stream through as
`RAW` and the reply lands in `MESSAGES_SNAPSHOT`. Lifecycle + reply are
delivered; token-by-token translation is the open item. Out of scope for the two
named protocols.

---

## Severity-ranked findings

1. **S2 — `stream_mode` unparsed in server.py** (LOW-MED, fallback lane, unpinned)
2. **ACP-0.11 capability drift watch** (LOW — verify contract asserts field-level)
3. **S1 — missing terminal `event: end`** (LOW, pinned, proven non-breaking)
4. **ACP streaming coverage gap** — plan/thought/task not live-asserted (LOW, coverage only)

**Nothing is HIGH.** Nothing breaks an upstream SDK client. Nothing is live in prod.

---

## Recommended actions (await sign-off — this was an audit, not a change request)

- **R1:** Add `yield _sse("end", None)` as the final frame in `server.py:_stream_run`
  and flip the pinned contract to include `"end"`. (~3 lines + contract flip;
  aligns the fallback wire with upstream — `no-legacy-left-behind` cleanup.)
- **R2:** Honor `stream_mode` in `server.py` (`RunCreate`/`EphemeralRun` field +
  query, default to the current triple). (spec fidelity on the fallback lane.)
- **R3:** Add a unit test that our `acp.py` `initialize` override still yields a
  valid result under `acp` 0.11 (drift guard); optionally live-assert one
  non-text Part type when an org emits it.
- **R4 (optional):** re-confirm Aegra `/runs/stream` emits the full upstream set
  incl. `end` against a real key (already CLOUD-E2E-OK today; tautological from
  Aegra being langgraph-api).

---

## Evidence proven vs inferred

- **PROVEN (SDK source + tests):** `end`-sentinel semantics; server.py event set
  + hardcoded `stream_mode`; prod lane = Aegra live / server.py down; ACP
  delegation + override non-interference; all package versions.
- **INFERRED (by construction):** Aegra emits full set incl. `end` (it *is*
  langgraph-api). **INFERRED (memory, not re-verified):** AG-UI token gap.

---

## Update 2026-07-09 — gaps CLOSED (shipped + proven)

All recommended fixes landed and are proven green (verify-or-die):

- **R1 ✅** — `server.py:_stream_run` now emits the terminal `event: end`
  (`data: null`) as an unconditional trailing yield (fires on success + caught
  exception; skips on client disconnect/`CancelledError`). The pinned contract
  flipped to `{"metadata","messages","updates","values","end"}`.
- **R2 ✅** — `stream_mode` is now honored: `RunCreate`/`EphemeralRun` carry a
  `stream_mode` field; both routes accept a `?stream_mode=` query param (how
  `langgraph_sdk` ships it); `_resolve_stream_modes` normalizes single/comma/list
  → default triple; the emission loop generalizes (event name = mode name). The
  hardcoded literal is gone.
- **R3 ✅** — acp-0.11 drift guard: the capability contract now field-level-pins
  `McpCapabilities.acp`, `PromptCapabilities.audio`, `embedded_context` all stay
  unadvertised (verified falsy under 0.11). No `acp.py` change needed (schema
  defaults are already falsy).

**Test proof:**
- `tests/server/test_runs_stream_contract.py` — **8/8** (incl. 3 new:
  `test_runs_stream_emits_terminal_end_frame`,
  `test_runs_stream_end_frame_after_error`,
  `test_runs_stream_honors_requested_stream_mode`).
- `tests/server/test_acp.py::test_acp_advertises_session_load_and_list` — **PASS**
  (real stdio subprocess).
- `pux-harness/tests/server/test_server_interactive.py` — **6/6** (no `end`-frame
  regression; the suite uses subset assertions).

**Remaining (optional, not blocking):** R4 (re-confirm Aegra `/runs/stream` emits
`end` against a live key — tautological from Aegra being langgraph-api; already
CLOUD-E2E-OK). AG-UI token-translation gap unchanged (out of the two named
protocols). server.py remains the **disabled fallback**; prod (Aegra) was already
fully conformant.
