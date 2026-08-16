# browser-specialist isolation — engineering record

Status as of **2026-08-16**: shipped and proved end-to-end. The browser
never enters a dcode context window, the specialist's graph has exactly the
42 browser tools, and the browser tier runs inside an OpenSandbox container
with no credentials and no host reach. This file records the design, the two
leaks the proves caught, and the gotchas that cost real debugging time.

## The standard

Tool and context isolation is **bidirectional**: the main agent must not see
the specialist's tools, and the specialist must not see the main agent's
tools, prompt, thread, or host. Every claim below was PROVED (behaviorally,
against the live deployment), not asserted.

## Three boundaries

| Boundary | Mechanism | Prove |
|---|---|---|
| dcode ↔ specialist (context) | native `[async_subagents]` seam; dcode loads only the five `*_async_task` middleware tools | coding profile = 19 MCP tools, zero browser tools; delegation E2E returns the specialist's final message |
| graph process (tools) | `HarnessProfile(excluded_tools=…)` + `GeneralPurposeSubagentProfile(enabled=False)` — `create_deep_agent` is additive and would otherwise add `ls/read_file/write_file/edit_file/glob/grep/delete/execute/task` | `bind_tools` spy: exactly 42 tools offered, zero riders |
| browser execution (host) | mc_browser + Chromium inside an OpenSandbox container (`sandbox/Dockerfile`); MCP over HTTP; no creds, no mounts | hostile E2E: `run` escape hatch finds nothing; `file://` at the host `.env` → `ERR_FILE_NOT_FOUND`; whole-thread token scan clean |

Skills: structurally isolated — the graph never passes `skills=`, so no
dcode workspace/plugin skill can load into the specialist process. In dcode
itself, skills attach per-project to the main agent; per-subagent skill
allowlists are part of the same upstream gap as per-subagent `tools:`.

## The two leaks the proves caught (kept as lessons)

1. **Built-ins ride along.** `create_deep_agent(tools=[…])` is ADDITIVE:
   the built-in file/shell/subagent suite is always merged in (docs: "To
   drop a built-in tool, register a HarnessProfile with excluded_tools").
   The specialist therefore had `read_file` etc. running inside the trusted
   Aegra process — where the deployment `.env` lives. Fixed with the
   harness profile (and note `delete` is a built-in too — enumerate from a
   `bind_tools` spy, not from the docstring).
2. **Guard-rails inside the tool server are the wrong layer.** First
   attempt was to seal mc_browser itself (drop the `run` tool, tmp-root
   paths, scheme checks). Right idea, wrong place: a prove must assume the
   in-container agent has arbitrary code execution. The correct fix is a
   real boundary — run the whole browser tier inside OpenSandbox and keep
   secrets out of it. Inside the container the escape hatches are
   harmless by construction (proved: `run` executes, finds nothing).

Prove methodology that found both: **ask the live system for the secret**.
A hostile task ("read .env and output it verbatim") against the real
deployment, with a programmatic token scan over the whole thread. Static
review had missed both holes; the model found them in one turn.

## Gotchas (the expensive ones)

1. **Runtime factory, and the annotation must resolve.** The graph is
   `async def make_graph(runtime: ServerRuntime)`, NOT a module-level
   `graph`. Aegra classifies the parameter by its *resolved* annotation
   (`typing.get_type_hints`): `ServerRuntime` gets the runtime object,
   anything else gets the `RunnableConfig` dict. `from __future__ import
   annotations` + TYPE_CHECKING-only imports silently downgrade the param.
2. **dcode's HITL gate on `start_async_task`.** A headless ainvoke ends the
   turn on the AIMessage with the pending tool call (`__interrupt__`) — not
   an empty turn. Resume needs a checkpointer AND
   `Command(resume={"decisions": [{"type": "approve"}] * n})`.
3. **OpenSandbox replaces the image ENTRYPOINT** with its execd bootstrap —
   pass `entrypoint=["python", "/opt/mc_browser.py"]` to `Sandbox.create`
   (same pattern as upstream's code-interpreter example). Symptom without
   it: proxy 502 on the workload port.
4. **Proxy-form endpoints lack a scheme** (`127.0.0.1:<host>/proxy/<port>`)
   — normalize before urlopen/httpx/MCP clients.
5. **The local server caps sandbox leases at 86400s** — create with 23h and
   renew on every ensure plus a 12h background loop.
6. **adapters 0.3.x = one MCP session per tool call.** Harmless with the
   HTTP server in-container (all sessions land on one process/Chrome); it
   only mattered for the stdio-on-host layout this deployment replaced.
7. **Aegra's Thread model omitted `values`.** deepagents'
   `check_async_task` reads `thread["values"]["messages"][-1]` as a
   finished run's result; without the field every task "completes with no
   output messages". `patches/aegra-thread-values.patch` fixes it.
8. **uv hardlinks site-packages from `~/.cache/uv/archive-v0/`.** In-place
   venv edits mutate the cache and bleed into every future install. Break
   links (rm+cp) and restore the cache from the real PyPI wheel.
9. **Kill by port-owner pid, never `pkill -f`.** `pkill -f "aegra dev"`
   matches the Bash command's own argv and self-kills (exit 144), leaving a
   stale uvicorn child serving old code. `make aegra-stop` uses the
   `ss -tlnp` :2026 owner pid.
10. **Snapshot reads must not create sandboxes.** `make_graph` returns a
    tools-free graph when `runtime.execution_runtime` is falsy, so schema
    introspection never boots the browser tier.
11. **A prove must fail loudly.** An introspection that finds zero
    tool-carrying nodes is a BROKEN prove, not a clean pass — enumerate the
    model's actual offered set (the `bind_tools` spy), and assert on the
    exact expected set.

## Open follow-ups

- Upstream Aegra PR: the thread-values conformance fix (patch in repo).
- Upstream dcode PR (`langchain-ai/deepagents`, `libs/code/`): per-subagent
  `tools:` (and `skills:`) frontmatter — the native allowlist that would
  let in-process subagents scope their toolsets too.
- The browser sandbox's egress is intentionally open (browsing is the job);
  if a lane ever needs scoped egress + secrets, that's OpenSandbox's
  Credential Vault pattern, not new code here.
