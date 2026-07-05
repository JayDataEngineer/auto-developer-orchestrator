"""``pux`` CLI — a thin client of the Agent Protocol server (``pux serve``).

Replaces the deleted TS harness (``bin/pux.mjs``). The server is the canonical
executor; this is a client. If the server isn't running, commands fail loudly
with a hint to start it — no silent in-process fallback (the Agent Protocol
architecture: server serves, client drives).

  pux agents                       list orgs (agents)
  pux dispatch --org X "task"      ephemeral blocking run; prints output + thread_id
  pux resume [--org X]             list recent threads
  pux show <thread_id>             thread state (last message)
  pux history <thread_id>          revision history
  pux run <thread_id> "task"       background run on an existing thread -> run_id
  pux wait <run_id>                block for a background run's output

The in-process runner (``python -m pux_harness.main --org X``) stays available
for dev/no-server use.
"""
from __future__ import annotations

import argparse
import json
import os
import sys
from typing import Any

import httpx

PUX_API_URL = os.environ.get("PUX_API_URL", "http://127.0.0.1:9988").rstrip("/")
TIMEOUT = httpx.Timeout(connect=10.0, read=600.0, write=30.0, pool=30.0)


def _post(path: str, **json_body: Any) -> Any:
    try:
        r = httpx.post(f"{PUX_API_URL}{path}", json=json_body, timeout=TIMEOUT)
    except httpx.ConnectError as exc:
        _die(f"can't reach the Agent Protocol server at {PUX_API_URL} "
             f"({exc}). Start it with: pux serve")
    if r.status_code >= 400:
        try:
            detail = r.json().get("detail", r.text)
        except Exception:  # noqa: BLE001
            detail = r.text
        _die(f"server returned {r.status_code}: {detail}")
    return r.json()


def _get(path: str) -> Any:
    try:
        r = httpx.get(f"{PUX_API_URL}{path}", timeout=TIMEOUT)
    except httpx.ConnectError as exc:
        _die(f"can't reach the Agent Protocol server at {PUX_API_URL} "
             f"({exc}). Start it with: pux serve")
    if r.status_code >= 400:
        _die(f"server returned {r.status_code}: {r.text}")
    return r.json()


def _die(msg: str) -> None:
    print(f"pux: {msg}", file=sys.stderr)
    raise SystemExit(1)


def _print_block(label: str, body: str) -> None:
    print(f"=== {label} ===")
    print(body.rstrip() if isinstance(body, str) else body)


def cmd_agents() -> None:
    agents = _post("/agents/search")
    print(f"{len(agents)} agents (orgs):")
    for a in agents:
        print(f"  {a['agent_id']:<22} {a['description']}")


def cmd_dispatch(org: str, task: str, recursion_limit: int, rubric: str | None = None) -> None:
    # With a --rubric override, send the input as a messages+rubric dict so the
    # server's _normalize_input passes it through and the override wins over the
    # org's shipped default rubric. Without --rubric, send a bare task string;
    # the server injects the org's default rubric if the org opted into the gate.
    if rubric:
        payload: Any = {
            "messages": [{"role": "user", "content": task}],
            "rubric": rubric,
        }
    else:
        payload = task
    res = _post("/runs/wait", agent_id=org, input=payload, recursion_limit=recursion_limit)
    status = res.get("status")
    if status == "error":
        _print_block("ERROR", res.get("error", "(no detail)"))
        raise SystemExit(1)
    _print_block("FINAL ANSWER", res.get("output", "(empty)"))
    print(f"\n[thread] {res.get('thread_id')}   [agent] {res.get('agent_id')}   "
          f"[status] {status}")
    print("(resume with: pux show <thread_id>)")


def cmd_resume(org: str | None) -> None:
    body: dict[str, Any] = {}
    if org:
        body["agent_id"] = org
    threads = _post("/threads/search", **body)
    if not threads:
        print("(no threads)")
        return
    for t in threads:
        print(f"  {t['thread_id']}   [agent] {t['agent_id']:<16} {t['created_at']}")


def cmd_show(thread_id: str) -> None:
    state = _get(f"/threads/{thread_id}")
    vals = state.get("values") or {}
    msgs = vals.get("messages") or []
    last = msgs[-1] if msgs else {}
    content = last.get("content") if isinstance(last, dict) else last
    _print_block(f"thread {thread_id} (agent={state.get('agent_id')}, "
                 f"status={state.get('status')})",
                 content or "(no messages)")


def cmd_history(thread_id: str) -> None:
    hist = _get(f"/threads/{thread_id}/history")
    print(f"{len(hist)} revisions for {thread_id}:")
    for h in hist:
        nexts = ",".join(h.get("next") or []) or "-"
        print(f"  {h.get('checkpoint_id')}  next={nexts}")


def cmd_run(thread_id: str, task: str, recursion_limit: int) -> None:
    res = _post(f"/threads/{thread_id}/runs", input=task, recursion_limit=recursion_limit)
    print(f"[run] {res.get('run_id')}  status={res.get('status')}  "
          f"thread={thread_id}  (wait with: pux wait {res.get('run_id')})")


def cmd_wait(run_id: str) -> None:
    res = _get(f"/runs/{run_id}/wait")
    if res.get("status") == "error":
        _print_block("ERROR", res.get("error", "(no detail)"))
        raise SystemExit(1)
    _print_block(f"run {run_id} (status={res.get('status')})", res.get("output", "(empty)"))


def cmd_jobs_run(org: str, job: str | None) -> None:
    body: dict[str, Any] = {}
    if job:
        body["job"] = job
    res = _post(f"/jobs/{org}/run", **body)
    jobs = res.get("jobs", [])
    if not jobs:
        print(res.get("message", "no jobs"))
        return
    failed = [j for j in jobs if j["status"] != "ok"]
    for j in jobs:
        status_icon = "ok" if j["status"] == "ok" else "FAIL"
        err = f"  error={j['error'][:120]}" if j.get("error") else ""
        print(f"  {j['name']:<24} {status_icon:<6} {j['duration']}s{err}")
    print(f"\n{len(jobs)} jobs run, {len(failed)} failed")
    if failed:
        raise SystemExit(1)


def cmd_jobs_status(org: str) -> None:
    res = _get(f"/jobs/{org}/status")
    jobs = res.get("jobs", [])
    if not jobs:
        print(res.get("message", "no jobs declared"))
        return
    print(f"{'NAME':<24} {'SCRIPT':<40} {'TIMEOUT':<8} DESCRIPTION")
    for j in jobs:
        timeout = f"{j['timeout']}s" if j["timeout"] else "none"
        print(f"  {j['name']:<22} {j['script']:<40} {timeout:<8} {j.get('description', '')}")


def main() -> None:
    ap = argparse.ArgumentParser(
        prog="pux", description="Pux Agent Protocol client (drives `pux serve`)."
    )
    sub = ap.add_subparsers(dest="cmd", required=True)

    sub.add_parser("agents", help="list orgs (agents)").set_defaults(func=lambda a: cmd_agents())

    p_disp = sub.add_parser("dispatch", help="ephemeral blocking run on an org")
    p_disp.add_argument("--org", default="general")
    p_disp.add_argument("--recursion-limit", type=int, default=60)
    p_disp.add_argument("--rubric", default=None,
                        help="override the org's shipped rubric (arms the "
                             "RubricMiddleware verify-gate for an opted-in org)")
    p_disp.add_argument("task")
    p_disp.set_defaults(func=lambda a: cmd_dispatch(a.org, a.task, a.recursion_limit, a.rubric))

    p_res = sub.add_parser("resume", help="list recent threads")
    p_res.add_argument("--org", default=None)
    p_res.set_defaults(func=lambda a: cmd_resume(a.org))

    p_show = sub.add_parser("show", help="show a thread's last message")
    p_show.add_argument("thread_id")
    p_show.set_defaults(func=lambda a: cmd_show(a.thread_id))

    p_hist = sub.add_parser("history", help="show a thread's revision history")
    p_hist.add_argument("thread_id")
    p_hist.set_defaults(func=lambda a: cmd_history(a.thread_id))

    p_run = sub.add_parser("run", help="background run on an existing thread")
    p_run.add_argument("--recursion-limit", type=int, default=60)
    p_run.add_argument("thread_id")
    p_run.add_argument("task")
    p_run.set_defaults(func=lambda a: cmd_run(a.thread_id, a.task, a.recursion_limit))

    p_wait = sub.add_parser("wait", help="block for a background run's output")
    p_wait.add_argument("run_id")
    p_wait.set_defaults(func=lambda a: cmd_wait(a.run_id))

    p_jobs = sub.add_parser("jobs", help="run prep jobs or show status")
    jobs_sub = p_jobs.add_subparsers(dest="jobs_cmd", required=True)

    p_jr = jobs_sub.add_parser("run", help="run prep jobs in the sandbox")
    p_jr.add_argument("--org", required=True, help="org to run jobs for")
    p_jr.add_argument("--job", default=None, help="run only this named job")
    p_jr.set_defaults(func=lambda a: cmd_jobs_run(a.org, a.job))

    p_js = jobs_sub.add_parser("status", help="show declared prep jobs")
    p_js.add_argument("--org", required=True, help="org to show jobs for")
    p_js.set_defaults(func=lambda a: cmd_jobs_status(a.org))

    args = ap.parse_args()
    args.func(args)


if __name__ == "__main__":
    main()
