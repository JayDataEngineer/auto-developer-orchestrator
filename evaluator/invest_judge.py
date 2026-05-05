#!/usr/bin/env python3
"""
Invest-Bot LLM Judge — domain-specific evaluation for trading decisions.

Scores invest-bot traces on finance-specific dimensions:
  - data_groundedness (0-1): Did the model cite actual market numbers?
  - risk_awareness (0-1): Position sizing, stop-loss, portfolio heat mentioned?
  - reasoning_quality (0-1): Sound logic or confusion?
  - signal_calibration (boolean): When confidence > 0.8, was the trade profitable?

Usage:
  python3 invest_judge.py                    # Score all unscored invest traces
  python3 invest_judge.py --watch            # Continuous polling
  python3 invest_judge.py --dry-run          # Preview scores without posting
  python3 invest_judge.py --limit 10         # Score at most 10 traces
"""

import argparse
import json
import os
import re
import sys
import time
from datetime import datetime, timezone
from urllib.request import Request, urlopen
from urllib.error import HTTPError, URLError

from metrics_client import LangfuseMetricsClient


# ── Config ───────────────────────────────────────────────────────────

class Config:
    def __init__(self):
        self.judge_url = os.environ.get("JUDGE_MODEL_URL", "https://openrouter.ai/api")
        self.judge_model = os.environ.get("JUDGE_MODEL_NAME", "deepseek/deepseek-v4-flash")
        self.judge_api_key = os.environ.get("JUDGE_API_KEY", "")


# ── Invest-specific Judge Prompt ────────────────────────────────────

INVEST_JUDGE_SYSTEM = """You are an expert evaluator for an AI trading assistant.
Given a trace of the agent's execution, score it on these finance-specific dimensions.

Respond with ONLY a JSON object, no other text:
{
  "data_groundedness": <0-1>,
  "risk_awareness": <0-1>,
  "reasoning_quality": <0-1>,
  "signal_direction": "<buy|sell|hold|unknown>",
  "signal_tickers": ["TICKER1", "TICKER2"],
  "reasoning": "<1-2 sentence explanation>"
}

Scoring rubric:
- data_groundedness: Did the model cite actual numbers (prices, PE, RSI, revenue)?
  1.0 = multiple specific data points cited, 0.5 = some numbers mentioned,
  0.0 = vague claims without data backing
- risk_awareness: Did the model consider position sizing, stop-loss, portfolio heat,
  drawdown limits, or diversification?
  1.0 = explicit risk management, 0.5 = acknowledged risk but vague,
  0.0 = no risk consideration at all
- reasoning_quality: Is the trading logic sound?
  1.0 = clear cause-effect, considers multiple factors, acknowledges uncertainty
  0.5 = reasonable but missing key factors or logical leaps
  0.0 = contradictory, confused correlation with causation, or no reasoning
- signal_direction: What trading action did the model recommend?
- signal_tickers: Which tickers were discussed for trading?
"""


def call_judge(cfg: Config, trace_summary: str) -> dict | None:
    """Send trace to LLM for scoring. Returns parsed scores or None."""
    url = f"{cfg.judge_url.rstrip('/')}/v1/chat/completions"
    body = {
        "model": cfg.judge_model,
        "messages": [
            {"role": "system", "content": INVEST_JUDGE_SYSTEM},
            {"role": "user", "content": trace_summary},
        ],
        "max_tokens": 300,
        "temperature": 0.2,
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
            if not text.strip():
                text = msg.get("reasoning_content", "") or ""
            text = text.strip()
            # Strip thinking tags
            text = re.sub(r"<think[^>]*>.*?</think\s*>", "", text, flags=re.DOTALL).strip()
            start = text.find("{")
            end = text.rfind("}") + 1
            if start >= 0 and end > start:
                return json.loads(text[start:end])
            return None
    except (HTTPError, URLError, json.JSONDecodeError, KeyError, IndexError) as e:
        print(f"  Judge error: {e}")
        return None


def build_invest_trace_summary(trace: dict) -> str:
    """Build a concise summary focused on trading decisions."""
    parts = []
    parts.append(f"Trace: {trace.get('name', 'unknown')}")
    parts.append(f"Tags: {', '.join(trace.get('tags', []))}")

    inp = trace.get("input")
    if inp:
        inp_str = str(inp)[:500]
        parts.append(f"User prompt: {inp_str}")

    # Observations — focus on tool calls
    obs_list = trace.get("observations", [])
    if isinstance(obs_list, list):
        tool_calls = []
        for obs in obs_list:
            if isinstance(obs, dict):
                otype = obs.get("type", "?")
                name = obs.get("name", "?")
                if otype == "SPAN" and name.startswith("tool:"):
                    tool_name = name[5:]
                    obs_input = obs.get("input", {})
                    obs_output = obs.get("output", {})

                    call_desc = f"  [{tool_name}]"
                    if isinstance(obs_input, dict):
                        args_preview = str(obs_input)[:200]
                        call_desc += f" args={args_preview}"
                    if isinstance(obs_output, dict):
                        result_preview = str(obs_output)[:300]
                        call_desc += f" result={result_preview}"
                    tool_calls.append(call_desc)

        if tool_calls:
            parts.append(f"\nTool calls ({len(tool_calls)}):")
            parts.extend(tool_calls)

    # Output
    out = trace.get("output")
    if out:
        parts.append(f"\nFinal output: {str(out)[:500]}")

    return "\n".join(parts)


def has_invest_scores(lf: LangfuseMetricsClient, trace_id: str) -> bool:
    """Check if trace already has invest judge scores."""
    scores = lf.get_scores(trace_id=trace_id)
    if not scores:
        return False
    score_names = {s.get("name", "") for s in scores if isinstance(s, dict)}
    return "data_groundedness" in score_names


def score_invest_trace(lf: LangfuseMetricsClient, cfg: Config, trace: dict, dry_run=False) -> bool:
    """Score a single invest trace via LLM judge."""
    tid = trace.get("id", "?")
    tags = trace.get("tags", [])

    print(f"  Judging trace {tid[:20]}... (tags={tags})")

    full_trace = lf.get_trace(tid)
    if not full_trace:
        print(f"    Could not fetch full trace, skipping")
        return False

    summary = build_invest_trace_summary(full_trace)
    scores = call_judge(cfg, summary)

    if not scores:
        print(f"    Judge returned no scores, skipping")
        return False

    groundedness = scores.get("data_groundedness")
    risk = scores.get("risk_awareness")
    reasoning = scores.get("reasoning_quality")
    explanation = scores.get("reasoning", "")

    if groundedness is None:
        print(f"    Incomplete scores: {scores}")
        return False

    print(f"    grounded={groundedness} risk={risk} reasoning={reasoning}")

    if dry_run:
        print(f"    (dry run — not posting)")
        return True

    # Post invest-specific scores
    lf.post_score(tid, "data_groundedness", groundedness, "NUMERIC", explanation)
    lf.post_score(tid, "risk_awareness", risk, "NUMERIC", explanation)
    lf.post_score(tid, "reasoning_quality", reasoning, "NUMERIC", explanation)

    # Post signal direction as categorical
    direction = scores.get("signal_direction", "unknown")
    valid_dirs = {"buy", "sell", "hold", "unknown"}
    if direction in valid_dirs:
        lf.post_score(tid, "judge_signal_direction", direction, "CATEGORICAL", explanation)

    # Post per-ticker signals
    for ticker in scores.get("signal_tickers", [])[:5]:
        lf.post_score(tid, f"ticker:{ticker}", direction, "CATEGORICAL", explanation)

    return True


def main():
    parser = argparse.ArgumentParser(description="Invest-Bot LLM Judge")
    parser.add_argument("--watch", action="store_true", help="Continuous polling mode")
    parser.add_argument("--interval", type=int, default=120, help="Poll interval (default: 120s)")
    parser.add_argument("--limit", type=int, default=20, help="Max traces per cycle")
    parser.add_argument("--dry-run", action="store_true", help="Preview without posting")
    args = parser.parse_args()

    cfg = Config()
    lf = LangfuseMetricsClient()

    print(f"Invest Judge — {lf.base}")
    print(f"Judge model: {cfg.judge_model}")
    print()

    if args.watch:
        print(f"Watch mode — polling every {args.interval}s (Ctrl+C to stop)")
        while True:
            try:
                traces = lf.get_traces(tags=["investing"], limit=50)
                unscored = [t for t in traces if not has_invest_scores(lf, t.get("id", ""))]
                if unscored:
                    print(f"\n[{datetime.now(timezone.utc).strftime('%H:%M:%S')}] {len(unscored)} unscored invest traces")
                    count = 0
                    for t in unscored[:args.limit]:
                        if score_invest_trace(lf, cfg, t, dry_run=args.dry_run):
                            count += 1
                    print(f"Scored {count}/{len(unscored)} traces")
                else:
                    print(f"[{datetime.now(timezone.utc).strftime('%H:%M:%S')}] No unscored invest traces")
                time.sleep(args.interval)
            except KeyboardInterrupt:
                print("\nStopped.")
                break
    else:
        traces = lf.get_traces(tags=["investing"], limit=50)
        if not traces:
            print("No invest traces found.")
            return
        unscored = [t for t in traces if not has_invest_scores(lf, t.get("id", ""))]
        if not unscored:
            print("All invest traces already scored.")
            return
        print(f"Found {len(unscored)} unscored invest traces")
        count = 0
        for t in unscored[:args.limit]:
            if score_invest_trace(lf, cfg, t, dry_run=args.dry_run):
                count += 1
        print(f"\nScored {count}/{len(unscored[:args.limit])} traces")


if __name__ == "__main__":
    main()
