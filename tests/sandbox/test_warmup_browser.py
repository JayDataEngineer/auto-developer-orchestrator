"""Unit tests for the pre-run browser warmup job script.

Proves the DECISION LOGIC of ``warmup_browser.main()``: poll ``/status``
until sb_server answers, then hit ``/warmup``; correct exit codes for the
up-and-warm path, sb-server-never-up, warmup-non-200, and warmup-request-error.

The script lives under ``orgs/_shared/sandbox/`` (org source, not an
importable package), so it is loaded by file path and its ``_get`` seam is
monkeypatched — no real HTTP. The live proof that ``/warmup`` actually
forces a Chrome attach is a separate E2E step.
"""
from __future__ import annotations

import importlib.util
from pathlib import Path

_SCRIPT = (Path(__file__).resolve().parents[2]
           / "orgs" / "_shared" / "sandbox" / "warmup_browser.py")


def _load_module():
    spec = importlib.util.spec_from_file_location("warmup_browser", _SCRIPT)
    assert spec and spec.loader
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def test_warmup_succeeds(monkeypatch) -> None:
    mod = _load_module()
    calls: list[str] = []

    def fake_get(path, timeout):  # noqa: ANN001 - matches _get signature
        calls.append(path)
        if path == "/status":
            return 200, b'{"alive": false}'  # server up, browser not yet attached
        return 200, b'{"warmed": true, "alive": true}'

    monkeypatch.setattr(mod, "_get", fake_get)
    assert mod.main() == 0
    # Polled status (server-up) THEN warmup (the real init).
    assert calls[0] == "/status"
    assert calls[-1] == "/warmup"


def test_sb_server_never_up_returns_nonzero(monkeypatch) -> None:
    mod = _load_module()
    monkeypatch.setattr(mod, "READY_BUDGET", 0.5)  # tight poll budget -> fast test

    def fake_get(path, timeout):  # noqa: ANN001
        raise OSError("connection refused")

    monkeypatch.setattr(mod, "_get", fake_get)
    assert mod.main() == 1  # warn-and-continue: never crashes, signals failure


def test_warmup_non_200_returns_nonzero(monkeypatch) -> None:
    mod = _load_module()

    def fake_get(path, timeout):  # noqa: ANN001
        if path == "/status":
            return 200, b"{}"
        return 503, b'{"error": "browser not available"}'  # ensure() failed

    monkeypatch.setattr(mod, "_get", fake_get)
    assert mod.main() == 1


def test_warmup_request_error_returns_nonzero(monkeypatch) -> None:
    mod = _load_module()

    def fake_get(path, timeout):  # noqa: ANN001
        if path == "/status":
            return 200, b"{}"
        raise TimeoutError("warmup timed out")  # network blip during warm

    monkeypatch.setattr(mod, "_get", fake_get)
    assert mod.main() == 1
