#!/usr/bin/env python3
"""
audit_transcript — CLI front-end for transcript auditing.

Three subcommands:
  classify <session.jsonl>     Classify every turn, emit JSON
  summary <session.jsonl>      Emit only the aggregate summary
  compare <a.jsonl> <b.jsonl>  Diff two sessions' summaries (regression check)

LLM endpoint is optional. Without one, only the fast regex classifier runs.
With one, the LLM classifier runs only on turns the fast path flagged
(saves ~80% of model calls on healthy sessions).

Endpoint is read from ~/.pi/agent/settings.json — same providers the agent
uses. Configure with `auditModel` field (falls back to logic default).
"""

from __future__ import annotations

import argparse
import json
import os
import sys
from pathlib import Path
from typing import Optional
from urllib import error, request

# scripts/ is on sys.path when invoked via `python3 scripts/audit_transcript.py`
sys.path.insert(0, str(Path(__file__).parent))
import audit_lib


# --- Endpoint resolution ----------------------------------------------------

def load_settings() -> dict:
    """Read ~/.pi/agent/settings.json. Returns {} if missing/unparseable."""
    path = Path.home() / ".pi" / "agent" / "settings.json"
    if not path.exists():
        return {}
    try:
        return json.loads(path.read_text())
    except (json.JSONDecodeError, OSError):
        return {}


def resolve_endpoint(settings: dict) -> Optional[tuple[str, str, str]]:
    """Pick (baseUrl, apiKey, model) for the audit classifier.

    Priority: settings.auditModel -> defaultLogic -> first provider.
    Returns None if no usable endpoint.
    """
    providers = settings.get("providers") or {}
    if not providers:
        return None

    defaults = settings.get("defaults") or {}
    target_model = settings.get("auditModel") or defaults.get("logic")
    if target_model:
        for prov in providers.values():
            models = prov.get("models") or []
            if any(m.get("id") == target_model for m in models):
                return prov.get("baseUrl", ""), prov.get("apiKey", ""), target_model

    # Fall back to first provider's first model
    prov = next(iter(providers.values()))
    models = prov.get("models") or []
    if not models:
        return None
    return prov.get("baseUrl", ""), prov.get("apiKey", ""), models[0].get("id", "")


def make_llm_call(baseUrl: str, apiKey: str, model: str):
    """Return a callable(prompt) -> str that POSTs to /v1/chat/completions."""
    def call(prompt: str) -> str:
        url = baseUrl.rstrip("/") + "/v1/chat/completions"
        body = json.dumps({
            "model": model,
            "messages": [
                {"role": "system", "content": "You are a JSON-only transcript auditor."},
                {"role": "user", "content": prompt},
            ],
            "temperature": 0.0,
            "max_tokens": 512,
        }).encode("utf-8")
        headers = {"Content-Type": "application/json"}
        if apiKey:
            headers["Authorization"] = f"Bearer {apiKey}"
        req = request.Request(url, data=body, headers=headers, method="POST")
        try:
            with request.urlopen(req, timeout=60) as resp:
                payload = json.loads(resp.read())
                return payload["choices"][0]["message"]["content"]
        except (error.URLError, KeyError, json.JSONDecodeError) as exc:
            return json.dumps({"tags": [], "evidence": f"[llm call failed: {exc}]"})
    return call


# --- Subcommands ------------------------------------------------------------

def cmd_classify(args: argparse.Namespace) -> int:
    session_id, turns = audit_lib.load_transcript(args.session)

    llm_call = None
    if not args.fast_only:
        settings = load_settings()
        endpoint = resolve_endpoint(settings)
        if endpoint:
            llm_call = make_llm_call(*endpoint)
            if args.verbose:
                print(f"Using LLM classifier: {endpoint[2]} @ {endpoint[0]}", file=sys.stderr)
        elif args.verbose:
            print("No LLM endpoint configured; using fast classifier only", file=sys.stderr)

    classifications, summary = audit_lib.audit(turns, llm_call=llm_call)
    report = audit_lib.AuditReport(
        session_id=session_id,
        total_turns=len(turns),
        classifications=[c if isinstance(c, dict) else c.__dict__ for c in classifications],
        summary=summary,
    )

    print(json.dumps(report.to_dict(), indent=2))
    return 0


def cmd_summary(args: argparse.Namespace) -> int:
    session_id, turns = audit_lib.load_transcript(args.session)
    _, summary = audit_lib.audit(turns)
    summary["session_id"] = session_id
    print(json.dumps(summary, indent=2))
    return 0


def cmd_compare(args: argparse.Namespace) -> int:
    _, turns_a = audit_lib.load_transcript(args.session_a)
    _, turns_b = audit_lib.load_transcript(args.session_b)
    _, summary_a = audit_lib.audit(turns_a)
    _, summary_b = audit_lib.audit(turns_b)

    diff = {
        "session_a": {"path": args.session_a, "total_turns": summary_a["total_turns"]},
        "session_b": {"path": args.session_b, "total_turns": summary_b["total_turns"]},
        "tag_deltas": {},
        "regressions": [],
    }
    for tag in audit_lib.PATTERN_TAGS:
        a = summary_a["tag_rates"].get(tag, 0.0)
        b = summary_b["tag_rates"].get(tag, 0.0)
        diff["tag_deltas"][tag] = {"a": a, "b": b, "delta": b - a}
        if b > a * 1.5 and b > 0:
            diff["regressions"].append(tag)

    print(json.dumps(diff, indent=2))
    return 0


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(
        prog="audit_transcript",
        description="Audit agent transcripts against the Anthropic Fable/Mythos six-pattern taxonomy.",
    )
    sub = parser.add_subparsers(dest="cmd", required=True)

    p_classify = sub.add_parser("classify", help="Classify every turn, emit full JSON report")
    p_classify.add_argument("session", help="Path to .pux/sessions/<id>.jsonl")
    p_classify.add_argument("--fast-only", action="store_true",
                             help="Skip LLM classifier; use regex-only (cheap path)")
    p_classify.add_argument("-v", "--verbose", action="store_true")
    p_classify.set_defaults(func=cmd_classify)

    p_summary = sub.add_parser("summary", help="Emit aggregate summary only")
    p_summary.add_argument("session", help="Path to .pux/sessions/<id>.jsonl")
    p_summary.set_defaults(func=cmd_summary)

    p_compare = sub.add_parser("compare", help="Compare two sessions; flag regressions")
    p_compare.add_argument("session_a")
    p_compare.add_argument("session_b")
    p_compare.set_defaults(func=cmd_compare)

    args = parser.parse_args(argv)
    return args.func(args)


if __name__ == "__main__":
    sys.exit(main())
