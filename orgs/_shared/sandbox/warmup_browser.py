#!/usr/bin/env python3
"""Pre-run browser warmup job.

Forces the in-container SeleniumBase Chrome to attach NOW — during the
prepare phase, before the agent loop — by hitting sb_server's ``/warmup``
endpoint (which calls ``ensure()``). This moves the SeleniumBase CDP
attach + first-page cold start OUT of the first LLM-driven browser tool
call, so it no longer risks the 60s browser timeout (``browser.py``'s
``_BROWSER_TIMEOUT``) and burns no agent turns on a cold start.

Declared as a ``jobs:`` entry by browser-using orgs (general's ``browser``
agent, dev-bot's ``web-agent``). It is NOT for orgs that don't use the
in-container browser (e.g. deep-research-engine, which uses the web_research
MCP + ddg.py).

``/status`` is a cheap alive check that NEVER initializes the browser, so we
poll it only to confirm sb_server (supervisord priority 70) is up — then
``/warmup`` does the real work.

Warn-and-continue: if sb_server never answers (an image that doesn't run
supervisord/sb_server) or warmup fails, the job exits non-zero; the runner
logs it and continues. The agent then simply pays the cold start on first
use, exactly as before — no regression, no crash.

Stdlib only (urllib) — no dependency on ``requests``.
"""
from __future__ import annotations

import os
import sys
import time
import urllib.error
import urllib.request

PORT = int(os.environ.get("SB_SERVER_PORT", "9876"))
BASE = f"http://127.0.0.1:{PORT}"
# sb_server (supervisord priority 70) may still be booting when jobs run.
READY_BUDGET = 30.0
# The cold-start attach itself; ensure() opens about:blank via sb_cdp.
WARMUP_TIMEOUT = 45.0


def _get(path: str, timeout: float) -> tuple[int, bytes]:
    with urllib.request.urlopen(BASE + path, timeout=timeout) as r:
        return r.status, r.read()


def main() -> int:
    # 1. Wait for sb_server to answer. /status returns HTTP 200 regardless of
    #    browser-alive state (it never inits), so 200 here == "server up".
    deadline = time.monotonic() + READY_BUDGET
    up = False
    while time.monotonic() < deadline:
        try:
            status, _ = _get("/status", timeout=2)
            if status == 200:
                up = True
                break
        except Exception:
            pass
        time.sleep(0.5)
    if not up:
        print(f"warmup: sb_server not reachable at {BASE} after "
              f"{READY_BUDGET:.0f}s; skipping (agent will cold-start on first call)",
              file=sys.stderr)
        return 1

    # 2. Force browser init.
    try:
        status, body = _get("/warmup", timeout=WARMUP_TIMEOUT)
    except Exception as exc:  # network/timeout — treat as warm failure, not crash
        print(f"warmup: /warmup request failed: {exc}", file=sys.stderr)
        return 1
    if status != 200:
        print(f"warmup: /warmup returned HTTP {status}: {body[:200]!r}",
              file=sys.stderr)
        return 1
    print(f"warmup: browser ready ({body[:200]})")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
