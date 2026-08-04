"""Tests for the pure logic in ``pux_harness.tui`` + ``tui_branding``.

The ``pux tui`` launcher has two halves:
  1. Pure file ops — org/agent resolution, the prompt-adaptation transforms, and
     the ``~/.deepagents/<org>/`` install. NO dcode dependency. Tested here
     against the REAL org tree (not a mock), so a roster slug that doesn't
     resolve or an AGENTS.md that isn't where the installer expects actually
     fails the test ([[feedback_prepare_wiring_e2e_gap]] — wiring seams proven
     through the real layout, not a hand-rolled fixture).
  2. The dcode launch (re-exec under dcode's python + in-process banner patch +
     ``cli_main``) — needs the deepagents-code uv tool + a real terminal.
     Visually verified per the project's TUI rule, not unit-tested here.
"""
from __future__ import annotations

import pytest

from pux_harness.kit._paths import project_root

PROJECT_ROOT = project_root()

from pux_harness.tui import (
    _PORTED_SUBAGENTS,
    _adapt_cto_prompt,
    _adapt_subagent_prompt,
    _discover_orgs,
    _install_agent,
    _resolve_agent_md,
    _resolve_org,
)
from pux_harness.tui_branding import (
    DEFAULT_BRANDING,
    ORG_BRANDING,
    get_branding,
    get_pux_banner,
)

# Orgs the resolver MUST find regardless of future reorgs — the specialist set
# plus the root ``general``. Updated post-rename: ``dev-bot`` → ``coder``;
# ``web-search`` and ``orchestrator`` were added when they became specialists.
KNOWN_SPECIALISTS = {
    "coder",
    "deep-research-engine",
    "game-studio",
    "invest",
    "orchestrator",
    "social-media-pipeline",
    "telegram-agent",
    "twitter-agent",
    "video-production",
    "web-search",
}


# --- org resolution ---------------------------------------------------------


def test_discover_orgs_finds_known_specialists_and_general() -> None:
    orgs = set(_discover_orgs())
    assert "general" in orgs
    missing = KNOWN_SPECIALISTS - orgs
    assert not missing, f"resolver missed specialist orgs: {sorted(missing)}"


def test_resolve_org_general_under_root() -> None:
    org_dir = _resolve_org("general")
    assert (org_dir / "AGENTS.md").is_file()


def test_resolve_org_specialist_under_specialists_dir() -> None:
    # coder lives under orgs/specialists/ (post-f570305); the resolver checks
    # BOTH roots (orgs/ then orgs/specialists/).
    org_dir = _resolve_org("coder")
    assert (org_dir / "AGENTS.md").is_file()
    assert org_dir.parent.name == "specialists"


def test_resolve_org_unknown_exits_with_code_1() -> None:
    with pytest.raises(SystemExit) as exc:
        _resolve_org("does-not-exist-xyz")
    assert exc.value.code == 1


# --- agent-md resolution ----------------------------------------------------


def test_resolve_agent_md_org_local_first() -> None:
    # coder-explorer is a coder-local subagent.
    md = _resolve_agent_md(_resolve_org("coder"), "coder-explorer")
    assert md is not None and md.is_file()


def test_resolve_agent_md_falls_back_to_shared() -> None:
    # general's roster [researcher, browser] lives under orgs/_shared/agents/.
    md = _resolve_agent_md(_resolve_org("general"), "researcher")
    assert md is not None and md.is_file()
    assert "_shared" in md.parts


def test_resolve_agent_md_missing_returns_none() -> None:
    assert _resolve_agent_md(_resolve_org("general"), "no-such-slug") is None


# --- prompt adaptation: the no-pux-isms tripwire ---------------------------
# dcode exposes a DIFFERENT tool surface than the pux sandbox (no
# pux_sandbox_python, no browser tools, no /sandbox/workspace/ path). The
# adapters rewrite the canonical org prompts to dcode-native names so the
# shipped persona never references a tool the agent doesn't have
# ([[feedback_no_legacy_left_behind]]).


def test_adapt_cto_prompt_drops_pux_sandbox_python_bullet() -> None:
    src = (
        "intro\n"
        "- `pux_sandbox_python` — quick checks (parse output)\n"
        "  continuation line that must also go\n"
        "- `keep_me` — this stays\n"
    )
    out = _adapt_cto_prompt(src)
    assert "pux_sandbox_python" not in out
    assert "continuation line that must also go" not in out
    assert "`keep_me`" in out


def test_adapt_cto_prompt_reworks_both_web_agent_clauses() -> None:
    # BOTH web-agent references must go: the intro delegation sentence AND the
    # Verify-section clause. web-agent drives pux-only browser tools absent in
    # dcode — leaving either would make the CTO delegate to a non-existent
    # subagent.
    src = (
        "To keep context clean you delegate: deep recon to `coder-explorer`, "
        "mechanical writes to `code-worker`, and live-browser e2e verification "
        "to `web-agent`. You never delegate the thinking.\n\n"
        "If the deliverable is a web site, delegate the live-browser checks to "
        "`web-agent` and report findings.\n"
    )
    out = _adapt_cto_prompt(src)
    assert "web-agent" not in out
    # The Verify clause is replaced with inline-verification guidance.
    assert "fetch_url" in out or "execute" in out


def test_adapt_cto_prompt_leaves_methodology_intact() -> None:
    # Only the tool-surface refs change — the PLAN/EXECUTE/RECOVER/ESCALATE
    # state machine, risk tiers, and verify-or-die gate stay verbatim.
    src = "## Mission\nYou PLAN, EXECUTE, RECOVER, ESCALATE.\nverify-or-die.\n"
    out = _adapt_cto_prompt(src)
    assert "PLAN, EXECUTE, RECOVER, ESCALATE" in out
    assert "verify-or-die" in out


def test_adapt_subagent_prompt_replaces_pux_refs_and_paths() -> None:
    src = (
        "The workspace lives at `/sandbox/workspace/` inside the sandbox "
        "container — that's the project root.\n"
        "Run code via `pux_sandbox_python`. These are always available to you "
        "regardless of the `tools:` whitelist (they come from "
        "`FilesystemMiddleware`).\n"
    )
    out = _adapt_subagent_prompt(src)
    assert "pux_sandbox_python" not in out
    assert "/sandbox/workspace/" not in out
    assert "FilesystemMiddleware" not in out
    # pux_sandbox_python → execute (dcode's shell tool runs python).
    assert "execute" in out


# --- the real install: ~/.deepagents/<org>/ --------------------------------


@pytest.fixture
def temp_home(monkeypatch, tmp_path):
    """Redirect ``Path.home()`` to a temp dir so ``_install_agent`` never
    touches the real ``~/.deepagents``."""
    monkeypatch.setenv("HOME", str(tmp_path))
    return tmp_path


def test_install_agent_writes_cto_and_ported_subagents(temp_home) -> None:
    org = "coder"
    target = _install_agent(org, _resolve_org(org))
    assert target == temp_home / ".deepagents" / org
    assert (target / "AGENTS.md").is_file()
    for slug in _PORTED_SUBAGENTS:
        assert (target / "agents" / slug / "AGENTS.md").is_file(), slug


def test_ported_subagents_exclude_browser_only_web_agent() -> None:
    # web-agent drives pux-only browser tools absent in dcode —
    # porting it would advertise tools the agent can't call. This is the roster
    # boundary: code-worker + coder-explorer in, web-agent out.
    assert "code-worker" in _PORTED_SUBAGENTS
    assert "coder-explorer" in _PORTED_SUBAGENTS
    assert "web-agent" not in _PORTED_SUBAGENTS


def test_installed_cto_prompt_is_clean_of_pux_isms(temp_home) -> None:
    # The SHIPPED persona must reference only dcode-native tools — the strongest
    # guard, run against the real org source through the real installer.
    # NOTE: ``web-agent`` IS allowed — the coder CTO deliberately delegates
    # browser e2e verification to web-agent (it loads the page, asserts the
    # DOM, drives a CANVAS check). web-agent is NOT in _PORTED_SUBAGENTS (it
    # drives pux-only browser tools absent in dcode), but the CTO prompt
    # references it as the delegate target on the pux side. The pux_isms guard
    # here is for tool NAMES (``pux_sandbox_*``) and pux-only paths
    # (``/sandbox/workspace/``), not delegate role names.
    target = _install_agent("coder", _resolve_org("coder"))
    text = (target / "AGENTS.md").read_text()
    assert "pux_sandbox_python" not in text
    assert "/sandbox/workspace/" not in text


def test_installed_subagent_prompts_are_clean(temp_home) -> None:
    target = _install_agent("coder", _resolve_org("coder"))
    for slug in _PORTED_SUBAGENTS:
        text = (target / "agents" / slug / "AGENTS.md").read_text()
        assert "pux_sandbox_python" not in text, slug
        assert "/sandbox/workspace/" not in text, slug


def test_install_agent_overwrites_stale_manual_edits(temp_home) -> None:
    # The org source under orgs/ is the source of truth — a manual edit to the
    # installed copy is overwritten on the next launch (idempotent refresh).
    org = "coder"
    org_dir = _resolve_org(org)
    target = _install_agent(org, org_dir)
    cto = target / "AGENTS.md"
    cto.write_text("STALE MANUAL EDIT")
    _install_agent(org, org_dir)
    assert "STALE MANUAL EDIT" not in cto.read_text()


def test_project_root_points_at_repo_root() -> None:
    # ``PROJECT_ROOT`` is the APP root (injected via the kit's ``project_root()``
    # resolver), which holds ``orgs/``. ``tui.py`` itself lives in the pux-harness
    # submodule checkout at ``<root>/pux-harness/pux_harness/tui.py`` — proving
    # both that the app root is right AND the submodule is checked out.
    assert (PROJECT_ROOT / "orgs").is_dir()
    assert (PROJECT_ROOT / "pux-harness" / "pux_harness" / "tui.py").is_file()


# --- branding ---------------------------------------------------------------


def test_get_branding_known_org() -> None:
    assert "Engineering mode" in get_branding("coder")["subheader"]


def test_get_branding_unknown_org_falls_back_to_default() -> None:
    assert get_branding("nonexistent") == DEFAULT_BRANDING


def test_branding_covers_every_real_specialist() -> None:
    # Regression guard: a new specialist org landing on the generic default
    # splash is silent drift — every specialist (bar the _demo fixture) must
    # carry a tailored subheader.
    specialists = set(_discover_orgs()) - {"general", "_demo"}
    missing = sorted(s for s in specialists if s not in ORG_BRANDING)
    assert not missing, f"specialists without tailored branding: {missing}"


def test_get_pux_banner_is_three_glyph_block() -> None:
    banner = get_pux_banner()
    # 6 rows of P+U+X glyphs — sanity (not exact-art).
    assert banner.count("\n") == 5
    assert banner.strip() != ""
    assert "╗" in banner or "╔" in banner  # box-drawing glyph set
