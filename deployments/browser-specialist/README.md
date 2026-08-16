# browser-specialist — the isolated 42-tool stealth browser

One sentence: the browser never enters a dcode context window, and the
browser tier cannot touch this host. The graph is deepagents-core's
`create_deep_agent` with the workspace's browser MCP server attached;
it is deployed on [Aegra](https://aegra.dev) (self-hosted LangGraph Platform
alternative) and exposed to dcode through the **native `[async_subagents]`
seam** in `~/.deepagents/config.toml` — the main agent gets the five
`*_async_task` tools, nothing else.

## Isolation — three boundaries, each in its natural place

1. **dcode ↔ specialist (context):** dcode loads only the five
   `*_async_task` middleware tools; none of the 42 browser tool schemas
   enter any dcode session. The specialist likewise has none of the main
   agent's tools — its graph is built from scratch with the browser MCP.
2. **The graph process (tools):** `create_deep_agent` is additive — the
   built-in file/shell/subagent suite (`ls, read_file, write_file,
   edit_file, glob, grep, delete, execute, task`) would ride along and hand
   the agent host-filesystem access *from inside the trusted Aegra tier*
   (this deployment's `.env` holds the model token). The sanctioned seam is
   a `HarnessProfile` with `excluded_tools` +
   `GeneralPurposeSubagentProfile(enabled=False)` — the graph's toolset is
   then EXACTLY the 42 browser tools (proved by a `bind_tools` spy).
3. **Browser execution (host):** mc_browser and its Chromium run INSIDE an
   [OpenSandbox](https://github.com/opensandbox-group/OpenSandbox) container
   (`sandbox/Dockerfile`) — the platform this workspace already runs on
   `:8080`. One long-lived sandbox serves MCP over HTTP; it carries no
   credentials, no host mounts, and no host reach. A live hostile prove had
   the agent run its `run` escape hatch (arbitrary Python) and `file://`
   navigation straight at the host `.env` path: nothing to find
   (`ERR_FILE_NOT_FOUND`, empty `/home`). Escapes are bounded by the
   container — which is the point: a real boundary instead of guard-rails
   inside the tool server.

Skills are structurally isolated too: the graph never passes `skills=`, so
no dcode workspace/plugin skill loads into this process; a specialist skill
would be declared in this deployment and nowhere else.

## Layout

```
aegra.json                 graph registry (browser_specialist → graph.py:make_graph)
pyproject.toml             the deployment env (aegra-cli, deepagents, adapters, opensandbox)
sandbox/Dockerfile         the OpenSandbox workload image (mc_browser + Chromium, MCP HTTP)
sandbox_ctl.py             ops CLI for the sandbox (status / kill)
patches/
  aegra-thread-values.patch  Agent Protocol conformance fix (see below)
src/browser_specialist/
  graph.py                 make_graph(runtime) — runtime factory + the sealed toolset
  sandbox.py               browser sandbox lifecycle (connect-or-create, renew, endpoint)
  utils.py                 the model (ChatOpenAI against the pux-openai endpoint)
.env                       NOT committed — ports, auth, ANTHROPIC_AUTH_TOKEN
.sandbox_id               NOT committed — the persisted workload sandbox id
```

## Run

```bash
cp .env.example .env        # fill ANTHROPIC_AUTH_TOKEN (same key as dcode's model)
make aegra-sandbox-image    # once (and after mc_browser.py changes): build the workload image
make aegra                  # patches venv, checks the image, starts Aegra + waits
make aegra-status           # Aegra + sandbox health
make aegra-sandbox-status   # sandbox id/state/MCP endpoint
make aegra-stop             # stop Aegra (the browser sandbox is left running by design)
make aegra-sandbox-kill     # teardown the browser sandbox (Chrome state dies with it)
```

Aegra serves on `127.0.0.1:2026` with its own Postgres (`5433`) via
`aegra dev`'s docker compose. The browser sandbox is created on the first
browser task (lease 23h — the server caps at 24h — renewed every 12h and on
every Aegra restart), so tabs/cookies survive Aegra restarts.
`AUTH_TYPE=noop` is fine for the single-user box; put it behind real auth
before exposing the port.

## How it stays native

- **Runtime factory, not import-time build.** `make_graph(runtime)` is the
  langgraph-sdk contract Aegra invokes inside its own event loop; the
  annotation must resolve at runtime (no `TYPE_CHECKING`-only imports) —
  Aegra classifies the parameter by its resolved annotation: `ServerRuntime`
  gets the runtime object, anything else gets the `RunnableConfig` dict.
- **The conformance patch.** Aegra's `GET /threads/{id}` omitted `values`
  from its `Thread` model, but deepagents' `check_async_task` reads
  `thread["values"]["messages"][-1]` as the completed task's result —
  without it every finished task reports "(completed with no output
  messages)". `patches/aegra-thread-values.patch` adds the field +
  populates it from the latest checkpoint (best-effort). `make aegra-patch`
  applies it to the venv after `uv sync`; the same change is prepared as an
  upstream PR.
- **MCP over HTTP into the container.** langchain-mcp-adapters 0.3.x opens
  a new session per tool call — harmless here: all sessions land on the one
  long-lived mc_browser HTTP process inside the sandbox, which owns the one
  Chromium.

## The dcode seam

```toml
# ~/.deepagents/config.toml
[async_subagents.browser-specialist]
description = "42-tool stealth browser (SeleniumBase Pure CDP) — navigate, read, click, type, forms, dropdowns, tabs, sessions/cookies, screenshots, captcha solving, warmup. Runs in an isolated OpenSandbox container. Returns structured page state, never raw HTML."
url = "http://127.0.0.1:2026"
graph_id = "browser_specialist"
```

Delegate inside any session: *"ask browser-specialist to open X and report
the title"* — the agent calls `start_async_task` (dcode's HITL gate asks
once), polls `check_async_task`, and reports the specialist's final message.

Env knobs: `BROWSER_SANDBOX_IMAGE`, `BROWSER_SANDBOX_PORT`,
`BROWSER_SANDBOX_ID_FILE`, `OPENSANDBOX_DOMAIN`, `OPENSANDBOX_API_KEY`
(see `.env.example`). The subagent model is `BROWSER_SPECIALIST_MODEL` /
`BROWSER_SPECIALIST_BASE_URL` (defaults: `glm-5.2` @ the pux-openai
endpoint). mc_browser keeps its own stdio + persistent-Chrome modes for
direct local use; the deployment uses neither.
