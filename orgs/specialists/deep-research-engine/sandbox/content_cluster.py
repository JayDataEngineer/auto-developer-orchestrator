#!/usr/bin/env python3
"""Content clustering client for Pux sandbox workers.

Standalone CLI — groups text items into thematic clusters using LLM.
Optional --existing-topics reuses topic names already in SurrealDB.

Usage:
    python3 content_cluster.py cluster --input texts.json
    python3 content_cluster.py cluster --input texts.json --existing-topics

Environment:
    LLM_API_URL       OpenAI-compatible /v1/chat/completions endpoint. REQUIRED.
                      The sandbox policy injects this from the harness model tier
                      (``sandbox.llm: <role>`` in policy.yaml → resolved via
                      models.yaml); for standalone use, export it yourself.
    LLM_MODEL         Model id (e.g. "mimo-v2.5"). REQUIRED. Same injection path.
    LLM_API_KEY       API key. Optional (some local endpoints need none).
    SURREALDB_URL     (default: http://localhost:8000)
    SURREALDB_NS      (default: research)
    SURREALDB_DB      (default: main)
    SURREALDB_USER    (default: root)
    SURREALDB_PASS    (default: root)
"""

import argparse
import json
import os
import sys
from pathlib import Path


CLUSTER_PROMPT = """You are a content clustering specialist. Group the following items into thematic clusters.

**RULES:**
1. Create 3-15 clusters covering all the content.
2. Each cluster has a concise name, a one-sentence summary, a list of suggested labels/tags, and key entities mentioned.
3. Group by topic, person, time period, or activity.
4. Items may belong to multiple clusters (use overlapping indices).
5. Prefer existing topic names when the content clearly fits.

{topics_section}

**OUTPUT FORMAT — respond with ONLY valid JSON:**
```json
{{
  "clusters": [
    {{
      "name": "...",
      "summary": "...",
      "item_indices": [0, 5],
      "suggested_labels": ["..."],
      "key_entities": ["..."]
    }}
  ]
}}
```

**SOURCE TEXT (items separated by ---):**

{combined}"""


def _llm_env_required():
    """The sandbox policy (``sandbox.llm: <role>``) injects these from the
    harness model tier (models.yaml). This script is a DUMB CONSUMER — it does
    not re-resolve the provider, model, or key."""
    url = os.environ.get("LLM_API_URL", "")
    model = os.environ.get("LLM_MODEL", "")
    if not url or not model:
        print(
            "ERROR: LLM_API_URL and LLM_MODEL must be set. The sandbox policy\n"
            "injects them via `sandbox.llm: <role>` (resolved from models.yaml).\n"
            "For standalone use:\n"
            '  export LLM_API_URL="https://opencode.ai/zen/go/v1/chat/completions"\n'
            '  export LLM_MODEL="mimo-v2.5"\n'
            '  export LLM_API_KEY="<key>"',
            file=sys.stderr,
        )
        sys.exit(1)
    return url, model


def call_llm(prompt, model=None, temperature=0.3, max_tokens=16000):
    """Call LLM API and return response text."""
    import urllib.request

    url, default_model = _llm_env_required()
    api_key = os.environ.get("LLM_API_KEY", "")
    headers = {
        "Content-Type": "application/json",
        # Cloudflare bot-integrity (error 1010) bans the default
        # ``Python-urllib/3.x`` signature. A neutral identifier passes.
        "User-Agent": "pux-harness-sandbox/1.0",
    }
    if api_key:
        headers["Authorization"] = f"Bearer {api_key}"

    data = json.dumps({
        "messages": [{"role": "user", "content": prompt}],
        "model": model or default_model,
        "temperature": temperature,
        "max_tokens": max_tokens,
    }).encode()

    req = urllib.request.Request(
        url,
        data=data,
        headers=headers,
    )

    try:
        with urllib.request.urlopen(req, timeout=180) as resp:
            result = json.loads(resp.read())
            return result["choices"][0]["message"]["content"]
    except Exception as e:
        print(f"ERROR: LLM API failed: {e}", file=sys.stderr)
        sys.exit(1)


def fetch_existing_topics():
    """Fetch existing topic records from SurrealDB for reuse.

    Queries the `topic` table for name + summary. Topics are linked into the
    graph via RELATE (extracted_from / mentions edges), but for clustering we
    only need the surface labels. Uses the SurrealDB /sql HTTP endpoint —
    same pattern as surreal_client.py.
    """
    url = os.environ.get("SURREALDB_URL", "http://localhost:8000") + "/sql"
    ns = os.environ.get("SURREALDB_NS", "research")
    db = os.environ.get("SURREALDB_DB", "main")
    user = os.environ.get("SURREALDB_USER", "root")
    password = os.environ.get("SURREALDB_PASS", "root")

    sql = "SELECT name, summary FROM topic LIMIT 50;"
    req = urllib.request.Request(
        url,
        data=sql.encode(),
        headers={
            "Content-Type": "text/plain",
            "Accept": "application/json",
            "surreal-ns": ns,
            "surreal-db": db,
        },
    )

    try:
        import base64
        auth = base64.b64encode(f"{user}:{password}".encode()).decode()
        req.add_header("Authorization", f"Basic {auth}")
    except Exception:
        pass

    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            payload = json.loads(resp.read())
    except Exception as e:
        print(f"WARNING: Failed to query SurrealDB topics: {e}", file=sys.stderr)
        return ""

    # SurrealDB /sql returns a list of result objects (one per statement).
    # Extract rows from the first (only) result.
    rows = []
    if isinstance(payload, list) and payload:
        first = payload[0]
        if isinstance(first, dict) and "result" in first:
            rows = first["result"] or []

    if not rows:
        return ""

    lines = [
        f"- {r.get('name', '?')}" + (f": {r['summary']}" if r.get("summary") else "")
        for r in rows
        if isinstance(r, dict)
    ]
    if not lines:
        return ""
    return (
        "EXISTING TOPICS (reuse these when the content fits):\n"
        + "\n".join(lines)
    )


def parse_clusters(raw):
    """Extract cluster list from LLM response text."""
    text = raw.strip()
    if "```json" in text:
        text = text.split("```json")[1].split("```")[0].strip()
    elif "```" in text:
        text = text.split("```")[1].split("```")[0].strip()

    try:
        parsed = json.loads(text)
        if isinstance(parsed, dict) and "clusters" in parsed:
            return parsed["clusters"]
        if isinstance(parsed, list):
            return parsed
    except json.JSONDecodeError:
        # Try to repair truncated JSON by closing open brackets
        for repair in [']}', '}]', '"}]}']:
            try:
                parsed = json.loads(text.rstrip(', "') + repair)
                if isinstance(parsed, dict) and "clusters" in parsed:
                    return parsed["clusters"]
                if isinstance(parsed, list):
                    return parsed
            except json.JSONDecodeError:
                continue

    print(f"WARNING: Could not parse clusters from LLM output: {text[:200]}", file=sys.stderr)
    return []


def cmd_cluster(args):
    """Cluster text items into thematic groups."""
    data = json.loads(Path(args.input).read_text(encoding="utf-8"))

    # Collect text chunks from input
    text_chunks = []
    if isinstance(data, list):
        for item in data:
            if isinstance(item, str):
                text_chunks.append(item)
            elif isinstance(item, dict):
                text = item.get("text", item.get("content", ""))
                if text:
                    text_chunks.append(str(text))
    elif isinstance(data, dict):
        # Single document
        text = data.get("text", data.get("content", ""))
        if text:
            text_chunks.append(str(text))

    if not text_chunks:
        print("No text to cluster", file=sys.stderr)
        sys.exit(1)

    combined = "\n\n---\n\n".join(text_chunks)[:80000]

    # Fetch existing topics from SurrealDB if requested
    topics_section = ""
    if args.existing_topics:
        topics_section = fetch_existing_topics()
        if topics_section:
            print(f"Found existing topics for reuse", file=sys.stderr)

    # Build prompt and call LLM
    prompt = CLUSTER_PROMPT.format(topics_section=topics_section, combined=combined)
    raw = call_llm(prompt, model=args.model)
    clusters = parse_clusters(raw)

    if not clusters:
        print("No clusters produced", file=sys.stderr)
        sys.exit(1)

    output = {"clusters": clusters, "source_items": len(text_chunks)}

    if args.output:
        Path(args.output).write_text(json.dumps(output, indent=2, ensure_ascii=False))
        print(json.dumps({"status": "ok", "clusters": len(clusters), "output": args.output}))
    else:
        print(json.dumps(output, indent=2, ensure_ascii=False))

    print(f"\n({len(clusters)} clusters from {len(text_chunks)} items)", file=sys.stderr)


def main():
    parser = argparse.ArgumentParser(description="Content clustering for Pux sandbox")
    sub = parser.add_subparsers(dest="command")

    p = sub.add_parser("cluster", help="Cluster text items into thematic groups")
    p.add_argument("--input", required=True, help="JSON file with text items")
    p.add_argument("--output", help="Output JSON file (default: stdout)")
    p.add_argument("--existing-topics", action="store_true", help="Query SurrealDB for existing topic names to reuse")
    p.add_argument("--model", help="LLM model to use")

    args = parser.parse_args()
    if not args.command:
        parser.print_help()
        sys.exit(1)

    commands = {"cluster": cmd_cluster}
    commands[args.command](args)


if __name__ == "__main__":
    main()
