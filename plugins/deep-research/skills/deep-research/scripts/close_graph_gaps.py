#!/usr/bin/env python3
"""Close ALL graph gaps — read artifacts, build the real knowledge graph.

Gaps this closes:
  1. face_appearance.item_id is NULL  → set from face_analysis photo filenames
  2. face_appearance.cluster_id missing → write back from face_clusters labels
  3. 0 person nodes from clusters → UPSERT person:face_cluster_N
  4. 0 appears_in edges → RELATE person:cluster -> appears_in -> item
  5. 0 extracted_from edges → run entity_extract, upsert, link to source
  6. 0 mentions edges → topic -> mentions -> item (keyword match)
  7. sender -> authored edges on items

Reads from the run-dir artifacts. Writes via SurrealQL. Idempotent (UPSERT).
"""
import json
import os
import re
import sys
import urllib.request
from pathlib import Path


def _workspace_root():
    """Layout-robust workspace root: env override, else nearest .git ancestor,
    else cwd (works in-repo under plugins/, in dcode's plugin cache, anywhere)."""
    for var in ("WORKSPACE_ROOT", "CLAUDE_PROJECT_DIR"):
        v = os.environ.get(var)
        if v:
            return Path(v).expanduser()
    for anc in Path(__file__).resolve().parents:
        if (anc / ".git").exists():
            return anc
    return Path.cwd()


URL = os.environ.get("SURREALDB_URL", "http://127.0.0.1:8000/sql").replace("/mcp", "/sql")
NS = os.environ.get("SURREALDB_NS", "research")
DB = os.environ.get("SURREALDB_DB", "main")
AUTH = os.environ.get("SURREALDB_AUTH", "Basic cm9vdDpyb290")  # base64(root:root)


def sql(body, timeout=30):
    req = urllib.request.Request(
        URL, data=body.encode(),
        headers={"Accept": "application/json", "surreal-ns": NS,
                 "surreal-db": DB, "Authorization": AUTH},
    )
    with urllib.request.urlopen(req, timeout=timeout) as r:
        return json.loads(r.read())


def jval(v):
    """SurrealQL-safe JSON literal (ensure_ascii=False: emoji/unicode kept
    as UTF-8 — SurrealDB rejects surrogate pair escapes like 😂)."""
    return json.dumps(v, ensure_ascii=False)


# ---------------------------------------------------------------------------
def gap1_2_face_itemid_clusterid(run_dir):
    """Write item_id + cluster_id onto every face_appearance row.

    face_analysis.json: list of {photo, n_faces, faces[], embeddings[]}.
    face_clusters.json: {labels: [int, ...]} in the SAME ORDER as the
    concatenated embeddings from face_analysis (photos in order, faces
    within photo in order).
    """
    fa = json.loads((run_dir / "face_analysis.json").read_text())
    fc = json.loads((run_dir / "face_clusters.json").read_text())
    labels = fc["labels"]

    # Build the ordered list of (photo_filename, face_idx, embedding) by
    # walking face_analysis and expanding every photo that had faces.
    ordered = []
    for entry in fa:
        embs = entry.get("embeddings") or []
        if not embs:
            continue
        for idx in range(len(embs)):
            ordered.append((entry["photo"], idx))

    if len(ordered) != len(labels):
        print(f"  WARN: {len(ordered)} faces vs {len(labels)} labels — aligning by min")

    # Get current face_appearance ids in order (they were inserted in the same
    # order as face_analysis expansion). Query them.
    res = sql("SELECT id, item_id, cluster_id FROM face_appearance ORDER BY id;")
    rows = res[0]["result"] if res else []

    updated = 0
    for i, row in enumerate(rows[:len(ordered)]):
        photo, face_idx = ordered[i]
        cluster = labels[i] if i < len(labels) else -1
        # Find the item_id for this photo filename
        # photo is like "photo_100@13-03-2026_08-59-45.jpg"; path in DB like "photos/photo_100@..."
        lookup = sql(
            f"SELECT id FROM item WHERE path CONTAINS {jval(photo)} LIMIT 1;"
        )
        items = lookup[0]["result"] if lookup else []
        item_id = items[0]["id"] if items else None
        if item_id:
            sql(
                f"UPDATE {row['id']} SET item_id = {jval(str(item_id))}, "
                f"cluster_id = {cluster}, source_file = {jval(photo)};"
            )
            updated += 1
        else:
            sql(f"UPDATE {row['id']} SET cluster_id = {cluster}, source_file = {jval(photo)};")
    print(f"  gap 1+2: updated {updated}/{len(rows)} face_appearance rows with item_id + cluster_id")
    return updated


# ---------------------------------------------------------------------------
def gap3_4_person_nodes_and_appears_in():
    """Create person:face_cluster_N nodes + appears_in edges."""
    # Distinct non-noise clusters
    res = sql("SELECT cluster_id, count() AS n FROM face_appearance "
              "WHERE cluster_id != -1 GROUP BY cluster_id ORDER BY cluster_id;")
    clusters = res[0]["result"] if res else []
    persons = 0
    edges = 0
    for c in clusters:
        cid = c["cluster_id"]
        n = c["n"]
        # Upsert a person node for this cluster
        sql(f"UPSERT person:face_cluster_{cid} SET "
            f"canonical_name = {jval(f'Identity-{cid}')}, "
            f"role = 'subject', "
            f"face_cluster_id = {cid}, "
            f"face_count = {n}, "
            f"notes = {jval('Derived from HDBSCAN face clustering of photo corpus.')};")
        persons += 1
        # Query the distinct item_ids for this cluster, then RELATE each.
        # NOTE: SurrealDB has no `SELECT DISTINCT col` — use GROUP BY.
        items_res = sql(
            f"SELECT item_id FROM face_appearance "
            f"WHERE cluster_id = {cid} AND item_id != NONE GROUP BY item_id;"
        )
        item_ids = [r["item_id"] for r in (items_res[0]["result"] if items_res else []) if r.get("item_id")]
        for iid in item_ids:
            iid_safe = str(iid).replace(":", "_")
            try:
                sql(f"RELATE person:face_cluster_{cid} -> appears_in -> {iid} "
                    f"SET id = appears_in:fc{cid}_{iid_safe};")
                edges += 1
            except Exception:
                pass
    # Count actual edges created
    cnt = sql("RETURN count(SELECT id FROM appears_in);")
    actual = cnt[0]["result"] if cnt else 0
    print(f"  gap 3+4: {persons} person nodes upserted, appears_in edges = {actual}")
    return actual


# ---------------------------------------------------------------------------
def gap5_extracted_from_edges():
    """Run entity_extract over audio summaries + items, upsert, link to source."""
    try:
        sys.path.insert(0, str(Path(__file__).parent))
        from entity_extract import extract_entities
    except Exception as e:
        print(f"  gap 5: entity_extract unavailable ({e}) — skipping")
        return 0

    run_dir = Path(os.environ.get("RUN_DIR",
        str(_workspace_root() / "artifacts" / "run-2026-07-12")))
    asum_path = run_dir / "audio_summaries.json"
    if not asum_path.exists():
        print("  gap 5: no audio_summaries.json — skipping")
        return 0

    summaries = json.loads(asum_path.read_text())
    # Upsert a source record for the intelligence analysis
    sql(f"UPSERT source:audio_intel SET title = {jval('Audio Intelligence Summaries')}, "
        f"url = '', author = 'DRE-audio-analyst', "
        f"accessed_at = '2026-07-13';")

    all_people, all_orgs, all_topics, all_locs = set(), set(), set(), set()
    for s in summaries:
        text = s.get("summary", "")[:8000]
        ents = extract_entities(text)
        all_people.update(ents.get("people", []))
        all_orgs.update(ents.get("organizations", []))
        all_topics.update(ents.get("topics", []))
        all_locs.update(ents.get("locations", []))

    def _safe(name):
        return re.sub(r"[^a-z0-9]+", "_", name.lower()).strip("_")[:50]

    linked = 0
    for p in all_people:
        if not p or len(p) < 3:
            continue
        sid = _safe(p)
        if not sid:
            continue
        sql(f"UPSERT person:ent_{sid} SET canonical_name = {jval(p)}, "
            f"role = 'mentioned', notes = 'Extracted from audio intel summaries.';")
        sql(f"RELATE source:audio_intel -> extracted_from -> person:ent_{sid} "
            f"SET id = extracted_from:person_ent_{sid};")
        linked += 1
    for o in all_orgs:
        if not o or len(o) < 3:
            continue
        sid = _safe(o)
        if not sid:
            continue
        sql(f"UPSERT organization:ent_{sid} SET name = {jval(o)};")
        sql(f"RELATE source:audio_intel -> extracted_from -> organization:ent_{sid} "
            f"SET id = extracted_from:org_ent_{sid};")
        linked += 1
    for t in all_topics:
        if not t or len(t) < 3:
            continue
        sid = _safe(t)
        if not sid:
            continue
        sql(f"UPSERT topic:ent_{sid} SET name = {jval(t)}, "
            f"summary = 'Extracted from audio intel summaries.';")
        sql(f"RELATE source:audio_intel -> extracted_from -> topic:ent_{sid} "
            f"SET id = extracted_from:topic_ent_{sid};")
        linked += 1
    for l in all_locs:
        if not l or len(l) < 3:
            continue
        sid = _safe(l)
        if not sid:
            continue
        sql(f"UPSERT location:ent_{sid} SET name = {jval(l)};")
        sql(f"RELATE source:audio_intel -> extracted_from -> location:ent_{sid} "
            f"SET id = extracted_from:location_ent_{sid};")
        linked += 1
    print(f"  gap 5: extracted people={len(all_people)} orgs={len(all_orgs)} "
          f"topics={len(all_topics)} locs={len(all_locs)}; {linked} edges created")
    return linked


# ---------------------------------------------------------------------------
def gap6_mentions_edges():
    """topic -> mentions -> item (keyword overlap)."""
    topics_res = sql("SELECT id, name FROM topic;")
    topics = topics_res[0]["result"] if topics_res and isinstance(topics_res[0].get("result"), list) else []
    total = 0
    for t in topics:
        if not isinstance(t, dict):
            continue
        tid = t.get("id")
        name = t.get("name", "")
        if not tid or not name or len(name) < 4:
            continue
        # Find items whose text contains the topic name
        try:
            matches = sql(
                f"SELECT id FROM item WHERE text != NONE AND string::lowercase(text) "
                f"CONTAINS {jval(name.lower())} LIMIT 50;"
            )
        except Exception:
            continue
        items = matches[0]["result"] if matches and isinstance(matches[0].get("result"), list) else []
        for it in items:
            if not isinstance(it, dict) or not it.get("id"):
                continue
            tid_safe = str(tid).replace(":", "_")
            iid_safe = str(it["id"]).replace(":", "_")
            try:
                sql(f"RELATE {tid} -> mentions -> {it['id']} "
                    f"SET id = mentions:{tid_safe}_{iid_safe};")
                total += 1
            except Exception:
                pass
    cnt = sql("RETURN count(SELECT id FROM mentions);")
    actual = cnt[0]["result"] if cnt else 0
    print(f"  gap 6: mentions edges = {actual}")
    return actual


# ---------------------------------------------------------------------------
def gap7_sender_authored_edges():
    """Link items to their senders via authored edges."""
    # For each distinct sender, upsert a person and relate
    res = sql("SELECT sender, count() AS n FROM item GROUP BY sender;")
    senders = res[0]["result"] if res else []
    total = 0
    for s in senders:
        name = s.get("sender")
        if not name or name == "Unknown":
            continue
        sid = re.sub(r"[^a-z0-9]+", "_", name.lower()).strip("_")[:50]
        if not sid:
            continue
        sql(f"UPSERT person:sender_{sid} SET canonical_name = {jval(name)}, "
            f"role = 'author', item_count = {s['n']};")
        # Query this sender's items, then RELATE each (SurrealQL has no
        # WHERE-filtered RELATE target).
        items_res = sql(f"SELECT id FROM item WHERE sender = {jval(name)};")
        for it in (items_res[0]["result"] if items_res else []):
            iid_safe = str(it["id"]).replace(":", "_")
            try:
                sql(f"RELATE person:sender_{sid} -> authored -> {it['id']} "
                    f"SET id = authored:sender_{sid}_{iid_safe};")
                total += 1
            except Exception:
                pass
    cnt = sql("RETURN count(SELECT id FROM authored);")
    actual = cnt[0]["result"] if cnt else 0
    print(f"  gap 7: sender nodes upserted, authored edges = {actual}")
    return actual


# ---------------------------------------------------------------------------
def main():
    run_dir = Path(os.environ.get("RUN_DIR",
        str(_workspace_root() / "artifacts" / "run-2026-07-12")))
    if len(sys.argv) > 1:
        run_dir = Path(sys.argv[1])
    print(f"=== close_graph_gaps: {run_dir} ===")
    print()
    print("[gap 1+2] face_appearance item_id + cluster_id writeback")
    gap1_2_face_itemid_clusterid(run_dir)
    print()
    print("[gap 3+4] person nodes from clusters + appears_in edges")
    gap3_4_person_nodes_and_appears_in()
    print()
    print("[gap 5] entity extraction + extracted_from edges")
    gap5_extracted_from_edges()
    print()
    print("[gap 6] topic -> mentions -> item edges")
    gap6_mentions_edges()
    print()
    print("[gap 7] sender -> authored -> item edges")
    gap7_sender_authored_edges()
    print()
    print("=== DONE — final counts ===")
    cnt = sql(
        "RETURN count(SELECT id FROM item);"
        "RETURN count(SELECT id FROM person);"
        "RETURN count(SELECT id FROM topic);"
        "RETURN count(SELECT id FROM organization);"
        "RETURN count(SELECT id FROM location);"
        "RETURN count(SELECT id FROM face_appearance);"
        "RETURN count(SELECT id FROM appears_in);"
        "RETURN count(SELECT id FROM extracted_from);"
        "RETURN count(SELECT id FROM mentions);"
        "RETURN count(SELECT id FROM authored);"
        "RETURN count(SELECT id FROM face_appearance WHERE item_id != NONE);"
        "RETURN count(SELECT id FROM face_appearance WHERE cluster_id != -1);"
    )
    labels = ["item", "person", "topic", "organization", "location",
              "face_appearance", "appears_in", "extracted_from", "mentions",
              "authored", "fa_with_item_id", "fa_with_cluster"]
    for l, r in zip(labels, cnt):
        print(f"  {l:22s} {r['result']:>6}")


if __name__ == "__main__":
    main()
