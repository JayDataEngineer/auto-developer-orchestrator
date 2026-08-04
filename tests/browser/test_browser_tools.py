"""Browser tool factories POST to the right sb_server endpoint
with the right body shape.

Each ``_browser_X_tool`` factory wraps a single ``_sb_post(exec_client,
"/endpoint", {body})`` call. We monkeypatch ``_sb_post`` to capture the
(endpoint, body) handed to it, then invoke each constructed StructuredTool's
``_run`` and assert the engine-faithful contract. Arg shapes were verified
against ``sandbox/scripts/sb_server.py`` (not invented).

Docker-free: the factories only CAPTURE ``exec_client`` (they never call it at
build time), and ``_sb_post`` is stubbed so no curl runs. The dummy
``exec_client`` is never used.
"""
from __future__ import annotations

from typing import Any

import pytest

from pux_harness.sandbox import tools
# ``_sb_post`` lives in the ``browser`` SUBMODULE and ``_result`` in ``_shared``;
# both moved out of the package namespace when the monolithic tools.py was split
# into a package. The browser factories resolve ``_sb_post`` through the
# ``browser`` module's globals at call time, so that — not the package — is the
# namespace the monkeypatch must target for the stub to take effect.
from pux_harness.sandbox.tools import browser
from pux_harness.sandbox.tools._shared import _result


class _Capture:
    """Stand-in for ``_sb_post``; records each (endpoint, body) call."""

    def __init__(self) -> None:
        self.calls: list[tuple[str, Any]] = []

    def __call__(self, exec_client, endpoint, body_obj, *args, **kwargs):
        self.calls.append((endpoint, body_obj))
        return _result({"success": True, "endpoint": endpoint})


@pytest.fixture
def cap(monkeypatch) -> _Capture:
    c = _Capture()
    monkeypatch.setattr(browser, "_sb_post", c)
    return c


def _browser_tools() -> dict:
    """All browser specialists, keyed by their bare slug."""
    specs = tools.build_native_specialists(
        exec_client="DUMMY", vision_model=None, org=None
    )
    return {
        t.name.replace("pux_sandbox_", ""): t
        for t in specs
        if t.name.startswith("pux_sandbox_browser_")
    }


def test_all_browser_factories_registered():
    """Every browser slug is registered with its prefixed name."""
    specs = _browser_tools()
    expected = {
        "browser_navigate", "browser_click", "browser_type", "browser_screenshot",
        "browser_evaluate", "browser_search", "browser_scroll", "browser_go_back",
        "browser_wait", "browser_find_text", "browser_extract",
        "browser_extract_images", "browser_save_screenshot", "browser_download",
        "browser_upload", "browser_tabs", "browser_new_tab", "browser_switch_tab",
        "browser_close_tab", "browser_dropdown_options", "browser_select_dropdown",
        "browser_save_session", "browser_restore_session",
        # SOTA mouse/keyboard/DnD
        "browser_drag", "browser_hover", "browser_press", "browser_click_at",
        "browser_scroll_into_view", "browser_a11y", "browser_iframe",
        # Captcha bypass + fingerprint-legitimacy
        "browser_uc", "browser_accept_cookies", "browser_warmup_history",
        "browser_solve_captcha", "browser_reset",
    }
    assert set(specs) == expected, (
        f"missing: {expected - set(specs)}; extra: {set(specs) - expected}")
    # And every one has a non-empty description (the autopilot richness lives here).
    for slug, t in specs.items():
        assert t.description and t.description.strip(), f"{slug} has empty description"


# --- Advanced browser tools: body-shape contract ----------------------------


def test_browser_drag_index_to_index_uses_auto(cap):
    t = _browser_tools()["browser_drag"]
    t.invoke({"from_index": 2, "to_index": 5})
    assert cap.calls[0][0] == "/drag"
    body = cap.calls[0][1]
    assert body["from_index"] == 2 and body["to_index"] == 5
    assert body["strategy"] == "auto"  # default
    assert body["steps"] == 25         # default


def test_browser_drag_offset_mode_forces_physics_body(cap):
    t = _browser_tools()["browser_drag"]
    t.invoke({"from_index": 1, "dx": 120, "dy": 0, "strategy": "physics", "steps": 40})
    body = cap.calls[0][1]
    assert body == {"from_index": 1, "dx": 120, "dy": 0, "strategy": "physics", "steps": 40}


def test_browser_drag_requires_source_and_target(cap):
    t = _browser_tools()["browser_drag"]
    out = t.invoke({"to_index": 3})  # no source
    assert cap.calls == []
    assert "source" in out
    out = t.invoke({"from_index": 3})  # no target
    assert cap.calls == []
    assert "target" in out


def test_browser_hover_accepts_index(cap):
    _browser_tools()["browser_hover"].invoke({"index": 4})
    assert cap.calls[0] == ("/hover", {"index": 4})


def test_browser_hover_accepts_coords(cap):
    _browser_tools()["browser_hover"].invoke({"x": 100, "y": 200})
    assert cap.calls[0] == ("/hover", {"x": 100, "y": 200})


def test_browser_hover_requires_target(cap):
    out = _browser_tools()["browser_hover"].invoke({})
    assert cap.calls == []
    assert "index/selector OR x,y" in out


def test_browser_press_posts_keys(cap):
    _browser_tools()["browser_press"].invoke({"keys": "Control+a"})
    assert cap.calls[0] == ("/press", {"keys": "Control+a"})


def test_browser_press_with_target(cap):
    _browser_tools()["browser_press"].invoke({"keys": "ArrowDown", "index": 7})
    assert cap.calls[0] == ("/press", {"keys": "ArrowDown", "index": 7})


def test_browser_press_requires_keys(cap):
    out = _browser_tools()["browser_press"].invoke({"keys": ""})
    assert cap.calls == []
    assert "keys is required" in out


def test_browser_click_at_coords(cap):
    _browser_tools()["browser_click_at"].invoke({"x": 50, "y": 60})
    body = cap.calls[0][1]
    assert cap.calls[0][0] == "/click_at"
    assert body["x"] == 50 and body["y"] == 60
    assert body["button"] == 0 and body["double"] is False and body["right"] is False


def test_browser_click_at_right_and_double_flags(cap):
    _browser_tools()["browser_click_at"].invoke({"index": 3, "right": True})
    _browser_tools()["browser_click_at"].invoke({"selector": "#x", "double": True})
    assert cap.calls[0][1]["right"] is True and cap.calls[0][1]["index"] == 3
    assert cap.calls[1][1]["double"] is True and cap.calls[1][1]["selector"] == "#x"


def test_browser_click_at_requires_target(cap):
    out = _browser_tools()["browser_click_at"].invoke({})
    assert cap.calls == []
    assert "x,y OR index/selector" in out


# --- trusted input (isTrusted=true CDP) flag forwarding ----------------------
# trusted is OPT-IN: present in the POST body only when the model sets it. The
# default path (every org today) must stay byte-identical — no "trusted": false
# noise, no behavior change — so this guards both directions.


def test_browser_click_forwards_trusted_flag(cap):
    t = _browser_tools()["browser_click"]
    # default: trusted absent (existing orgs see no change)
    t.invoke({"selector": "#go"})
    assert "trusted" not in cap.calls[0][1]
    assert cap.calls[0] == ("/click", {"selector": "#go"})
    # opt-in: trusted forwarded
    t.invoke({"selector": "#go", "trusted": True})
    assert cap.calls[1][1]["trusted"] is True


def test_browser_type_forwards_trusted_flag(cap):
    t = _browser_tools()["browser_type"]
    t.invoke({"selector": "#q", "text": "hi"})
    assert "trusted" not in cap.calls[0][1]
    t.invoke({"selector": "#q", "text": "hi", "trusted": True})
    assert cap.calls[1][1]["trusted"] is True


def test_browser_click_at_forwards_trusted_flag(cap):
    t = _browser_tools()["browser_click_at"]
    t.invoke({"x": 5, "y": 6})
    assert "trusted" not in cap.calls[0][1]
    t.invoke({"x": 5, "y": 6, "trusted": True})
    assert cap.calls[1][1]["trusted"] is True


def test_browser_scroll_into_view_accepts_index(cap):
    _browser_tools()["browser_scroll_into_view"].invoke({"index": 9})
    assert cap.calls[0] == ("/scroll_into_view", {"index": 9})


def test_browser_scroll_into_view_requires_target(cap):
    out = _browser_tools()["browser_scroll_into_view"].invoke({})
    assert cap.calls == []
    assert "index or selector is required" in out


def test_browser_a11y_posts_empty(cap):
    _browser_tools()["browser_a11y"].invoke({})
    assert cap.calls == [("/a11y", {})]


def test_browser_iframe_default_list(cap):
    _browser_tools()["browser_iframe"].invoke({})
    assert cap.calls == [("/iframe", {"action": "list"})]


def test_browser_iframe_enter_with_selector(cap):
    _browser_tools()["browser_iframe"].invoke({"action": "enter", "selector": "iframe.pay"})
    assert cap.calls == [("/iframe", {"action": "enter", "selector": "iframe.pay"})]


def test_browser_search_posts_query(cap):
    cap_tool = _browser_tools()["browser_search"]
    cap_tool.invoke({"query": "pux orchestrator"})
    assert cap.calls == [("/search", {"query": "pux orchestrator"})]


def test_browser_search_requires_query(cap):
    cap_tool = _browser_tools()["browser_search"]
    out = cap_tool.invoke({"query": ""})
    assert cap.calls == []  # never reached _sb_post
    assert "query is required" in out


def test_browser_scroll_defaults_and_overrides(cap):
    t = _browser_tools()["browser_scroll"]
    t.invoke({})
    t.invoke({"direction": "up", "amount": 5})
    assert cap.calls == [
        ("/scroll", {"direction": "down", "amount": 0}),
        ("/scroll", {"direction": "up", "amount": 5}),
    ]


def test_browser_navigate_posts_url(cap):
    cap_tool = _browser_tools()["browser_navigate"]
    cap_tool.invoke({"url": "https://example.com"})
    assert cap.calls == [("/navigate", {"url": "https://example.com"})]


def test_browser_navigate_requires_url(cap):
    out = _browser_tools()["browser_navigate"].invoke({"url": ""})
    assert cap.calls == []
    assert "url is required" in out


@pytest.mark.parametrize("slug,endpoint", [
    ("browser_go_back", "/go_back"),
    ("browser_extract_images", "/extract_images"),
    ("browser_tabs", "/tabs"),
    ("browser_close_tab", "/close_tab"),
])
def test_browser_argless_tools_post_empty_object(cap, slug, endpoint):
    _browser_tools()[slug].invoke({})
    assert cap.calls == [(endpoint, {})]


def test_browser_wait_posts_seconds(cap):
    t = _browser_tools()["browser_wait"]
    t.invoke({})  # default
    t.invoke({"seconds": 5})
    assert cap.calls == [
        ("/wait", {"seconds": 2}),
        ("/wait", {"seconds": 5}),
    ]


def test_browser_find_text_posts_text(cap):
    _browser_tools()["browser_find_text"].invoke({"text": "Sign in"})
    assert cap.calls == [("/find_text", {"text": "Sign in"})]


def test_browser_extract_defaults_query(cap):
    t = _browser_tools()["browser_extract"]
    t.invoke({})
    assert cap.calls == [(
        "/extract",
        {"query": "extract all text content"},
    )]


def test_browser_save_screenshot_uses_screenshot_endpoint(cap):
    """browser_save_screenshot hits /screenshot (DISTINCT from browser_screenshot's
    /read) — the engine-faithful contract."""
    t = _browser_tools()["browser_save_screenshot"]
    t.invoke({"path": "/tmp/x.png"})
    t.invoke({})  # path optional
    assert cap.calls[0][0] == "/screenshot"
    assert cap.calls[0][1] == {"path": "/tmp/x.png"}
    # No path -> {} body (engine default).
    assert cap.calls[1] == ("/screenshot", {})


def test_browser_screenshot_uses_read_endpoint(cap):
    """The EXISTING browser_screenshot uses /read (the live-page read), kept
    distinct from /screenshot (the file-save). Regression guard."""
    _browser_tools()["browser_screenshot"].invoke({})
    assert cap.calls == [("/read", {})]


def test_browser_download_posts_url_and_path(cap):
    _browser_tools()["browser_download"].invoke(
        {"url": "https://x/file.pdf", "path": "/tmp/file.pdf"}
    )
    assert cap.calls == [(
        "/download",
        {"url": "https://x/file.pdf", "path": "/tmp/file.pdf"},
    )]


def test_browser_upload_posts_selector_and_filepath(cap):
    _browser_tools()["browser_upload"].invoke(
        {"selector": "#file", "file_path": "/tmp/u.csv"}
    )
    assert cap.calls == [(
        "/upload",
        {"selector": "#file", "file_path": "/tmp/u.csv"},
    )]


def test_browser_new_tab_defaults_about_blank(cap):
    t = _browser_tools()["browser_new_tab"]
    t.invoke({})
    t.invoke({"url": "https://example.com"})
    assert cap.calls == [
        ("/new_tab", {"url": "about:blank"}),
        ("/new_tab", {"url": "https://example.com"}),
    ]


def test_browser_switch_tab_default_index(cap):
    t = _browser_tools()["browser_switch_tab"]
    t.invoke({})
    t.invoke({"index": 2})
    assert cap.calls == [
        ("/switch_tab", {"index": 0}),
        ("/switch_tab", {"index": 2}),
    ]


def test_browser_dropdown_options_accepts_index(cap):
    _browser_tools()["browser_dropdown_options"].invoke({"index": 3})
    assert cap.calls[0][0] == "/dropdown_options"
    assert cap.calls[0][1] == {"index": 3}


def test_browser_dropdown_options_accepts_selector(cap):
    _browser_tools()["browser_dropdown_options"].invoke({"selector": "#sel"})
    assert cap.calls[0] == ("/dropdown_options", {"selector": "#sel"})


def test_browser_select_dropdown_by_value(cap):
    _browser_tools()["browser_select_dropdown"].invoke(
        {"index": 1, "value": "opt1"}
    )
    assert cap.calls[0][0] == "/select_dropdown"
    assert cap.calls[0][1] == {"index": 1, "value": "opt1"}


def test_browser_select_dropdown_by_text(cap):
    _browser_tools()["browser_select_dropdown"].invoke(
        {"selector": "#sel", "text": "Option A"}
    )
    assert cap.calls[0] == ("/select_dropdown", {"selector": "#sel", "text": "Option A"})


def test_browser_save_session_default_and_custom_path(cap):
    t = _browser_tools()["browser_save_session"]
    t.invoke({})
    t.invoke({"path": "/tmp/x.json"})
    assert cap.calls == [
        ("/save_session", {"path": "/tmp/browser-session.json"}),
        ("/save_session", {"path": "/tmp/x.json"}),
    ]


def test_browser_restore_session_posts_path(cap):
    _browser_tools()["browser_restore_session"].invoke({"path": "/tmp/s.json"})
    assert cap.calls == [("/restore_session", {"path": "/tmp/s.json"})]
