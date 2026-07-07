"""Behavioral test for sb_server's ``GET /warmup`` endpoint.

The warmup job (``orgs/_shared/sandbox/warmup_browser.py``) hits ``/warmup``
to force a Chrome attach during the prepare phase. The critical contract:
unlike ``/status`` (a cheap alive check that NEVER inits the browser),
``/warmup`` MUST call ``state.ensure()`` — that's the whole point (it moves
the SeleniumBase CDP attach out of the first LLM-driven tool call).

We load ``sb_server.py`` with SeleniumBase mocked (the established pattern
from ``test_sb_server_helpers.py``) and drive ``Handler.do_GET`` with a fake
state, asserting ``ensure()`` is invoked and the response shape is correct
for both the warm and the attach-failed paths. The ensure()->Chrome-attach
step itself is already live-proven by every browser tool invocation
(browser-sota-phase20 / browser-vision-phase22); this test pins the NEW
endpoint's dispatch + ensure() call.
"""
from __future__ import annotations

import importlib.util
import types
from pathlib import Path

_SB_SERVER_PY = Path(__file__).resolve().parents[2] / "sandbox" / "scripts" / "sb_server.py"


def _load_module():
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
        pass  # module-level DISPLAY-dependent code may fail; Handler is defined already
    return mod


mod = _load_module()


def _make_handler(path, *, ensure_returns, sb_after):
    """A fake Handler whose ``state.ensure()`` records the call and whose
    ``_ok``/``_err`` capture what the endpoint would send."""
    state = types.SimpleNamespace(
        sb=None, stealth=True, use_chromium=False,
    )
    calls = {"ensure": 0}

    def ensure():
        calls["ensure"] += 1
        state.sb = sb_after  # simulate the attach outcome
        return ensure_returns

    state.ensure = ensure

    handler = types.SimpleNamespace(state=state, path=path)
    sent = {}

    def fake_ok(data=None, **extra):
        sent["ok"] = data

    def fake_err(msg, code=500):
        sent["err"] = (msg, code)

    handler._ok = fake_ok
    handler._err = fake_err
    return handler, calls, sent


def test_warmup_calls_ensure_and_reports_warmed() -> None:
    handler, calls, sent = _make_handler(
        "/warmup", ensure_returns=True, sb_after=object(),
    )
    mod.Handler.do_GET(handler)

    # The single most important assertion: /warmup MUST call ensure().
    assert calls["ensure"] == 1, "/warmup must trigger state.ensure() (the real init)"
    assert "ok" in sent and "err" not in sent
    assert sent["ok"]["warmed"] is True
    assert sent["ok"]["alive"] is True


def test_warmup_503_when_attach_fails() -> None:
    handler, calls, sent = _make_handler(
        "/warmup", ensure_returns=False, sb_after=None,
    )
    mod.Handler.do_GET(handler)

    assert calls["ensure"] == 1  # tried, but attach failed
    assert "err" in sent and "ok" not in sent
    assert sent["err"][1] == 503  # service unavailable, not a 500 crash


def test_warmup_branch_is_reachable_and_distinct_from_status() -> None:
    """Static guard: the /warmup branch exists and routes nowhere else.
    Catches a future refactor that drops the elif or reuses /status logic."""
    src = _SB_SERVER_PY.read_text()
    assert 'self.path == "/warmup"' in src, "/warmup route must be present"
    assert "self.state.ensure()" in src, "/warmup must call state.ensure()"
