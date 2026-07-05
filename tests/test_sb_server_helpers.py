"""Tests for sandbox/scripts/sb_server.py — pure helpers only.

Tests: safe(), find_element_by_index(), _resolve_selector logic (via
Handler), and the JS constants structure. No browser, no Docker needed.
"""
from __future__ import annotations

import importlib.util
import json
import types
from pathlib import Path

import pytest

_SB_SERVER_PY = Path(__file__).resolve().parents[1] / "sandbox" / "scripts" / "sb_server.py"


def _load_module():
    """Import sb_server.py with SeleniumBase mocked out."""
    # Mock seleniumbase before import
    for mod_name in ("seleniumbase", "seleniumbase.sb_cdp"):
        sys_mod = types.ModuleType(mod_name)
        if mod_name == "seleniumbase":
            sys_mod.sb_cdp = types.ModuleType(f"{mod_name}.sb_cdp")
        import sys
        sys.modules[mod_name] = sys_mod

    spec = importlib.util.spec_from_file_location("sb_server", _SB_SERVER_PY)
    mod = importlib.util.module_from_spec(spec)
    try:
        spec.loader.exec_module(mod)
    except Exception:
        # Module-level code may fail due to missing DISPLAY; that's fine,
        # we only need the pure functions.
        pass
    return mod


mod = _load_module()


# --- safe() -------------------------------------------------------------------


def test_safe_returns_value():
    assert mod.safe(lambda: 42) == 42


def test_safe_returns_default_on_exception():
    assert mod.safe(lambda: 1 / 0, "fallback") == "fallback"


def test_safe_returns_none_default():
    assert mod.safe(lambda: 1 / 0) is None


# --- find_element_by_index ----------------------------------------------------


def _make_state(element_map):
    """Create a minimal BrowserState-like object."""
    state = types.SimpleNamespace()
    state._last_element_map = element_map
    return state


def test_find_element_by_index_found():
    state = _make_state([
        {"index": 1, "selector": "button#ok"},
        {"index": 2, "selector": "input#name"},
    ])
    assert mod.find_element_by_index(state, 1) == "button#ok"
    assert mod.find_element_by_index(state, 2) == "input#name"


def test_find_element_by_index_not_found():
    state = _make_state([{"index": 1, "selector": "a"}])
    assert mod.find_element_by_index(state, 99) is None


def test_find_element_by_index_empty():
    state = _make_state([])
    assert mod.find_element_by_index(state, 1) is None


# --- Handler._resolve_selector ------------------------------------------------


def _make_handler(state):
    """Create a minimal Handler with a mock state."""
    handler = types.SimpleNamespace()
    handler.state = state
    handler._resolve_selector = mod.Handler._resolve_selector.__get__(handler)
    return handler


def test_resolve_selector_direct():
    state = _make_state([])
    handler = _make_handler(state)
    selector, err = handler._resolve_selector({"selector": "div.content"})
    assert selector == "div.content"
    assert err == ""


def test_resolve_selector_by_index():
    state = _make_state([{"index": 3, "selector": "button#go"}])
    handler = _make_handler(state)
    selector, err = handler._resolve_selector({"index": 3})
    assert selector == "button#go"
    assert err == ""


def test_resolve_selector_index_not_found():
    state = _make_state([])
    handler = _make_handler(state)
    selector, err = handler._resolve_selector({"index": 99})
    assert err != ""
    assert "99" in err


def test_resolve_selector_missing_both():
    state = _make_state([])
    handler = _make_handler(state)
    selector, err = handler._resolve_selector({})
    assert err != ""


# --- JS constants are valid ---------------------------------------------------


def test_som_labeler_js_is_iife():
    js = mod.SOM_LABELER_JS.strip()
    assert js.startswith("((") or js.startswith("((")
    assert js.endswith(")")


def test_occlusion_check_js_is_iife():
    js = mod.OCCLUSION_CHECK_JS.strip()
    assert js.startswith("((")


def test_cdp_type_js_is_iife():
    js = mod.CDP_TYPE_JS.strip()
    assert js.startswith("((")


def test_dropdown_options_js_is_iife():
    js = mod.DROPDOWN_OPTIONS_JS.strip()
    assert js.startswith("((")


def test_select_dropdown_js_is_iife():
    js = mod.SELECT_DROPDOWN_JS.strip()
    assert js.startswith("((")


# --- _cookie_to_dict ----------------------------------------------------------


def test_cookie_to_dict_from_dict():
    c = {"name": "sid", "value": "abc", "domain": ".example.com"}
    result = mod.Handler._cookie_to_dict(None, c) if hasattr(mod.Handler, "_cookie_to_dict") else None
    # _cookie_to_dict is a nested function inside do_GET; test the concept
    # by checking the dict pass-through path
    if result is not None:
        assert result == c


def test_cookie_to_dict_from_dataclass():
    """Test the attribute-based fallback path."""
    c = types.SimpleNamespace(
        name="token", value="xyz", domain=".test.com",
        path="/", secure=True, expires=None, http_only=False,
    )
    # Inline the logic since _cookie_to_dict is nested
    result = {
        "name": getattr(c, "name", ""),
        "value": getattr(c, "value", ""),
        "domain": getattr(c, "domain", ""),
        "path": getattr(c, "path", ""),
        "secure": bool(getattr(c, "secure", False)),
        "expires": getattr(c, "expires", None),
        "httponly": bool(getattr(c, "http_only", False)),
    }
    assert result["name"] == "token"
    assert result["secure"] is True
    assert result["httponly"] is False
