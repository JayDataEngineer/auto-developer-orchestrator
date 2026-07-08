# Dynamic Tools (level c) + Agent Package Engine

**Status:** DESIGN (2026-07-08). **Part 1 (level c) shipped** as P1 (2026-07-08):
`pux_harness/sandbox/tools/dynamic.py` + the four `pux_dyn_*` tools + `lib/` skeleton +
`sandbox.dynamic_tools` opt-in, proven (24 unit tests incl. the bounded-result thesis; all
real-org contract builds green; `pux check-contract` clean for all 10 orgs). **P2 shipped**
(2026-07-08): graduation (`pux promote-function`, c→b, lib→git-tracked `sandbox/functions/`)
+ pruning (`pux archive-function`, reversible `.archive/`) — proven (9 unit tests incl. a
real-python3 graduation proof; live CLI round-trip on `invest`). **P3 shipped (2026-07-08):**
declarative manifest (`pux_harness/manifest.py` — default-deny `package.include` globs +
PERMANENT `HARD_EXCLUDE`) drives `pux pack` (renamed from `export`, which now HARD-ERRORs —
Decision 5); `pux lock` + `org.lock.yaml` (github MCP refs → commit SHAs via host-side
`git ls-remote`, best-effort; declared pip/apt deps snapshotted) pin the org's external deps
(committed by default — Decision 4) and ship in the pack. Proven (50 manifest/lockfile unit
tests incl. the pack-contents==manifest contract + the legacy-allowlist-removed permanent
failure; 128 export/cli tests; live `pux lock --org general` resolved a real SHA; `pux pack`
ships `org.lock.yaml`; `pux export` hard-errors exit 1). **P4 shipped (2026-07-08):**
`pux_harness/pack_hooks.py` — the `PACK_HOOK_REGISTRY` (`ast_check_hook` via stdlib `compile`,
`gitleaks_hook` via the host-side CLI, reuse-first). Hooks run AFTER collection, BEFORE the
tarball is written — a broken agent function or a leaked secret REFUSES the pack
(`PackHookError`); results seed `manifest.json` → `provenance.hooks` (the P5 audit surface).
`data/`/`.pux/` HARD_EXCLUDE remains the PRIMARY secret boundary; gitleaks is the
defense-in-depth scan over what ships. Proven (23 hook unit tests via an injected runner +
5 `pack_org` integration tests; **live**: planted `ghp_` PAT → `pux pack` REFUSES exit 1
[github-pat + generic-api-key rules]; broken AST → refuses; clean `general` → packs +
records provenance). Part 2's remaining phases (P5 OCI, P6 signing) stay design — feed the
`upstream-protocol-pivot` **P4 (export rework)**.

**Posture (the headline):** reuse upstream packaging primitives; own only the thin pux
glue. OCI-via-`oras` (mature), gitleaks/`ruff`/`uv` (already in repo), APS manifest
(align-with-shape, conform-later — do **not** hard-depend on a v0.1 spec).

---

## Part 1 — Level (c) Dynamic Tools

### The three tool levels (recap)
- (a) **MCP / universal** — fixed bodies, agent can't change. ✅ shipped (REGISTRY + MCP consumer).
- (b) **Pythonic / git-tracked** — operator-authored, in-container. ✅ shipped (`sandbox/tools/tools.yaml`).
- (c) **Dynamic** — agent-authored at runtime, persistent, prunable; the org *learns*. ❌ **missing rung.**
- Monotonic per-call context curve (a)>(b)>(c); (c) is the only one that *compounds* → the real lever for low-power agents. This is the pre-pivot `make_script`/`edit_script` concept (deleted `961a762`).

### Placement: `orgs/<org>/lib/` (NOT `.pux/`)
`.pux/` is the **transient runtime** namespace (`_scaffold_workspace`, `container.py:617`, makes
`.pux/sessions`). Level (c) is durable + portability-essential — the opposite — so it gets its own
dir, **gitignored but pack-included**:

```
orgs/<org>/
├── AGENTS.md, agents/, policy.yaml   # brain + contract (operator-curated)
├── skills/                           # backbone context (operator, markdown, host)
├── sandbox/                          # (b) operator-authored TYPED tools + tools.yaml
├── lib/                              # (c) AGENT-authored, evolving, gitignored, PACKS
│   ├── functions/  (__init__.py + one module per fn: def run(**kw): ...)
│   ├── index.yaml  (metadata source of truth — travels)
│   └── .archive/   (pruned, reversible)
├── data/                             # runtime I/O (secrets) — gitignored, never packs
└── .pux/                             # transient runtime (sessions) — gitignored, never packs
```

| Dir | Level | Owner | Git | Pack |
|---|---|---|---|---|
| `sandbox/` | (b) pythonic | operator | tracked | ✅ |
| `lib/` | (c) dynamic | **agent** | ignored | ✅ |
| `data/` | runtime I/O | runtime | ignored | ❌ (secrets) |
| `.pux/` | transient | runtime | ignored | ❌ |

This *reduces* the existing mess: today `sandbox/` conflates typed tools, loose helpers, and shell
scripts with no owned home for agent code. `lib/` gives agent code a named home; it can't pollute curated tools.

### Mechanics — one module, four tools, reuse in-repo machinery
New `pux_harness/sandbox/tools/dynamic.py`, rhyming with `declared.py` (synthesizes `StructuredTool`s
whose `func` exec's in-container via `DockerExecClient`). Four tools in `build_stack`, **opt-in** via
`sandbox.dynamic_tools: bool` (same shape as `display.watch`):
`make_function(name, description, code)`, `edit_function`, `list_functions` (index only — cheap),
`call_function(name, **kw)` (exec `from functions.<name> import run; run(**kw)` in-container; bumps usage/success).

- **Persistence is free:** the project is bind-mounted 1:1 (`<project>:/sandbox/workspace`,
  `container.py:321`), so host-written `lib/` is immediately container-visible AND host-durable —
  `lib/` survives teardown *by construction*. **Zero `container.py` changes.**
- **Authoring is HOST-side, execution is IN-CONTAINER** (the split that matters — proven in P1):
  `make`/`edit` write `lib/functions/<name>.py` + `lib/index.yaml` via `pathlib` from the pux process
  (host-owned — **NOT root**; sidesteps the `_scaffold_workspace` chown trap entirely). `call_function`
  exec's `cd <lib_container_dir> && _PUX_DYN_KWARGS=<json> PYTHONDONTWRITEBYTECODE=1 python3 -c
  "<runner>"`; for `python3 -c` the cwd is `sys.path[0]`, so `from functions.<name> import run`
  resolves with `lib/functions/__init__.py` present (created on first use). **No `PYTHONPATH` change,
  no scaffold change** — composition is free, and kwargs ride an env var (no shell-quoting arbitrary
  values).
- **Bookkeeping source of truth = `lib/index.yaml`** (name→{desc,usage,success,created,provenance,version});
  the Store = optional host read-cache. index.yaml travels in the pack; the Store doesn't.

### Lifecycle: persistence (free) · pruning (rubric-gated job → `.archive/`, load-bearing) ·
graduation (`pux promote-function` lib/→sandbox/, c→b, git-tracked, travels everywhere).

### Caveats
- `call_function` exec inherits the org **egress ACL + audit** (the `declared.py` discipline).
- **Not `.py`-as-agent** — library is tooling the agent *extends*; the deepagents loop is unchanged.
- **Distinct from skills** — skills = operator markdown backbone (host); lib = agent executable code (container).

---

## Part 2 — Agent Package Engine (export rework)

### What professionals actually use (researched 2026-07-08)
1. **Manifest-driven archive** (npm `package.json` `files` / Python wheel `MANIFEST.in`): the package
   *declares* what ships via include globs; anything unmatched is left behind (**default-deny**).
2. **OCI artifact** (CNCF; Helm-v3-in-OCI; ModelPack; WasmCloud): non-container resources packaged as
   OCI artifacts — content-addressed layers under a standard manifest — so ordinary registries
   (GHCR/Docker Hub) distribute/version/sign them. `oras` is the CNCF tool for this.
3. **Pre-publish hook pipeline** (`prepack`, lint, AST secrets-scan, Cosign): the pack is a *build*
   through an extensible hook chain, not a `rglob`.

Emerging agent-specific standard: **APS** (`agentpackaging.org`, `apstool`, Apache-2.0, OCI-inspired,
**v0.1**) — manifest (`agent.yaml`) + package + registry API + optional provenance/signing. Also exists:
**`.agent`** (`nomoticai/agentpk`, ⚠️ **patent-pending → ruled out** as a dependency).

### Reuse map — own the glue, not the primitives
| Component | Upstream (REUSE) | pux (OURS — thin glue) |
|---|---|---|
| OCI artifact build + (later) registry push | **`oras`** CLI binary (CNCF; shell-out — Decision 2) | layer mapping (config/source/lib), `org.pux.*` annotations |
| Manifest schema | **APS `agent.yaml`** (Apache-2.0) — *align-with shape, conform-later* | pux-owned schema, APS-shaped; pux capability annotations |
| Secrets scan | **gitleaks** (`.gitleaks.toml` already in repo) | hook wiring; scope over `lib/functions/*.py` |
| AST / syntax lint | **`ruff`** / `compileall` (already dev-dep) | hook wiring |
| pip lockfile | **`uv lock`** (workspace is already uv) | MCP-commit pinning (small) |
| Hook orchestration | — | **`PACK_HOOK_REGISTRY`** (mirror `MIDDLEWARE_REGISTRY`) |
| `lib/` dynamic tools | `DockerExecClient`, `declared.py` pattern (in-repo) | `dynamic.py` (4 tools) + `index.yaml` |

**Honest read on APS:** it's a small/new project (`vedahegde60/agent-packaging-standard`), v0.1. Betting
*core packaging* on a v0.1 lib is a risk. So: **don't hard-depend.** Make the pux manifest schema
**APS-shaped** (same field names/structure for identity/interface/runtime/deps/capabilities) so flipping
to full APS conformance later is a config change, not a rewrite. The solid, mature reuse is **`oras`** —
we do NOT hand-roll `oci-layout`/`index.json`/blobs/digests.

### The current hack, named
`export.py::_collect_org_files` (274–301): hardcoded dir allowlist `("agents","skills","sandbox","config")`
+ core files; `data/` excluded by hand-comment. Unsigned, unmanifested `.tar.gz`. What ships is implicit
in a Python function, not declared by the org. Secrets excluded reactively (the `data/` leak fix), not by design.

### The rework (reuse-first)

#### 2.1 Declarative manifest + lockfile (`org.yaml`)
`org.yaml` gains `package:` (**default-deny** include globs + exclude belt-and-suspenders),
`capabilities:` (the receiver's **audit surface** — declared egress/tools/rw-paths, surfaced from
`policy.yaml`, NOT the secrets), and `dependencies:` (surfacing `sandbox.deps` + MCP servers).

```yaml
manifest_version: "2.0"     # APS-shaped field names; pux-owned schema
name: "financial-analyst"
version: "1.4.2"
package:
  include:                  # default-DENY: only these ship
    - "AGENTS.md"
    - "policy.yaml"
    - "sandbox/**/*.py"     # (b)
    - "lib/**/*.py"         # (c)
    - "lib/index.yaml"
  exclude:
    - "**/__pycache__"
    - "data/**"
    - ".pux/**"
capabilities:               # receiver inspects this BEFORE running (not inferred from code)
  egress: ["api.alpaca.markets", "api.twitter.com"]
  tools: ["scan_signals", "detect_regime"]
  dynamic_tools: true
  rw_paths: ["data/", "memos/"]
dependencies:
  mcp_servers:
    - { name: "postgres-connector", source: "github:github/postgres-mcp", version: "v1.2.0" }
```

`org.lock.yaml` — **generated by `uv lock` (pip) + pux glue (MCP commit pins)**, ships in the config
layer; optionally repo-committed for dev reproducibility:
```yaml
lock_version: "2.0"
resolved_at: "<iso8601>"          # passed in at pack time
dependencies:
  mcp_servers:
    - { name: "postgres-connector", resolved: "<commit-url>", integrity: "sha256-..." }
  pip_packages:
    - { name: "deepagents", version: "1.8.4", hash: "sha256-..." }   # from uv lock
```

#### 2.2 Pre-pack hook pipeline (registry, not hardcoded)
`pux pack` runs a pluggable hook chain via **`PACK_HOOK_REGISTRY`** (mirrors `MIDDLEWARE_REGISTRY` —
extensible, the "prepack" pattern). Any failure aborts the build:
`SchemaValidation` (`org.yaml` JSON-schema) · `StaticASTLint` (**`ruff`/`compileall`** over
`sandbox/`+`lib/` — structural/syntax level only; *behavioral* checks are runtime governance, out of
packaging scope) · `SecretsScanning` (**gitleaks** over all shipped source, **esp. `lib/functions/*.py`**
— the new agent-code leak vector; continues the `data/` contract) · `ProvenanceGenerator` (SHA-256 per
file → immutable lineage). Signing hook (Cosign/Ed25519) added in a later phase.

#### 2.3 OCI-artifact output — via `oras`, NOT hand-rolled
`pux pack` produces an **OCI artifact** built by **shelling out to the `oras` CLI binary** (Decision 2 —
not `oras-python`). `oras` must be on `PATH` where pux runs (pack is **host-side**, not in-container);
`pux pack` fails clearly if absent. `oras` handles digests/manifest/`oci-layout`/`index.json` — we do
not. Custom config mediaType
(`vnd.pux.org.config.v1+json`) + layer annotations (`org.pux.layer.type: source-code | agent-library |
config`). Output is consumable by `oras`/`crane`/`skopeo` and pushable to GHCR/Docker Hub later
(`oras push`) **without repackaging**. Integrity digests cover mutable `lib/`, so a tampered learned
function is detectable on verify.

### How level (c) travels (the coupling)
`lib/` + `index.yaml` are a declared source layer; `.pux/`+`data/` declared not-shipped. Image digest
covers the library layer; gitleaks scrubs `lib/functions/*.py`. Consumer unpacks an OCI artifact with a
`lib/` folder (not `.pux/`), self-contained (code + index.yaml, no host Store/sqlite) — `compile_org`/`run.py` drives it.

### Three portability pathways
1. **Git (template):** share repo; `lib/`+`.pux/` gitignored → clean template, agent learns from scratch.
2. **Pack (live):** `pux pack` → hooks scrub → signed layered OCI artifact with the full pre-evolved library; consumer starts capable.
3. **Graduation (permanent):** `pux promote-function` → lib/→sandbox/ (c→b, git-tracked) → travels via Git AND Pack.

### Migration / no-legacy-left-behind
- `pux export` → **`pux pack`**. Old verb + `_collect_org_files` allowlist + `test_collect_org_files_excludes_data_dir`
  all become **permanent contract failures** (no silent alias). The manifest's `exclude: data/**` is the new permanent form of the `data/` exclusion.

### Mapping: hack → pro
| Current (hack) | Professional (reused) |
|---|---|
| hardcoded dir allowlist | `package.include` default-deny globs (npm `files`/wheel `MANIFEST.in`) |
| `data/` excluded by hand-comment | `package.exclude: data/**` + declared secret-requirements |
| unsigned tarball | OCI digest + Cosign (via `oras`) |
| no capability surface | `capabilities:` audit surface |
| `pux export` hand-passed tarball | `pux pack` → `oras` OCI artifact → `oras push` |
| no content audit | hook pipeline: `ruff` + gitleaks + provenance |
| runtime MCP resolution | `org.lock.yaml` (`uv lock` + MCP pins) |

---

## Phased path (prove per phase; verify-or-die)
- **P1 — Level (c) minimal slice** (`invest`): `lib/` + `dynamic.py` (make/call/list) + `index.yaml` + opt-in flag. Prove: repeated task's per-turn context drops after the org "learns" it.
- **P2 — Pruning + graduation ✅ SHIPPED (2026-07-08):** `pux promote-function` (lib→git-tracked
  `sandbox/functions/`, c→b — same `def run` runner; agent can call but not edit) +
  `pux archive-function` (lib→`lib/.archive/`, reversible). The rubric-gated *auto*-prune job
  is deferred (the manual archive mechanism + reversibility landed). Proven: promoted fn runs
  in-container from its tracked location (real-python3 value=60); promoted fn's usage/success
  history preserved across graduation; 9 unit tests + live CLI round-trip on `invest`.
- **P3 — Manifest + lockfile + default-deny pack ✅ SHIPPED (2026-07-08):** `pux_harness/manifest.py`
  drives `pux pack` via default-deny `package.include` globs + PERMANENT `HARD_EXCLUDE`
  (`data/`/`.pux/`/`__pycache__` — the credential-leak contract). APS-shaped schema
  (`manifest_version`/`package`/`capabilities`/`dependencies`). `export.py` → `pack.py`
  (`pux export` HARD-ERRORS — Decision 5, no alias); the old `_collect_org_files` allowlist is
  a `NotImplementedError` stub. **`pux lock`** (`pux_harness/lockfile.py`) pins github MCP refs
  to commit SHAs via host-side `git ls-remote` (best-effort, never fatal offline — unresolved
  refs recorded honestly) + snapshots declared `sandbox.deps.{pip,apt}` into `org.lock.yaml`
  (committed by default — Decision 4; a `DEFAULT_INCLUDE`, so it ships in the pack). Proven:
  pack contents == manifest (contract test); old allowlist → permanent `NotImplementedError`
  failure; 50 manifest/lockfile unit tests; live `pux lock --org general` resolved
  `github/github-mcp-server@latest` to a real SHA; `pux export` hard-errors (exit 1).
  *(Note: pip resolution is as-declared for now — operators pin the critical ones; full
  `uv lock`-driven resolution is a follow-up.)*
- **P4 — Hook pipeline ✅ SHIPPED (2026-07-08):** `pux_harness/pack_hooks.py` — the
  `PACK_HOOK_REGISTRY` (`ast_check_hook` via stdlib `compile` — no ruff binary needed;
  `gitleaks_hook` via the host-side CLI, reuse-first). Wired into `pack_org` AFTER
  collection, BEFORE the tarball — `PackHookError` refuses the pack (no archive written);
  results seed `manifest.json` → `provenance.hooks` (the P5 audit surface). `ast_check`
  targets agent-authored `lib/functions/*.py` + org `sandbox/*.py` (skips the vendored
  kit). `gitleaks_hook` is a REQUIRED gate (absent binary → refuse, no silent skip).
  Prove: planted `ghp_` PAT → `pux pack` REFUSES exit 1 (github-pat + generic-api-key
  rules); broken AST → refuses (ast fires first, before gitleaks); clean `general` →
  packs + records provenance. *(Note: gitleaks detection is pattern + entropy driven —
  provider PATs/high-entropy key assignments are caught; a contrived low-entropy string
  may pass. `data/`/`.pux/` HARD_EXCLUDE is the PRIMARY secret boundary; this is
  defense-in-depth. The gitleaks scan LOGIC is unit-proven via an injected runner; the
  live refusal uses the real CLI.)*
- **P5 — OCI artifact via `oras` (shell-out) + provenance:** emit layered OCI artifact; `org.pux.*` annotations; `oras` records SHA-256 layer digests into `provenance.json` → artifact is immutable/tamper-evident now (signing slots reserved — Decision 3). Prove: `oras`/`crane` inspect/push; tamper a `lib/` blob → digest mismatch on verify.
- **P6 — (Stretch) signing + registry:** Cosign/Ed25519 signing hook via OCI manifest annotations (Decision 3); `oras push`/pull round-trip through GHCR.

## Locked decisions (2026-07-08)

1. **Manifest schema → pux-owned, APS-shaped.** Our schema; APS field names/structure so a
   future conformance flip is a config change, not a rewrite. Not full-APS-now, not fully custom.
2. **`oras` binding → shell-out to the `oras` CLI binary** (NOT `oras-python`). Pre-bundled on
   `PATH` in the pux run environment; `pux pack` fails with a clear message if absent. **Pack is a
   host-side operation** — `oras` lives where pux runs, NOT in the org sandbox container.
3. **Signing → P6 (stretch), but P5 reserves the slots.** P5: `oras` computes SHA-256 layer
   digests natively and records them in `provenance.json` → the artifact is immutable +
   tamper-evident *immediately*. P6: add the signing hook (Cosign / Ed25519) via OCI manifest
   annotations. **Why:** key-management / trust-chains are a distinct concern from archiving
   mechanics — keep them separate.
4. **`org.lock.yaml` → committed to Git by default** (treat like `package-lock.json` /
   `poetry.lock`). New **`pux lock`** command regenerates it (`uv lock` for pip + MCP git-commit
   pinning). **Why:** without it, cross-host dev resolves different transient versions — especially
   dangerous for the dynamically-pulled MCP servers. (`lib/` stays gitignored; the lockfile is
   config, tracked alongside `org.yaml`.)
5. **`pux export` → `pux pack`: CONFIRMED.** Hard error on the old verb, **NO silent alias**.
   Message: *"`pux export` has been deprecated and replaced by `pux pack` to enforce manifest
   validation, secrets scanning, and OCI packaging standards. Please use `pux pack` instead."*
   Forces updated scripts/muscle memory — prevents accidentally bypassing the new security hooks.
6. **Sequence → P1 first.** Can't design a secure validator / packaging / OCI layer-mapper for an
   asset class that doesn't exist yet. Build P1 (real agent-authored Python in `lib/`), THEN P3/P4
   have concrete failing tests: a fake key in a function → secrets hook halts; a deliberate syntax
   error → AST hook halts.

## Sources
- APS: https://agentpackaging.org/ (`apstool`, Apache-2.0, v0.1) · `.agent`: https://github.com/nomoticai/agentpk (patent-pending, ruled out)
- OCI artifacts / `oras`: https://oras.land/ · https://helm.sh/blog/storing-charts-in-oci/ · https://fluxcd.io/flux/cheatsheets/oci-artifacts/
- LangGraph manifest: https://docs.langchain.com/oss/javascript/langgraph/studio (`langgraph.json`)
