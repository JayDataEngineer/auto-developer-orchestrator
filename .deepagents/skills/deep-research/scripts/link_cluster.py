#!/usr/bin/env python3
"""Link a face/voice cluster to a named entity via a same_as edge.

This is the agent-facing CLI for LLM identity resolution (Agent Pipeline
Step 2 in AGENTS.md). After reasoning about a cluster's photos, OCR, and
surrounding message context, the agent calls this to persist the linkage:

    python3 .deepagents/skills/deep-research/scripts/link_cluster.py face_cluster_2 ent_Christopher_Semok \
        --confidence 0.75 \
        --signal llm_visual_context_reasoning \
        --reasoning "Cluster photos show screenshots of Telegram convos \
                     about Commissar (Semok alias). Sender = Semok."

The edge goes from person:<cluster> -> same_as -> person:<entity>. Once
written, build_entity_dossiers.py will populate the named entity's
appears_in/ folder with the cluster's media on the next run.

Idempotent: deterministic edge ID means re-running doesn't create dupes.
"""
import argparse
import json
import os
import sys
import urllib.request
from pathlib import Path

URL = os.environ.get("SURREALDB_URL", "http://127.0.0.1:8000/sql")
NS = os.environ.get("SURREALDB_NS", "research")
DB = os.environ.get("SURREALDB_DB", "main")


def sql(query, auth=None):
    """Execute SurrealQL. auth is base64 'user:pass' or None."""
    headers = {
        "Accept": "application/json",
        "surreal-ns": NS,
        "surreal-db": DB,
        "Content-Type": "text/plain",
    }
    if auth:
        headers["Authorization"] = f"Basic {auth}"
    req = urllib.request.Request(URL, data=query.encode("utf-8"), headers=headers, method="POST")
    with urllib.request.urlopen(req, timeout=15) as r:
        return json.loads(r.read().decode("utf-8"))


def jval(s):
    """Serialize a Python string as a SurrealDB string literal."""
    return json.dumps(str(s))


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[1])
    ap.add_argument("cluster", help="Cluster ID, e.g. face_cluster_2 or voice_cluster_5")
    ap.add_argument("entity", help="Entity ID suffix, e.g. ent_Christopher_Semok "
                                   "(the script prepends 'person:ent_')")
    ap.add_argument("--confidence", type=float, default=0.7,
                    help="Confidence score 0.0-1.0 (default 0.7)")
    ap.add_argument("--signal", default="llm_reasoning",
                    help="Signal label (default llm_reasoning)")
    ap.add_argument("--reasoning", default="",
                    help="Free-text justification for the linkage")
    args = ap.parse_args()

    auth = os.environ.get("SURREALDB_AUTH")
    if not auth:
        try:
            auth_bytes = b"root:root"
            import base64
            auth = base64.b64encode(auth_bytes).decode("ascii")
        except Exception:
            auth = None

    cluster_node = args.cluster if args.cluster.startswith("person:") else f"person:{args.cluster}"
    if not args.entity.startswith("person:"):
        entity_suffix = args.entity[4:] if args.entity.startswith("ent_") else args.entity
        entity_node = f"person:ent_{entity_suffix}"
    else:
        entity_node = args.entity

    # Validate both nodes exist
    for node in (cluster_node, entity_node):
        try:
            r = sql(f"SELECT id FROM {node};", auth=auth)
            rows = r[0].get("result", []) if r and r[0].get("result") else []
            if not rows:
                print(f"ERROR: {node} not found in SurrealDB", file=sys.stderr)
                sys.exit(2)
        except Exception as e:
            print(f"ERROR looking up {node}: {e}", file=sys.stderr)
            sys.exit(2)

    # Deterministic edge id (legal SurrealDB record id — backtick-escaped)
    edge_suffix = f"{cluster_node.replace(':', '_')}_{entity_node.replace(':', '_')}"

    # Check for existing edge by in/out fields (more robust than querying by ID)
    try:
        existing = sql(
            f"SELECT id FROM same_as "
            f"WHERE in = {cluster_node} AND out = {entity_node};",
            auth=auth)
        existing_rows = []
        if existing and isinstance(existing, list) and isinstance(existing[0], dict):
            r = existing[0].get("result", [])
            existing_rows = r if isinstance(r, list) else []
        if existing_rows:
            # Update reasoning/confidence on the existing edge
            existing_id = existing_rows[0].get("id")
            update_q = (
                f"UPDATE {existing_id} SET "
                f"signal = {jval(args.signal)}, "
                f"confidence = {args.confidence}, "
                f"reasoning = {jval(args.reasoning)};"
            )
            sql(update_q, auth=auth)
            print(f"UPDATED existing edge: {cluster_node} -> same_as -> {entity_node}")
            print(f"  edge id: {existing_id}")
            print(f"  signal:  {args.signal}")
            print(f"  confidence: {args.confidence}")
            return 0
    except Exception as e:
        print(f"(existence check skipped: {e})", file=sys.stderr)

    relate_q = (
        f"RELATE {cluster_node} -> same_as -> {entity_node} "
        f"SET id = same_as:`{edge_suffix}`, "
        f"signal = {jval(args.signal)}, "
        f"confidence = {args.confidence}, "
        f"reasoning = {jval(args.reasoning)};"
    )
    try:
        sql(relate_q, auth=auth)
    except Exception as e:
        print(f"ERROR writing edge: {e}", file=sys.stderr)
        sys.exit(3)

    print(f"CREATED edge: {cluster_node} -> same_as -> {entity_node}")
    print(f"  edge id: same_as:{edge_suffix}")
    print(f"  signal:  {args.signal}")
    print(f"  confidence: {args.confidence}")
    if args.reasoning:
        print(f"  reasoning: {args.reasoning}")
    print(f"\nNext: rebuild dossiers to populate {entity_node}'s appears_in/ folder:")
    print(f"  python3 .deepagents/skills/deep-research/scripts/build_entity_dossiers.py \"$RUN_DIR\"")
    return 0


if __name__ == "__main__":
    sys.exit(main())
