"""Non-mock proof that foreign MCP tool-servers fit the universal pattern.

The MCP *consumer* path (pux calling foreign MCP servers) shipped without an
E2E test: ``pux mcp`` (pux AS server) is covered by ``test_mcp_server.py``, but
``resolve_tool_servers`` → ``ToolServerSpec`` → ``build_stack(mcp_tools=)`` →
profile filtering had NO test — every ``mcp_tools`` mention in the suite was an
opaque lambda stub ignoring the arg. Per verify-or-die that path was
*asserted, not proven*.

This file proves, against the REAL resolver + REAL factory, that MCP tools flow
through the SAME universal path as specialists / middleware / skills:
**declarative config → ONE resolver → contract-enforced → ``build_stack`` →
profile-overridable.** No live MCP server is needed — resolution is pure-data
(sync, no network); the live handshake is ``mcp_client.py``'s job and is
integration-tested elsewhere. The unit-of-proof here is: a declared server
becomes a shaped ``ToolServerSpec``, its tools get injected through the one
factory seam, and the org's profile can shape them like any other tool.
"""
from __future__ import annotations

from pathlib import Path

import pytest
from langchain_core.tools import BaseTool, StructuredTool
from pydantic import BaseModel

from pux_harness.agent import stack
from pux_harness.agent.mcp_client import _namespace_tools
from pux_harness.agent.tool_servers import (
    ToolServerSpec,
    resolve_tool_servers,
    validate_tool_servers,
)


# --- shared tool builder ----------------------------------------------------


class _NoArgs(BaseModel):
    pass


def _tool(name: str) -> BaseTool:
    """A minimal StructuredTool — enough to flow through build_stack + naming."""
    return StructuredTool(
        name=name, description="mcp tool", args_schema=_NoArgs, func=lambda: "",
    )


# ===========================================================================
# Part 1 — resolve_tool_servers: declarative config → ONE resolver
# ===========================================================================

# One catalog covering every transport + an env-placeholder entry. Per-test
# policy.yaml files opt into the entries they exercise.
_CATALOG = """
# Shared catalog of foreign MCP tool servers (test fixture).
web:
  kind: mcp
  transport: http
  url: https://research.example/mcp
  tools: [search, fetch, research]
media:
  kind: mcp
  transport: sse
  url: http://media.example:8101
local:
  kind: mcp
  transport: stdio
  command: /usr/bin/mcp-foo
  args: ["--mcp"]
secret:
  kind: mcp
  transport: http
  url: https://paid.example/mcp
  headers:
    Authorization: "Bearer ${API_TOKEN}"
"""


def _write_tree(root: Path) -> Path:
    """tmp project root with the shared catalog + an ``mcp-demo`` org."""
    shared = root / "orgs" / "_shared"
    shared.mkdir(parents=True)
    (shared / "tool_servers.yaml").write_text(_CATALOG)
    org = root / "orgs" / "mcp-demo"
    org.mkdir(parents=True)
    (org / "AGENTS.md").write_text("# mcp-demo\n")
    return root


def _capabilities(root: Path, items_yaml: str) -> None:
    """Write ``mcp-demo/org.yaml`` capabilities from tool_servers-style items.

    Post CU-4: the declaration site moved from ``policy.yaml``'s removed
    ``tool_servers:`` to ``org.yaml``'s ``capabilities:`` (kind: mcp). This
    helper translates the test's tool_servers-style items into the capabilities
    block format so the REAL resolver path runs end-to-end."""
    import yaml as _yaml

    raw = _yaml.safe_load(items_yaml) or []
    caps: list[dict] = []
    for item in raw:
        if isinstance(item, str):
            caps.append({"kind": "mcp", "ref": item})
        elif isinstance(item, dict):
            if "ref" in item:
                cap = {"kind": "mcp", "ref": item["ref"]}
                if "tools" in item:
                    cap["tools"] = item["tools"]
                caps.append(cap)
            else:
                # Inline spec entries pass through as-is (resolve_tool_servers
                # handles them programmatically, though org.yaml capabilities
                # only produces ref-form items via org_mcp_items_from_dict).
                caps.append(item)
    (root / "orgs" / "mcp-demo" / "org.yaml").write_text(
        _yaml.dump({"capabilities": caps}, default_flow_style=False),
    )


# Keep the old name as an alias so existing call sites work.
_policy = _capabilities


@pytest.fixture
def mcp_tree(tmp_path: Path, monkeypatch):
    """tmp project root with the catalog; the resolver's ``_orgs_dir`` is pointed
    at it so ``resolve_tool_servers`` reads the tmp catalog + tmp org.yaml (no
    monkeypatching of internals — the REAL resolver runs over the tmp tree).

    Must patch ``orgs._orgs_dir`` (not just ``tool_servers._orgs_dir``) because
    ``_org_path`` is imported from ``orgs`` and calls ``orgs._orgs_dir()``
    internally — patching only the ``tool_servers`` reference leaves the
    ``_org_path`` call pointing at the real orgs/ tree."""
    from pux_harness.agent import orgs, tool_servers
    root = _write_tree(tmp_path)
    monkeypatch.setattr(orgs, "_orgs_dir", lambda: root / "orgs")
    monkeypatch.setattr(tool_servers, "_orgs_dir", lambda: root / "orgs")
    return root


@pytest.fixture(autouse=True)
def _reset_catalog_cache():
    """``load_catalog()`` caches its read in a module-level ``_catalog_cache``
    (a production optimization — avoid re-reading the catalog per org per
    build). That cache leaks across tests: a test reading the REAL tree would
    poison every later test's tmp-tree catalog (and vice-versa) because
    ``mcp_tree`` only repoints ``_orgs_dir``, not the cache. Reset it before
    each test so every test reads its own catalog fresh — real or tmp per its
    ``_orgs_dir``."""
    from pux_harness.agent import tool_servers
    tool_servers._catalog_cache = None
    yield
    tool_servers._catalog_cache = None


def test_resolve_handles_both_declaration_forms(mcp_tree: Path):
    """The two supported org.yaml capabilities forms both resolve: bare ref
    (catalog ref → full allowlist) and ``{ref:, tools:}`` (catalog ref +
    allowlist override). One resolver, one shape out.

    Inline specs (``{name, kind, transport, ...}``) are handled by
    ``resolve_tool_servers`` programmatically but are NOT reachable through
    ``org.yaml`` capabilities (``org_mcp_items_from_dict`` only produces ref-form
    items). Post CU-4: the declaration site is org.yaml capabilities, and the
    inline-spec path exists for programmatic callers only."""
    _capabilities(mcp_tree, """
  - web                              # bare ref -> full allowlist [search,fetch,research]
  - ref: media                       # ref + override -> NARROW to [transcribe]
    tools: [transcribe]
""")
    specs = resolve_tool_servers("mcp-demo")
    by_name = {s.name: s for s in specs}
    assert set(by_name) == {"web", "media"}
    # bare ref inherits the catalog allowlist verbatim
    assert by_name["web"].transport == "http"
    assert by_name["web"].tools == ["search", "fetch", "research"]
    # ref-with-override NARROWS the allowlist (the override wins, not the catalog's)
    assert by_name["media"].transport == "sse"
    assert by_name["media"].tools == ["transcribe"]


def test_resolve_expands_env_placeholders(mcp_tree: Path):
    """``${VAR}`` in url/headers expands from the ``env`` arg; a missing var
    causes the server to be SKIPPED (per-server isolation — one unset var
    can't brick the whole org). No silent empty-string substitution."""
    _capabilities(mcp_tree, "  - secret\n")
    # with the var supplied -> expanded into the header
    specs = resolve_tool_servers("mcp-demo", env={"API_TOKEN": "tok123"})
    assert len(specs) == 1
    assert specs[0].headers["Authorization"] == "Bearer tok123"
    # without it -> per-server isolation: spec is SKIPPED (logged ERROR),
    # not raised. The org gets an empty list, not a crash.
    specs = resolve_tool_servers("mcp-demo", env={})
    assert specs == [], (
        "expected empty (server skipped), not silent empty-string substitution"
    )


def test_resolve_dangling_catalog_ref_raises(mcp_tree: Path):
    """An unknown catalog ref is a hard error (no silent skip — the declared
    capability must exist)."""
    _capabilities(mcp_tree, "  - no-such-server\n")
    with pytest.raises(ValueError, match="unknown catalog ref 'no-such-server'"):
        resolve_tool_servers("mcp-demo")


def test_resolve_no_declaration_returns_empty(mcp_tree: Path):
    """An org with no ``tool_servers:`` block resolves to ``[]`` (not an error —
    the default, MCP-free, state)."""
    # No policy.yaml written -> policy.load raises NoPolicy -> resolver returns [].
    specs = resolve_tool_servers("mcp-demo")
    assert specs == []


# ===========================================================================
# Part 2 — build_stack: MCP tools flow through the ONE factory, profile-shaped
# ===========================================================================

@pytest.fixture
def stubbed_factory(monkeypatch):
    """Stub build_stack's heavy harness deps (model + middleware classes + the
    context layer) so the factory runs without Docker / real middleware. The
    ORG config + the mcp_tools flow stay real — that's the surface under test."""
    monkeypatch.setattr(stack, "build_context_layer", lambda **kw: ([], []))
    monkeypatch.setattr(stack, "RoutingMiddleware", lambda: "ROUTE")
    monkeypatch.setattr(stack, "SessionGuideMiddleware", lambda: "GUIDE")
    monkeypatch.setattr(stack, "AuditMiddleware", lambda **kw: "AUDIT")
    monkeypatch.setattr(stack, "RubricMiddleware", lambda **kw: "RUBRIC")
    monkeypatch.setattr(stack, "get_model", lambda *a, **k: "MODEL")
    monkeypatch.setattr(stack, "build_grader_tools", lambda *a, **k: ["g1"])
    from pux_harness.agent import orgs
    monkeypatch.setattr(orgs, "get_model", lambda *a, **k: "WORKER_MODEL")
    monkeypatch.setenv("PUX_BROWSER_VISION", "0")


def test_mcp_tools_land_on_supervisor_surface(stubbed_factory):
    """MCP tools pass through ``build_stack(mcp_tools=)`` onto the supervisor
    tool surface — the SAME seam specialists use. No special MCP code path in
    the factory; mcp_tools is just another tool list. Real specialists are
    passed so the org's real roster resolves (mirrors test_real_orgs_build)."""
    from pux_harness.sandbox.tools import build_native_specialists
    mcp_tools = [_tool("mcp__web__search"), _tool("mcp__web__fetch")]
    plan = stack.build_stack(
        "general", specialists=build_native_specialists(exec_client="STUB"),
        profile=None, rubric_gate=None, exec_client="STUB", mcp_tools=mcp_tools,
    )
    names = [t.name for t in plan.supervisor_tools]
    assert "mcp__web__search" in names and "mcp__web__fetch" in names


def test_mcp_tools_are_profile_filterable(stubbed_factory):
    """The universal-pattern headline: an org's ``profile.excluded_tools`` drops
    an MCP tool the SAME way it drops a specialist. MCP is not a second-class
    citizen that bypasses overrides — it flows through ``apply_profile_to_tools``
    like every other tool."""
    from deepagents import HarnessProfileConfig
    from pux_harness.sandbox.tools import build_native_specialists
    mcp_tools = [_tool("mcp__web__search"), _tool("mcp__web__fetch")]
    profile = HarnessProfileConfig(excluded_tools=["mcp__web__search"])
    plan = stack.build_stack(
        "general", specialists=build_native_specialists(exec_client="STUB"),
        profile=profile, rubric_gate=None, exec_client="STUB", mcp_tools=mcp_tools,
    )
    names = {t.name for t in plan.supervisor_tools}
    assert "mcp__web__search" not in names   # excluded by profile override
    assert "mcp__web__fetch" in names        # sibling survives


def test_namespace_prefixes_mcp_dunder():
    """``_namespace_tools`` stamps the ``mcp__<server>__<tool>`` prefix the
    session manager uses — the convention that keeps foreign tool names from
    colliding with pux specialists (and makes ``excluded_tools`` targeting
    unambiguous, as the test above relies on)."""
    out = _namespace_tools([_tool("search"), _tool("fetch")], "web")
    assert [t.name for t in out] == ["mcp__web__search", "mcp__web__fetch"]


# ===========================================================================
# Part 3 — contract: a broken declaration fails --check-contract
# ===========================================================================

def test_contract_flags_dangling_ref(mcp_tree: Path):
    """``validate_tool_servers`` (the offline contract surface called from
    ``check_org``) turns a dangling catalog ref into an error string — so a
    broken declaration fails ``pux check-contract`` before it ever reaches a
    build. Declared capabilities must resolve or fail loud."""
    _capabilities(mcp_tree, "  - ghost\n")
    errors = validate_tool_servers("mcp-demo")
    assert len(errors) == 1
    assert "ghost" in errors[0]


def test_contract_clean_for_valid_declaration(mcp_tree: Path):
    """A valid declaration (bare ref to a catalog entry, no placeholders) yields
    zero contract errors — the green baseline."""
    _capabilities(mcp_tree, "  - web\n")
    assert validate_tool_servers("mcp-demo") == []


# ===========================================================================
# Part 4 — McpSessionManager.open(): the live handshake (dict-vs-list crash)
# ===========================================================================

def test_open_buckets_per_server_via_flat_get_tools(monkeypatch):
    """``open()`` must call ``get_tools(server_name=...)`` once per spec. The
    REAL ``MultiServerMCPClient.get_tools`` returns a FLAT list across all
    servers, NOT a ``dict[str, list]``. The first live handshake (2026-07-06)
    crashed ``AttributeError: 'list' object has no attribute 'get'`` because
    ``open()`` indexed the flat list as a dict. The isolation tests above pass
    ``_apply_allowlist`` a list directly and never drive ``open()``, so the bug
    slipped through ([[verify-or-die]]: a wiring seam proven only by isolation
    tests is unproven). This patches ``get_tools`` to the real flat-list shape
    and proves ``open()`` attributes tools per-server + namespaces + honors the
    allowlist — with NO network."""
    import asyncio
    from langchain_mcp_adapters.client import MultiServerMCPClient

    from pux_harness.agent.mcp_client import McpSessionManager

    async def fake_get_tools(self, *, server_name=None):
        # REAL shape: a FLAT list (no server bucketing). Distinct tools per
        # server proves open() attributes by server_name, not positionally.
        if server_name == "web":
            return [_tool("search"), _tool("scrape"), _tool("research")]
        if server_name == "media":
            return [_tool("transcribe"), _tool("classify")]
        return []

    monkeypatch.setattr(MultiServerMCPClient, "get_tools", fake_get_tools)
    specs = [
        ToolServerSpec(name="web", transport="http", url="https://x/mcp",
                       tools=["search", "research"]),  # allowlist narrows
        ToolServerSpec(name="media", transport="sse", url="http://y:8101",
                       tools=None),                    # take everything
    ]

    async def _run():
        mgr = McpSessionManager("o", specs)
        await mgr.open()
        return sorted(t.name for t in mgr.tools)

    # web: allowlist keeps search+research, drops scrape; media: takes both.
    assert asyncio.run(_run()) == [
        "mcp__media__classify",
        "mcp__media__transcribe",
        "mcp__web__research",
        "mcp__web__search",
    ]


def test_open_one_bad_server_does_not_brick_the_batch(monkeypatch):
    """A server whose ``tools/list`` raises contributes ZERO tools + a loud log
    but does NOT crash ``open()`` — the sibling server still loads. This is the
    per-server isolation a bare ``get_tools()`` (which gathers all servers and
    raises on the FIRST failure) could NOT provide."""
    import asyncio
    from langchain_mcp_adapters.client import MultiServerMCPClient

    from pux_harness.agent.mcp_client import McpSessionManager

    async def fake_get_tools(self, *, server_name=None):
        if server_name == "dead":
            raise ConnectionError("server unreachable")
        if server_name == "live":
            return [_tool("search")]
        return []

    monkeypatch.setattr(MultiServerMCPClient, "get_tools", fake_get_tools)
    specs = [
        ToolServerSpec(name="dead", transport="http", url="https://dead/mcp"),
        ToolServerSpec(name="live", transport="http", url="https://live/mcp"),
    ]

    async def _run():
        mgr = McpSessionManager("o", specs)
        await mgr.open()
        return [t.name for t in mgr.tools]

    assert asyncio.run(_run()) == ["mcp__live__search"]


# ===========================================================================
# Part 5 — the shipped wiring: general opts into web_research (REAL tree)
# ===========================================================================

def test_general_ships_web_research(monkeypatch):
    """The MCP consumer gap is CLOSED: a shipped org actually declares foreign
    MCP servers. ``general`` (the general-purpose CTO fallback) opts into BOTH
    ``web_research`` (http) and ``github`` (stdio) via ``orgs/general/policy.yaml``.
    This runs against the REAL orchestrator ``orgs/`` tree (no tmp fixture) so it
    proves the production wiring end-to-end at the resolver — if either
    declaration is removed or a catalog ref drifts, this fails. The live handshake
    + the agent invoking the tool are proven separately (wild run 2026-07-06 + the
    github release-bootstrap live proof); this is the offline lock that the wiring
    ships and resolves.

    web_research's URL is env-injected (``${PUX_MCP_WEB_RESEARCH_URL}``); github's
    PAT is mapped from ``${GITHUB_TOKEN}``. Both are env-injected (not git-tracked)
    — the strict runtime path needs both vars set, so this test sets them. The
    offline contract passes WITHOUT them (permissive) — see
    ``test_placeholder_url_passes_contract_but_fails_strict_without_env``."""
    monkeypatch.setenv("PUX_MCP_WEB_RESEARCH_URL", "https://injected.example/mcp")
    # Resolver-only placeholder: this is a pure-data offline resolution test (no
    # network, no server fork), so a fake token satisfies _substitute_spec's
    # strict path. github's LIVE handshake is proven in the bootstrap live proof.
    monkeypatch.setenv("GITHUB_TOKEN", "ghp_resolver-only-not-a-live-key")
    specs = {s.name: s for s in resolve_tool_servers("general")}
    # web_research — the original MCP consumer wiring (http, env-injected URL).
    web = specs["web_research"]
    assert web.transport == "http"
    # The URL is the env-injected value, NOT a git-tracked literal.
    assert web.url == "https://injected.example/mcp"
    # The catalog allowlist (verified against the live server's tools/list).
    assert web.tools == ["search", "fetch", "research"]
    # github — the stdio release-bootstrap server (PAT mapped, binary fetched
    # on-demand). Proves the github: block survives resolution intact for the
    # bootstrap seam (mcp_bootstrap.ensure_server) to consume at open() time.
    gh = specs["github"]
    assert gh.transport == "stdio"
    assert gh.command == "github-mcp-server"
    assert gh.env["GITHUB_PERSONAL_ACCESS_TOKEN"] == "ghp_resolver-only-not-a-live-key"
    assert gh.github["repo"] == "github/github-mcp-server"
    assert gh.github["binary"] == "github-mcp-server"
    # Both declarations are contract-clean (valid catalog refs, no dangling names).
    assert validate_tool_servers("general") == []


def test_placeholder_url_passes_contract_but_fails_strict_without_env(monkeypatch):
    """The contract/runtime split for git-safe URLs. A catalog entry may ship
    ``url: ${VAR}`` whose value is deployment-specific (and therefore must NOT
    be git-tracked). The offline contract validates STRUCTURE — the field is
    declared, the transport is known — so it PASSES despite the unresolved
    placeholder. The runtime path (strict) SKIPS the server (per-server
    isolation: one unset var can't brick the whole org) and logs an ERROR;
    the org still starts, just without that server's tools."""
    monkeypatch.delenv("PUX_MCP_WEB_RESEARCH_URL", raising=False)
    monkeypatch.delenv("GITHUB_TOKEN", raising=False)
    # Offline contract: permissive -> zero errors despite the ${VAR} url.
    assert validate_tool_servers("general") == []
    # Runtime: strict -> per-server isolation kicks in. The unresolved
    # placeholders cause web_research + github to be SKIPPED (logged ERROR),
    # not raised. The org gets an empty tool list, not a crash.
    specs = resolve_tool_servers("general")
    assert specs == [], (
        f"expected empty specs (all servers skipped), got {len(specs)} specs"
    )


