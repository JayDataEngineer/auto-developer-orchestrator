#!/usr/bin/env python3
"""Pre-run run-completion webhook/event-stream warmup job.

Verifies — during the prepare phase, before the agent loop — that the
run-completion notification endpoint (``pux serve``'s ``/events/health``) is
reachable from THIS sandbox. This is the "spin up that webhook" pre-flight:

``pux serve`` publishes every background run's terminal state to an in-process
event bus (``run_events.py``) exposed as ``GET /events`` (poll/catch-up) +
``GET /events/stream`` (SSE). A webhook-less client (Hermes — "can't make
webhooks on the sandbox") subscribes to that stream to learn run completions
WITHOUT polling ``list_runs`` and WITHOUT hosting a receiver. This job confirms
the path is alive at warmup time so a dead serve / wrong bind surfaces as a
prep warning, not a silently-missed notification.

Why this runs in-sandbox: it proves the network path THIS container has to
serve. ``pux serve`` binds the Tailscale IP (``PUX_API_HOST``), which the
sandbox reaches on its ``shared-infra`` network — ``host.docker.internal`` does
NOT connect (serve isn't on the docker-gateway iface), so the URL is built from
``PUX_API_HOST``/``PUX_API_PORT`` (passed into the container by
``SandboxContainer._build_env``), overridable via ``PUX_EVENTS_URL``.

Warn-and-continue: if serve is unreachable (localhost-only run, no serve up,
wrong host) the job exits non-zero; the runner logs it and continues. The agent
run is unaffected — completion still POSTs to any per-run ``webhook_url`` and
the operator sees the prep warning. No regression, no crash.

Stdlib only (urllib) — no dependency on ``requests``.
"""
from __future__ import annotations

import json
import os
import sys
import time
import urllib.error
import urllib.request

# Where serve lives, as seen from inside the sandbox. PUX_EVENTS_URL wins
# (full URL incl. scheme); else build from PUX_API_HOST/PUX_API_PORT.
BASE = os.environ.get("PUX_EVENTS_URL") or (
    f"http://{os.environ.get('PUX_API_HOST', '127.0.0.1')}:"
    f"{os.environ.get('PUX_API_PORT', '9988')}"
)
HEALTH = f"{BASE.rstrip('/')}/events/health"
# serve may still be booting when jobs run.
READY_BUDGET = 15.0
HEALTH_TIMEOUT = 4.0


def _probe() -> dict | None:
    try:
        with urllib.request.urlopen(HEALTH, timeout=HEALTH_TIMEOUT) as r:
            if r.status != 200:
                return None
            return json.loads(r.read() or b"{}")
    except (urllib.error.URLError, OSError, ValueError):
        return None


def main() -> int:
    # Loopback default => no remote serve was configured (standalone `pux direct`,
    # CLI runs, local dev). There's nothing to warm and nothing to warn about —
    # webhook-less notification only matters under a serve-backed deployment.
    host = os.environ.get("PUX_API_HOST", "127.0.0.1").strip()
    if not os.environ.get("PUX_EVENTS_URL") and host in {"127.0.0.1", "localhost", "0.0.0.0", ""}:
        print(
            "warmup_webhook: skipped (loopback PUX_API_HOST — no remote serve; "
            "webhook warmup is for serve-backed runs)",
            file=sys.stderr,
        )
        return 0
    deadline = time.monotonic() + READY_BUDGET
    last = None
    while time.monotonic() < deadline:
        last = _probe()
        if last and last.get("ok"):
            print(
                f"warmup_webhook: OK -> {HEALTH} "
                f"(subscribers={last.get('subscribers')}, events={last.get('events')})",
                file=sys.stderr,
            )
            return 0
        time.sleep(1.0)
    # A real serve host was configured but /events/health is down — fail loudly so
    # a misconfigured bind / dead serve surfaces as a prep warning, not a silently
    # missed notification. Still non-fatal to the run (the runner logs + continues).
    print(
        f"warmup_webhook: UNREACHABLE ({HEALTH}) after {READY_BUDGET:.0f}s — "
        f"run-completion notifications for this sandbox may not be observable by "
        f"a webhook-less client; last={last!r}. Non-fatal.",
        file=sys.stderr,
    )
    return 1


if __name__ == "__main__":
    sys.exit(main())
