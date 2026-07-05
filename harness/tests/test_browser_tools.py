"""Browser tool factories (Phase 16.1) POST to the right sb_server endpoint
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


class _Capture:
    """Stand-in for ``_sb_post``; records each (endpoint, body) call."""

    def __init__(self) -> None:
        self.calls: list[tuple[str, Any]] = []

    def __call__(self, exec_client, endpoint, body_obj, *args, **kwargs):
        self.calls.append((endpoint, body_obj))
        return tools._result({"success": True, "endpoint": endpoint})


@pytest.fixture
def cap(monkeypatch) -> _Capture:
    c = _Capture()
    monkeypatch.setattr(tools, "_sb_post", c)
    return c


def _browser_tools() -> dict:
    """All browser specialists, keyed by their bare slug."""
    specs = tools.build_native_specialists(
        exec_client="DUMMY", model=None, org=None
    )
    return {
        t.name.replace("pux_sandbox_", ""): t
        for t in specs
        if t.name.startswith("pux_sandbox_browser_")
    }


def test_all_browser_factories_registered():
    """Every Phase-16 browser slug is registered with its prefixed name."""
    specs = _browser_tools()
    expected = {
        "browser_navigate", "browser_click", "browser_type", "browser_screenshot",
        "browser_evaluate", "browser_search", "browser_scroll", "browser_go_back",
        "browser_wait", "browser_find_text", "browser_extract",
        "browser_extract_images", "browser_save_screenshot", "browser_download",
        "browser_upload", "browser_tabs", "browser_new_tab", "browser_switch_tab",
        "browser_close_tab", "browser_dropdown_options", "browser_select_dropdown",
        "browser_save_session", "browser_restore_session",
    }
    assert expected <= set(specs), f"missing: {expected - set(specs)}"
    # And every one has a non-empty description (the autopilot richness lives here).
    for slug, t in specs.items():
        assert t.description and t.description.strip(), f"{slug} has empty description"


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
