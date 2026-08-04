# Coder org — capability map

> So we don't research this again. Tested 2026-07-12 against `pux-harness` master,
> `toad 0.6.20`, coder base model `glm-5.2`.

The **coder** org (`orgs/specialists/coder/`) is the Claude-Code-equivalent
dev-bot. This doc records the two capabilities operators ask about, the exact
wire path for each, and how to re-verify.

---

## (a) Web-browser subagent — `web-agent` (already present)

**Status: shipped.** The roster is exactly
[`coder-explorer`, `code-worker`, `web-agent`] and is pinned by a permanent
contract tripwire
(`tests/contract/test_org_contract.py::test_coder_roster_on_real_repo`).

### What it is

`orgs/specialists/coder/agents/web-agent.md` — an **E2E web verification
specialist** (100 lines). It is NOT the generic shared browser agent; it is
purpose-built for the coding loop: load the deployed page, assert elements
exist, fill + submit forms, capture screenshot evidence, return a PASS/FAIL
report. The CTO delegates the live-browser verify step to it (see `AGENTS.md`
§Workflow step 4).

### Tool surface

It uses the **`pux_sandbox_browser_*`** specialist family (SeleniumBase Chrome
via `sb_server`, in-sandbox). The full set is registered at
`pux-harness/pux_harness/sandbox/tools/registry.py` (27 tools:
`browser_navigate`, `browser_click`, `browser_type`, `browser_screenshot`,
`browser_evaluate`, `browser_search`, `browser_save_session`,
`browser_restore_session`, …). The sandbox self-boots lazily on first call;
`orgs/_shared/sandbox/warmup_browser.py` pre-warms Chrome to skip the
SeleniumBase CDP-attach cold start.

### Why not the shared generic `browser.md`?

`orgs/_shared/agents/browser.md` exists as a broader web-browsing specialist
(search/read/interact). Coder's `web-agent` is intentionally narrower —
verify-and-report — to fit the dev-bot ship gate. Both exist; coder uses its
own. (If a future org wants the generic browser agent, roster the slug
`browser` and resolution falls through `_shared/agents/browser.md`
automatically — `load_subagents` is org-local first, then `_shared`.)

### Verify

```bash
./pux-harness/.venv/bin/python -m pytest tests/contract/test_org_contract.py -k coder -q
# 11 passed — includes the roster pin + the web-agent tool wiring assertions.
```

---

## (b) QuickJS interpreter (`CodeInterpreterMiddleware`) — pinned ON

**Status: auto-mounted + explicitly pinned.** The deepagents
`CodeInterpreterMiddleware` (`langchain-quickjs`) injects an `eval` tool — a
sandboxed in-process QuickJS JavaScript REPL — plus a `task(...)` global so the
CTO writes one short dispatch script (recon → `Promise.all` workers →
synthesize) instead of grinding items one model-chosen tool call at a time.
See https://docs.langchain.com/llms.txt → "Interpreters".

### How it's wired (two independent reasons it's on)

1. **Auto-mount (strength gate).** Coder's base model is `glm-5.2`, flagged
   `strength: pro` in `pux-harness/pux_harness/agent/models.yaml`.
   `driver_strong_orchestrator(role="base", org="coder")` → `True` →
   `stack._resolve_toggles` adds `interpreter` to the supervisor on-set.
2. **Explicit pin (belt-and-braces).** `orgs/specialists/coder/profile.yaml`
   carries a `middleware: { supervisor: { add: [interpreter] } }` block. The
   `add` override wins over the strength gate's off-path, so the interpreter
   stays on even if:
   - someone runs `--fast` / `PUX_TIER=fast` (mimo-v2.5 is `strength: flash`),
   - the base model is later repointed away from a `pro` id,
   - a reader greps the profile for "what can this org do?"

### Where the code lives

- Registry: `pux-harness/pux_harness/agent/stack.py` →
  `MiddlewareSpec("interpreter", {Scope.SUPERVISOR}, _build_interpreter)`
  (appended last — it's a tool-injector, not a model/tool-call wrapper, so mount
  order doesn't affect the wrap pipeline).
- Factory: `stack._build_interpreter` — lazy-imports
  `langchain_quickjs.CodeInterpreterMiddleware` on first strength:pro build so
  weak-model builds don't load the quickjs/wasmtime native libs. PTC
  (programmatic tool calling) is armed with a **read-only discovery allowlist**
  (`glob`/`grep`/`ls`/`read_file`); mutations still go through `task(...)`
  subagents. `subagents=True` exposes `task(...)` only when the org already has
  the deepagents `task` tool (roster-driven).
- Profile schema: `pux-harness/pux_harness/agent/profile.py` →
  `load_middleware_overrides` / `MiddlewareOverrides`; validated by
  `stack.validate_overrides(org)` (every add/remove name must be registered +
  in-scope).

### Live verification (run from repo root)

```bash
OPENROUTER_API_KEY=dummy ANTHROPIC_AUTH_TOKEN=dummy \
./pux-harness/.venv/bin/python -c "
import os; os.environ.setdefault('PUX_PROJECT_ROOT', '$(pwd)')
from pux_harness.agent.stack import build_stack, RuntimeFacts
from pux_harness.agent.profile import load_profile, load_rubric_gate
from pux_harness.agent.graph import shared_exec, shared_backend
from pux_harness.sandbox.tools import build_native_specialists
from pux_harness.agent.model import get_model, resolve_model_id, driver_strong_orchestrator
ex, bk = shared_exec(), shared_backend()
specs = build_native_specialists(ex, vision_model=get_model(role='multimodal', org='coder'), org='coder', backend=bk)
plan = build_stack('coder', specialists=specs, profile=load_profile('coder'),
                   rubric_gate=load_rubric_gate('coder'), exec_client=ex, facts=RuntimeFacts(transport='acp'))
mw = [type(m).__name__ for m in plan.supervisor_middleware]
print('supervisor middleware:', mw)
print('CodeInterpreterMiddleware mounted?', any('CodeInterpreter' in n for n in mw))
print('base:', resolve_model_id(role='base', org='coder'), '| strong?', driver_strong_orchestrator(role='base', org='coder'))
"
```

Expected (last lines):

```
supervisor middleware: ['ContextMiddleware', 'RoutingMiddleware', 'SessionGuideMiddleware',
  'PromptCaptureMiddleware', 'RubricMiddleware', 'ModelRetryMiddleware',
  'BrowserVisionMiddleware', 'CodeInterpreterMiddleware']
CodeInterpreterMiddleware mounted? True
base: glm-5.2 | strong? True
```

(The `eval` tool itself is NOT in `plan.supervisor_tools` —
`CodeInterpreterMiddleware` injects it at graph-compile time inside
`create_deep_agent`, not in the static plan tool list. Expected.)

### To turn it OFF for coder

```yaml
# orgs/specialists/coder/profile.yaml
middleware:
  supervisor:
    remove: [interpreter]   # remove wins over the strength gate's on-path too
```

### Security posture (unchanged from upstream)

QuickJS is same-process, no host fs/network/shell by default. The PTC allowlist
is the permission boundary — pux ships it read-only (discovery only). For
untrusted code, run the agent in the Docker sandbox (which is what `pux acp`
already does — browser/sandbox tools exec in-container). See the interpreters
doc §Security.

---

## Related docs

- `docs/toad-integration.md` — ACP as the universal editor pattern (toad /
  Aethna / Hermes all consume `pux acp`).
- `AGENTS.md` (repo root) — the per-org harness profile field reference.
- `pux-harness/pux_harness/agent/stack.py` — the single factory that compiles
  every org's stack (`build_stack`).
