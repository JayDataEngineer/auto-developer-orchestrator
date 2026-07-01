#!/usr/bin/env python3
"""Content clustering client for Pux sandbox workers.

Standalone CLI — groups text items into thematic clusters using LLM.
No DRE engine dependencies.

Usage:
    python3 content_cluster.py cluster --input texts.json
    python3 content_cluster.py cluster --input texts.json --existing-topics
    python3 content_cluster.py cluster --input texts.json --namespace myproject

Environment:
    LLM_API_URL       (default: http://localhost:18080/v1/chat/completions)
    LLM_MODEL         (default: qwen35-35b-a3b-vision)
    NEO4J_URI         (default: bolt://localhost:37687)
    NEO4J_USER        (default: neo4j)
    NEO4J_PASSWORD    (required for --existing-topics)
    NEO4J_DATABASE    (default: neo4j)
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


def get_llm_url():
    return os.environ.get("LLM_API_URL", "http://localhost:18080/v1/chat/completions")


def get_llm_model():
    return os.environ.get("LLM_MODEL", "qwen35-35b-a3b-vision")


def call_llm(prompt, model=None, temperature=0.3, max_tokens=16000):
    """Call LLM API and return response text."""
    import urllib.request

    data = json.dumps({
        "messages": [{"role": "user", "content": prompt}],
        "model": model or get_llm_model(),
        "temperature": temperature,
        "max_tokens": max_tokens,
    }).encode()

    req = urllib.request.Request(
        get_llm_url(),
        data=data,
        headers={"Content-Type": "application/json"},
    )

    try:
        with urllib.request.urlopen(req, timeout=180) as resp:
            result = json.loads(resp.read())
            return result["choices"][0]["message"]["content"]
    except Exception as e:
        print(f"ERROR: LLM API failed: {e}", file=sys.stderr)
        sys.exit(1)


def fetch_existing_topics(namespace="default"):
    """Fetch existing Topic nodes from Neo4j for reuse."""
    try:
        from neo4j import GraphDatabase
    except ImportError:
        print("WARNING: neo4j package not installed. Skipping topic reuse.", file=sys.stderr)
        return ""

    uri = os.environ.get("NEO4J_URI", "bolt://localhost:37687")
    user = os.environ.get("NEO4J_USER", "neo4j")
    password = os.environ.get("NEO4J_PASSWORD", "")

    if not password:
        return ""

    try:
        driver = GraphDatabase.driver(uri, auth=(user, password))
        database = os.environ.get("NEO4J_DATABASE", "neo4j")
        with driver.session(database=database) as session:
            result = session.run(
                "MATCH (t:Topic) WHERE t.namespace = $ns OR $ns = '__all__' "
                "RETURN t.name as name, t.summary as summary LIMIT 50",
                {"ns": namespace if namespace != "__all__" else "__all__"},
            )
            rows = [record.data() for record in result]
        driver.close()

        if not rows:
            return ""

        lines = [
            f"- {r['name']}" + (f": {r['summary']}" if r.get("summary") else "")
            for r in rows
        ]
        return (
            "EXISTING TOPICS (reuse these when the content fits):\n"
            + "\n".join(lines)
        )
    except Exception as e:
        print(f"WARNING: Failed to query Neo4j topics: {e}", file=sys.stderr)
        return ""


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

    # Fetch existing topics from Neo4j if requested
    topics_section = ""
    if args.existing_topics:
        topics_section = fetch_existing_topics(namespace=args.namespace or "default")
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
    p.add_argument("--existing-topics", action="store_true", help="Query Neo4j for existing topics to reuse")
    p.add_argument("--namespace", default=None, help="Neo4j namespace for topic lookup")
    p.add_argument("--model", help="LLM model to use")

    args = parser.parse_args()
    if not args.command:
        parser.print_help()
        sys.exit(1)

    commands = {"cluster": cmd_cluster}
    commands[args.command](args)


if __name__ == "__main__":
    main()
