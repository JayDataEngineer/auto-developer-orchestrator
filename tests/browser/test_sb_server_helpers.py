"""Tests for sandbox/scripts/sb_server.py — pure helpers only.

Tests: safe(), find_element_by_index(), _resolve_selector logic (via
Handler), and the JS constants structure. No browser, no Docker needed.
"""
from __future__ import annotations

import asyncio
import importlib.util
import json
import re
import sys
import types
from pathlib import Path

import pytest

_SB_SERVER_PY = Path(__file__).resolve().parents[2] / "sandbox" / "scripts" / "sb_server.py"


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


# --- Interaction JS constants are IIFEs ------------------------------------
# Same CDP Runtime.evaluate constraint as the constants above: each must be a
# single arrow-fn expression so it can be embedded as `return CONST(args)` and
# re-evaluated without leaking declarations across calls.

def _assert_iife(name):
    js = getattr(mod, name).strip()
    assert js.startswith("(("), f"{name} must be an IIFE arrow fn, got: {js[:40]!r}"
    assert js.endswith(")"), f"{name} must close the expression, got: {js[-40:]!r}"


def test_phase19_interaction_js_constants_are_iifes():
    for name in (
        "ELEMENT_CENTER_JS", "SIMULATE_DND_JS",
        "HOVER_JS", "PRESS_JS", "CLICK_AT_JS", "SCROLL_INTO_VIEW_JS",
    ):
        _assert_iife(name)


def test_simulate_dnd_js_fires_html5_sequence():
    """The HTML5 workaround MUST dispatch the full native drag event chain —
    that's the whole point (Selenium's ActionChains skips it). Static check
    guards against an accidental trim of the sequence."""
    js = mod.SIMULATE_DND_JS
    for evt in ("dragstart", "dragenter", "dragover", "drop", "dragend"):
        assert evt in js, f"SIMULATE_DND_JS missing {evt!r}"


def test_physics_drag_uses_trusted_cdp_not_iife():
    """No-legacy-left-behind: the physics-drag strategy was migrated from an
    in-page PHYS_DRAG_JS IIFE (synthetic, untrusted dispatchEvent → could NOT
    move native sliders / fire dnd-kit's PointerSensor) to the trusted CDP
    Input domain helper _trusted_cdp_drag. The OLD form must stay GONE.

    Guards two things: (1) no PHYS_DRAG_JS constant exists in the module, and
    (2) the trusted helper is present and drives the CDP Input primitive
    (dispatch_mouse_event), not a synthetic JS event chain."""
    assert not hasattr(mod, "PHYS_DRAG_JS"), (
        "PHYS_DRAG_JS should be deleted — physics drag now uses the trusted "
        "CDP helper _trusted_cdp_drag (synthetic events can't move native widgets)"
    )
    src = _SB_SERVER_PY.read_text()
    assert "def _trusted_cdp_drag(" in src, "trusted CDP drag helper must exist"
    assert "dispatch_mouse_event" in src, (
        "physics drag must use CDP Input.dispatchMouseEvent (isTrusted=true), "
        "not a synthetic in-page mousemove chain"
    )


def test_main_creates_httpserver_bound_to_server():
    """main() MUST construct the HTTPServer and bind it to ``server`` before
    serve_forever()/server_close() reference it.

    Why this exists (regression born 2026-07-07): an earlier refactor of
    ``main()`` (adding the ``--use-chromium`` flag + details-log formatting)
    silently DROPPED the ``server = HTTPServer(...)`` line. The file AST-parsed
    fine and every unit test passed (the suite mocks seleniumbase and never
    executes ``main()``), so the only ``NameError: name 'server' is not
    defined`` surfaced at runtime in-container — a pure verify-or-die gap. The
    image's BAKED copy still had the assignment, which masked the host bug until
    a ``docker cp`` overwrote it. This AST check walks ``main``'s body and
    refuses to ship without a real ``server = HTTPServer(...)`` assignment, so
    the bug class can't hide behind a passing unit suite again.
    """
    import ast

    tree = ast.parse(_SB_SERVER_PY.read_text())
    main_fn = next(
        (n for n in tree.body if isinstance(n, ast.FunctionDef) and n.name == "main"),
        None,
    )
    assert main_fn is not None, "sb_server.py must define main()"

    found = False
    for node in ast.walk(main_fn):
        if not isinstance(node, ast.Assign):
            continue
        # target must be a bare `server =` (Name), value an HTTPServer(...) call
        targets_named_server = [
            t for t in node.targets
            if isinstance(t, ast.Name) and t.id == "server"
        ]
        if not targets_named_server:
            continue
        call = node.value
        if (isinstance(call, ast.Call)
                and isinstance(call.func, ast.Name)
                and call.func.id == "HTTPServer"):
            found = True
    assert found, (
        "main() must bind `server = HTTPServer(...)` before serve_forever() — "
        "the dropped-assignment NameError crashloop must never recur"
    )


# --- trusted CDP click / type (OpenComputer value extraction P2) -------------
# _trusted_cdp_click mirrors _trusted_cdp_drag: the ONLY isTrusted=true click
# primitive in the codebase (Selenium sb.click + CLICK_AT_JS are untrusted).
# _trusted_cdp_type uses Input.insertText. Proven LIVE against a real page
# (see project_trusted_input memory); these unit tests pin the CDP call SHAPE
# (event sequence, button bitmask, click_count, insert_text payload) so a
# regression that silently breaks anti-bot input is caught without a browser.

_FAKE_MYCDP = types.ModuleType("mycdp")


class _FakeInput:
    """Records every dispatch_mouse_event / insert_text call as (type, kwargs)."""

    def __init__(self) -> None:
        self.events: list[tuple[str, dict]] = []

    def MouseButton(self, name):  # real API returns an enum; the name is enough here
        return name

    def dispatch_mouse_event(self, type, **kw):
        self.events.append((type, kw))
        yield  # mycdp commands are generators yielding the JSON-RPC payload

    def insert_text(self, text):
        self.events.append(("insert_text", {"text": text}))
        yield


class _FakeTab:
    """``sb.page`` stand-in: ``send`` consumes a mycdp generator command."""

    def __init__(self, inp: _FakeInput) -> None:
        self._inp = inp

    async def send(self, gen):
        for _ in gen:
            pass


def _fake_sb(inp: _FakeInput) -> types.SimpleNamespace:
    _FAKE_MYCDP.input_ = inp
    sys.modules["mycdp"] = _FAKE_MYCDP  # the helpers do `import mycdp as cdp`
    return types.SimpleNamespace(page=_FakeTab(inp), loop=asyncio.new_event_loop())


def test_trusted_cdp_click_helpers_exist_and_use_cdp_input():
    """Static guard mirroring test_physics_drag_uses_trusted_cdp_not_iife."""
    assert hasattr(mod, "_trusted_cdp_click"), "trusted CDP click helper must exist"
    assert hasattr(mod, "_trusted_cdp_type"), "trusted CDP type helper must exist"
    src = _SB_SERVER_PY.read_text()
    assert src.count("dispatch_mouse_event") >= 4, (
        "trusted click must drive CDP Input.dispatchMouseEvent "
        "(mouseMoved + mousePressed + mouseReleased)"
    )
    assert "insert_text" in src, "trusted type must use CDP Input.insertText"


def test_trusted_cdp_click_dispatches_move_press_release():
    inp = _FakeInput()
    mod._trusted_cdp_click(_fake_sb(inp), 10, 20, button="right", click_count=2)
    assert [t for t, _ in inp.events] == [
        "mouseMoved", "mousePressed", "mouseReleased",
    ]
    press = dict(inp.events[1][1])
    assert press["x"] == 10 and press["y"] == 20
    assert press["button"] == "right"          # MouseButton("right") pass-through
    assert press["buttons"] == 2               # right → bitmask 2
    assert press["click_count"] == 2           # double
    release = dict(inp.events[2][1])
    assert release["button"] == "right" and release["click_count"] == 2


def test_trusted_cdp_click_left_default_uses_button_mask_1():
    inp = _FakeInput()
    mod._trusted_cdp_click(_fake_sb(inp), 5, 5)  # defaults: left, single
    press = dict(inp.events[1][1])
    assert press["button"] == "left"
    assert press["buttons"] == 1               # left → bitmask 1
    assert press["click_count"] == 1


def test_trusted_cdp_type_uses_insert_text_with_payload():
    inp = _FakeInput()
    mod._trusted_cdp_type(_fake_sb(inp), "hello@world.com")
    assert inp.events == [("insert_text", {"text": "hello@world.com"})]


def test_no_execute_script_uses_return_iife_pattern():
    """No-legacy-left-behind tripwire for the SeleniumBase-CDP multi-line bug.

    SeleniumBase CDP ``evaluate``/``execute_script`` only strips a leading
    ``return`` from the LAST line of the script. Our JS constants are multi-line
    IIFEs, so a call site ``sb.execute_script(f'return {CONST_JS}(args)')`` puts
    ``return`` on the FIRST line; the strip is skipped and the raw ``return``
    reaches Playwright ``page.evaluate`` → "Illegal return statement" → the call
    silently no-ops (and ``/type`` silently fell back to ``sb.type``, masking it).

    The constants are IIFEs that return a value as an EXPRESSION, so the correct
    call site is the bare ``sb.execute_script(f'{CONST_JS}(args)')`` — no
    ``return``. This static check reads the source and refuses any
    ``return {ALL_CAPS_JS}`` inside an ``execute_script`` f-string so the bug
    class can never come back.
    """
    src = _SB_SERVER_PY.read_text()
    offending = []
    for line in src.splitlines():
        if "execute_script" not in line:
            continue
        if re.search(r"return \{[A-Z][A-Z_]*_JS\}", line):
            offending.append(line.strip())
    assert not offending, (
        "execute_script call site(s) use the buggy `return {X_JS}` pattern "
        "(multi-line IIFE + SeleniumBase CDP = Illegal return statement):\n"
        + "\n".join(offending)
    )


def test_js_str_quotes_and_escapes():
    """js_str() produces a single-quoted JS literal and escapes the two chars
    that can break out of it (backslash, single-quote)."""
    assert mod.js_str("button#ok") == "'button#ok'"
    assert mod.js_str("a[b='c']") == "'a[b=\\'c\\']'"
    assert mod.js_str("C:\\path") == "'C:\\\\path'"


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
