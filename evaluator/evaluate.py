#!/usr/bin/env python3
"""
Langfuse Live Evaluator — standalone add-on for the Orchestrator.

Polls Langfuse for unscored traces, runs LLM-as-a-Judge, posts scores back.
Completely decoupled from the Go backend — no code changes needed to add/remove.

Usage:
  python3 evaluate.py                    # Score all unscored traces
  python3 evaluate.py --watch            # Continuous polling (every 60s)
  python3 evaluate.py --limit 5          # Score at most 5 traces
  python3 evaluate.py --dry-run          # Show what would be scored, don't post

Config via environment variables (or .env file):
  LANGFUSE_URL       — Langfuse base URL (default: http://localhost:3100)
  LANGFUSE_PK        — public key (default: pk-orch-2026-lf-a1b2c3d4e5f6)
  LANGFUSE_SK        — secret key (default: sk-orch-2026-lf-a1b2c3d4e5f6)
  JUDGE_MODEL_URL    — LLM endpoint for judging (default: https://openrouter.ai/api)
  JUDGE_MODEL_NAME   — model name for judge calls (default: deepseek/deepseek-v4-flash)
  JUDGE_API_KEY      — API key for judge model (required for cloud providers)
"""

import argparse
import json
import os
import sys
import time
from datetime import datetime, timezone
from urllib.request import Request, urlopen
from urllib.error import HTTPError, URLError


# ── Config ───────────────────────────────────────────────────────────

def load_env():
    """Load .env file if present."""
    env_path = os.path.join(os.path.dirname(__file__), ".env")
    if os.path.exists(env_path):
        with open(env_path) as f:
            for line in f:
                line = line.strip()
                if line and not line.startswith("#") and "=" in line:
                    key, _, val = line.partition("=")
                    os.environ.setdefault(key.strip(), val.strip())


class Config:
    def __init__(self):
        load_env()
        self.lf_url = os.environ.get("LANGFUSE_URL", "http://localhost:3100")
        self.lf_pk = os.environ.get("LANGFUSE_PK", "pk-orch-2026-lf-a1b2c3d4e5f6")
        self.lf_sk = os.environ.get("LANGFUSE_SK", "sk-orch-2026-lf-a1b2c3d4e5f6")
        self.judge_url = os.environ.get("JUDGE_MODEL_URL", "https://openrouter.ai/api")
        self.judge_model = os.environ.get("JUDGE_MODEL_NAME", "deepseek/deepseek-v4-flash")
        self.judge_api_key = os.environ.get("JUDGE_API_KEY", "")


# ── Langfuse API client ─────────────────────────────────────────────

class LangfuseClient:
    def __init__(self, cfg: Config):
        self.base = cfg.lf_url.rstrip("/")
        self.pk = cfg.lf_pk
        self.sk = cfg.lf_sk

    def _req(self, method, path, body=None):
        url = f"{self.base}/api/public{path}"
        data = json.dumps(body).encode() if body else None
        req = Request(url, data=data, method=method)
        req.add_header("Content-Type", "application/json")
        # Basic auth
        import base64
        cred = base64.b64encode(f"{self.pk}:{self.sk}".encode()).decode()
        req.add_header("Authorization", f"Basic {cred}")
        try:
            with urlopen(req, timeout=30) as resp:
                if resp.status == 204:
                    return None
                return json.loads(resp.read())
        except HTTPError as e:
            body_text = e.read().decode() if e.fp else ""
            print(f"  API error {e.code} on {method} {path}: {body_text[:200]}")
            return None

    def get_traces(self, limit=50):
        return self._req("GET", f"/traces?limit={limit}")

    def get_trace(self, trace_id):
        return self._req("GET", f"/traces/{trace_id}")

    def get_scores(self, trace_id):
        data = self._req("GET", f"/scores?traceId={trace_id}")
        return data if isinstance(data, list) else data.get("data", []) if data else []

    def post_score(self, trace_id, name, value, data_type, comment=""):
        body = {
            "traceId": trace_id,
            "name": name,
            "value": value,
            "dataType": data_type,
            "source": "API",
            "comment": comment,
        }
        return self._req("POST", "/scores", body)


# ── LLM Judge ────────────────────────────────────────────────────────

JUDGE_SYSTEM = """You are an expert evaluator for an AI coding assistant (orchestrator agent).
Given a trace of the agent's execution, score it on these dimensions.

Respond with ONLY a JSON object, no other text:
{
  "response_quality": <0-10>,
  "tool_efficiency": <0-10>,
  "task_completed": <true/false>,
  "action_quality": "<correct|partially_correct|incorrect|unknown>",
  "reasoning": "<1-2 sentence explanation>"
}

Scoring rubric:
- response_quality: Does the final response address the user's request? Is it accurate and helpful?
  10 = perfect answer, 7 = good but incomplete, 4 = partially relevant, 0 = useless/wrong
- tool_efficiency: Did the agent use tools well? Minimal effective tool use scores high.
  10 = used exactly the right tools efficiently, 7 = good but some waste, 4 = excessive tool calls, 0 = wrong tools or spiral
- task_completed: Did the agent actually finish the task or did it give up/error out?
- action_quality: Were the agent's actions (tool calls, decisions) correct?
  correct = actions were appropriate and accurate
  partially_correct = some actions were right but others were unnecessary or slightly off
  incorrect = actions were wrong, inappropriate, or harmful
  unknown = not enough information to judge
"""


def call_judge(cfg: Config, trace_summary: str) -> dict | None:
    """Send trace to LLM for scoring. Returns parsed scores or None."""
    url = f"{cfg.judge_url.rstrip('/')}/v1/chat/completions"
    body = {
        "model": cfg.judge_model,
        "messages": [
            {"role": "system", "content": JUDGE_SYSTEM},
            {"role": "user", "content": trace_summary},
        ],
        "max_tokens": 300,
        "temperature": 0.3,
    }
    data = json.dumps(body).encode()
    req = Request(url, data=data, method="POST")
    req.add_header("Content-Type", "application/json")
    if cfg.judge_api_key:
        req.add_header("Authorization", f"Bearer {cfg.judge_api_key}")
    try:
        with urlopen(req, timeout=60) as resp:
            result = json.loads(resp.read())
            msg = result["choices"][0]["message"]
            text = msg.get("content", "") or ""
            # Gemma 4 puts response in reasoning_content with empty content
            if not text.strip():
                text = msg.get("reasoning_content", "") or ""
            text = text.strip()
            # Strip thinking tags if present
            if "<think" in text:
                import re
                text = re.sub(r"<think[^>]*>.*?</think\s*>", "", text, flags=re.DOTALL).strip()
            # Parse JSON from response
            start = text.find("{")
            end = text.rfind("}") + 1
            if start >= 0 and end > start:
                return json.loads(text[start:end])
            return None
    except (HTTPError, URLError, json.JSONDecodeError, KeyError, IndexError) as e:
        print(f"  Judge error: {e}")
        return None


def build_trace_summary(trace: dict) -> str:
    """Build a concise summary of a trace for the judge LLM."""
    parts = []
    parts.append(f"Trace: {trace.get('name', 'unknown')}")
    parts.append(f"Session: {trace.get('metadata', {}).get('session', 'unknown')}")
    parts.append(f"Total latency: {trace.get('latency', 0)}ms")

    # Input/output if available
    inp = trace.get("input")
    out = trace.get("output")
    if inp:
        inp_str = str(inp)[:300]
        parts.append(f"Input: {inp_str}")
    if out:
        out_str = str(out)[:500]
        parts.append(f"Output: {out_str}")

    # Observations
    obs_list = trace.get("observations", [])
    if isinstance(obs_list, list):
        parts.append(f"\nObservations ({len(obs_list)}):")
        for obs in obs_list:
            if isinstance(obs, dict):
                otype = obs.get("type", "?")
                name = obs.get("name", "?")
                model = obs.get("model", "")
                usage = obs.get("usage", {})
                latency = obs.get("latency", 0)

                line = f"  [{otype}] {name}"
                if model:
                    line += f" model={model}"
                if latency:
                    line += f" latency={latency}ms"
                if usage and any(v for v in usage.values() if isinstance(v, (int, float))):
                    line += f" tokens={usage}"
                parts.append(line)

                # Include observation output for generations
                obs_out = obs.get("output")
                if obs_out and otype == "GENERATION":
                    out_str = str(obs_out)[:300]
                    parts.append(f"    output: {out_str}")

    return "\n".join(parts)


# ── Main ─────────────────────────────────────────────────────────────

def find_unscored_traces(lf: LangfuseClient, scored_names: set):
    """Find traces that don't have all scored_names yet."""
    traces_data = lf.get_traces(limit=100)
    if not traces_data:
        return []
    traces = traces_data.get("data", traces_data) if isinstance(traces_data, dict) else traces_data
    unscored = []
    for t in traces:
        tid = t.get("id", "")
        if not tid:
            continue
        scores = lf.get_scores(tid)
        scored_names_for_trace = {s["name"] for s in (scores or []) if isinstance(s, dict)}
        # A trace needs scoring if it's missing ANY of the expected score names
        if not scored_names_for_trace.issuperset(scored_names):
            unscored.append(t)
    return unscored


def score_trace(lf: LangfuseClient, cfg: Config, trace: dict, dry_run=False):
    """Score a single trace via LLM judge and post results."""
    tid = trace.get("id", "?")
    name = trace.get("name", "?")
    session = trace.get("metadata", {}).get("session", "?")

    print(f"Evaluating trace {tid[:30]}... (session={session})")

    # Fetch full trace with observations
    full_trace = lf.get_trace(tid)
    if not full_trace:
        print(f"  Could not fetch full trace, skipping")
        return False

    summary = build_trace_summary(full_trace)
    scores = call_judge(cfg, summary)

    if not scores:
        print(f"  Judge returned no scores, skipping")
        return False

    quality = scores.get("response_quality")
    efficiency = scores.get("tool_efficiency")
    completed = scores.get("task_completed")
    action_quality = scores.get("action_quality", "unknown")
    reasoning = scores.get("reasoning", "")

    if quality is None:
        print(f"  Judge returned incomplete scores: {scores}")
        return False

    print(f"  quality={quality}/10  efficiency={efficiency}/10  completed={completed}  action={action_quality}")
    print(f"  reason: {reasoning[:120]}")

    if dry_run:
        print(f"  (dry run — not posting scores)")
        return True

    # Post scores
    lf.post_score(tid, "response_quality", quality, "NUMERIC", reasoning)
    lf.post_score(tid, "tool_efficiency", efficiency, "NUMERIC", reasoning)
    if completed is not None:
        val = 1 if completed else 0
        lf.post_score(tid, "task_completed", val, "BOOLEAN", reasoning)

    # Post categorical action quality score
    valid_actions = {"correct", "partially_correct", "incorrect", "unknown"}
    if action_quality in valid_actions:
        lf.post_score(tid, "action_quality", action_quality, "CATEGORICAL", reasoning)

    return True


def main():
    parser = argparse.ArgumentParser(description="Langfuse Live Evaluator")
    parser.add_argument("--watch", action="store_true", help="Continuous polling mode")
    parser.add_argument("--interval", type=int, default=60, help="Poll interval in seconds (default: 60)")
    parser.add_argument("--limit", type=int, default=50, help="Max traces to process per cycle")
    parser.add_argument("--dry-run", action="store_true", help="Show scores without posting")
    args = parser.parse_args()

    cfg = Config()
    lf = LangfuseClient(cfg)

    scored_names = {"response_quality", "tool_efficiency", "task_completed", "action_quality"}

    print(f"Langfuse Evaluator — {cfg.lf_url}")
    print(f"Judge model: {cfg.judge_model} @ {cfg.judge_url}")
    print()

    if args.watch:
        print(f"Watch mode — polling every {args.interval}s (Ctrl+C to stop)")
        while True:
            try:
                unscored = find_unscored_traces(lf, scored_names)
                if unscored:
                    print(f"\n[{datetime.now(timezone.utc).strftime('%H:%M:%S')}] Found {len(unscored)} unscored traces")
                    count = 0
                    for t in unscored[:args.limit]:
                        if score_trace(lf, cfg, t, dry_run=args.dry_run):
                            count += 1
                    print(f"Scored {count}/{len(unscored)} traces")
                else:
                    print(f"[{datetime.now(timezone.utc).strftime('%H:%M:%S')}] No unscored traces")
                time.sleep(args.interval)
            except KeyboardInterrupt:
                print("\nStopped.")
                break
    else:
        unscored = find_unscored_traces(lf, scored_names)
        if not unscored:
            print("No unscored traces found.")
            return
        print(f"Found {len(unscored)} unscored traces")
        count = 0
        for t in unscored[:args.limit]:
            if score_trace(lf, cfg, t, dry_run=args.dry_run):
                count += 1
        print(f"\nScored {count}/{len(unscored[:args.limit])} traces")


if __name__ == "__main__":
    main()
