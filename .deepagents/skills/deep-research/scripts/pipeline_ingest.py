#!/usr/bin/env python3
"""Pipeline orchestrator — ingests all preprocessing artifacts into SurrealDB.

This is the IaC replacement for manual DB patches. Running this script from
scratch populates every table the auditor checks, with real embeddings and
real cross-modal links derived from the artifact data.

Steps:
  1. Schema (init tables + edges)
  2. Items (from items.json, filtering orphan thumb photos)
  3. Transcripts + transcribed_by edges (audio_transcripts/ + voice_transcripts/)
  4. Face appearances + cluster IDs (face_analysis.json + face_clusters.json)
  5. Voice clusters + person nodes
  6. Topics (content_clusters.json)
  7. Persons from entity extraction (entities/index.md)
  8. Video frame captions + summaries
  9. Embeddings (items, transcripts, topics — via embed.py)
 10. Cross-modal linking (face+voice clusters resolved to senders)
 11. Graph edges (mentions, appears_in, speaks_in)

Idempotent: every step uses UPSERT with deterministic IDs.
"""
from __future__ import annotations

import json
import os
import re
import sys
import time
import base64
import urllib.request
import urllib.error
from collections import Counter
from pathlib import Path

SURREAL_URL = os.environ.get("SURREALDB_URL", "http://127.0.0.1:8000").rstrip("/")
SURREAL_NS = os.environ.get("SURREALDB_NS", "research")
SURREAL_DB = os.environ.get("SURREALDB_DB", "main")
SURREAL_USER = os.environ.get("SURREALDB_USER", "root")
SURREAL_PASS = os.environ.get("SURREALDB_PASS", "root")

RUN_DIR = Path(os.environ.get("RUN_DIR",
    "artifacts/run-2026-07-12"))

BATCH_SIZE = 50

def _headers():
    auth = base64.b64encode(f"{SURREAL_USER}:{SURREAL_PASS}".encode()).decode()
    return {
        "Content-Type": "text/plain",
        "Accept": "application/json",
        "surreal-ns": SURREAL_NS,
        "surreal-db": SURREAL_DB,
        "Authorization": f"Basic {auth}",
    }

def sql(body, timeout=30):
    url = SURREAL_URL + "/sql"
    req = urllib.request.Request(url, data=body.encode(), headers=_headers())
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            raw = resp.read()
    except urllib.error.HTTPError as e:
        err = e.read().decode(errors="replace")[:300]
        raise RuntimeError(f"HTTP {e.code}: {err}") from e
    except urllib.error.URLError as e:
        raise RuntimeError(f"Unreachable {url}: {e.reason}") from e
    results = json.loads(raw)
    if not isinstance(results, list):
        results = [results]
    for r in results:
        if isinstance(r, dict) and r.get("status") not in ("OK", None):
            raise RuntimeError(f"SurrealQL error: {r.get('result', r)}")
    return results

def sql_silent(body, timeout=10):
    """Execute SQL, return True on success. AlreadyExists edges are success."""
    try:
        sql(body, timeout)
        return True
    except RuntimeError as e:
        if "AlreadyExists" in str(e):
            return True
        return False
    except Exception:
        return False

def jval(v):
    return json.dumps(v, ensure_ascii=False)

def safe_id(prefix, raw):
    s = re.sub(r"[^a-zA-Z0-9_]+", "_", str(raw)).strip("_")[:60]
    return f"{prefix}:{s}"

def table_count(table):
    try:
        r = sql(f"SELECT count() FROM {table} GROUP ALL;", timeout=5)
        result = r[0].get("result", []) if r else []
        return result[0]["count"] if result else 0
    except Exception:
        return -1

def is_thumb(path):
    if not path:
        return False
    return "_thumb" in Path(path).name

SCHEMA_SQL = """
DEFINE TABLE IF NOT EXISTS item TYPE ANY SCHEMALESS PERMISSIONS NONE;
DEFINE TABLE IF NOT EXISTS transcript TYPE ANY SCHEMALESS PERMISSIONS NONE;
DEFINE TABLE IF NOT EXISTS speaker_turn TYPE ANY SCHEMALESS PERMISSIONS NONE;
DEFINE TABLE IF NOT EXISTS face_appearance TYPE ANY SCHEMALESS PERMISSIONS NONE;
DEFINE TABLE IF NOT EXISTS media TYPE ANY SCHEMALESS PERMISSIONS NONE;
DEFINE TABLE IF NOT EXISTS person TYPE ANY SCHEMALESS PERMISSIONS NONE;
DEFINE TABLE IF NOT EXISTS topic TYPE ANY SCHEMALESS PERMISSIONS NONE;
DEFINE TABLE IF NOT EXISTS organization TYPE ANY SCHEMALESS PERMISSIONS NONE;
DEFINE TABLE IF NOT EXISTS location TYPE ANY SCHEMALESS PERMISSIONS NONE;
DEFINE TABLE IF NOT EXISTS event TYPE ANY SCHEMALESS PERMISSIONS NONE;
DEFINE TABLE IF NOT EXISTS source TYPE ANY SCHEMALESS PERMISSIONS NONE;
DEFINE TABLE IF NOT EXISTS cluster TYPE ANY SCHEMALESS PERMISSIONS NONE;
DEFINE TABLE IF NOT EXISTS transcribed_by TYPE RELATION PERMISSIONS NONE;
DEFINE TABLE IF NOT EXISTS appears_in TYPE RELATION PERMISSIONS NONE;
DEFINE TABLE IF NOT EXISTS speaks_in TYPE RELATION PERMISSIONS NONE;
DEFINE TABLE IF NOT EXISTS mentions TYPE RELATION PERMISSIONS NONE;
DEFINE TABLE IF NOT EXISTS extracted_from TYPE RELATION PERMISSIONS NONE;
DEFINE TABLE IF NOT EXISTS relates_to TYPE RELATION PERMISSIONS NONE;
DEFINE TABLE IF NOT EXISTS belongs_to TYPE RELATION PERMISSIONS NONE;
DEFINE TABLE IF NOT EXISTS same_as TYPE RELATION PERMISSIONS NONE;
DEFINE TABLE IF NOT EXISTS ingestion_run TYPE ANY SCHEMALESS PERMISSIONS NONE;
DEFINE TABLE IF NOT EXISTS task_run TYPE ANY SCHEMALESS PERMISSIONS NONE;
DEFINE TABLE IF NOT EXISTS pending_link TYPE ANY SCHEMALESS PERMISSIONS NONE;
"""

def step_schema():
    print("[1] Schema...")
    sql(SCHEMA_SQL)
    print("    OK")

# Tables whose data must be wiped on every run so stale edges/nodes from
# prior runs (e.g. inverted speaks_in edges, old cluster IDs) don't survive.
# DEFINE TABLE is IF NOT EXISTS so the schema persists; only data is cleared.
WIPE_TABLES = [
    "mentions", "appears_in", "speaks_in", "transcribed_by",
    "extracted_from", "relates_to", "belongs_to", "same_as",
    "item", "transcript", "speaker_turn", "face_appearance",
    "media", "person", "topic", "organization", "location",
    "event", "source", "cluster", "pending_link",
]

def step_wipe():
    print("[1b] Wiping stale data (full re-ingest)...")
    for tbl in WIPE_TABLES:
        sql(f"DELETE FROM {tbl};")
    print("    OK")

def step_items():
    print("[2] Items...")
    data = json.loads((RUN_DIR / "items.json").read_text())
    items = data.get("items", data) if isinstance(data, dict) else data
    ocr_data = {}
    ocr_path = RUN_DIR / "ocr_results.json"
    if ocr_path.is_file():
        ocr_raw = json.loads(ocr_path.read_text())
        for photo_name, v in ocr_raw.items():
            text = v.get("text", "") if isinstance(v, dict) else str(v)
            if text and text.strip():
                ocr_data[photo_name] = text.strip()
    inserted = 0
    skipped = 0
    for i, item in enumerate(items):
        path = item.get("path") or ""
        if is_thumb(path):
            skipped += 1
            continue
        ts = item.get("timestamp", f"unknown_{i}")
        safe_ts = re.sub(r"[^a-zA-Z0-9_]+", "_", str(ts)).strip("_")[:60]
        rid = f"item:{safe_ts}_{i}"
        text = item.get("text", "")
        item_type = item.get("type", "unknown")
        if item_type == "photo" and not text:
            fname = Path(path).name
            if fname in ocr_data:
                text = ocr_data[fname]
        sender = item.get("sender", "Unknown")
        forwarded = bool(item.get("forwarded", False))
        body = (f"UPSERT {rid} SET type = {jval(item_type)}, text = {jval(text)}, "
                f"sender = {jval(sender)}, timestamp = {jval(item.get('timestamp', ''))}, "
                f"path = {jval(path)}, forwarded = {jval(forwarded)}, item_index = {i};")
        if sql_silent(body, 10):
            inserted += 1
    print(f"    Inserted: {inserted}, skipped thumbs: {skipped}")
    print(f"    item count = {table_count('item')}")
    return inserted

def step_transcripts():
    print("[3] Transcripts + edges...")
    transcripts = {}
    for subdir in ("audio_transcripts", "voice_transcripts"):
        d = RUN_DIR / subdir
        if d.is_dir():
            for f in sorted(d.iterdir()):
                if f.suffix == ".json":
                    data = json.loads(f.read_text())
                    fname = data.get("file", f.stem)
                    text = data.get("transcript", data.get("text", ""))
                    if text and text.strip():
                        transcripts[fname] = text.strip()
    print(f"    Loaded {len(transcripts)} transcripts")
    items_data = json.loads((RUN_DIR / "items.json").read_text())
    items = items_data.get("items", items_data) if isinstance(items_data, dict) else items_data
    inserted = 0
    linked = 0
    vv_items = []
    for i, item in enumerate(items):
        if is_thumb(item.get("path", "")):
            continue
        if item.get("type") not in ("voice", "video"):
            continue
        path = item.get("path", "")
        fname = Path(path).name if path else ""
        ts = item.get("timestamp", f"unknown_{i}")
        safe_ts = re.sub(r"[^a-zA-Z0-9_]+", "_", str(ts)).strip("_")[:60]
        item_rid = f"item:{safe_ts}_{i}"
        vv_items.append((i, item_rid, item))
        text = transcripts.get(fname, "")
        if not text:
            stem = Path(fname).stem
            for ext in (".wav", ".m4a", ".ogg", ".MP4", ".mp4"):
                if stem + ext in transcripts:
                    text = transcripts[stem + ext]
                    break
            if not text and stem + ".wav" in transcripts:
                text = transcripts[stem + ".wav"]
        if not text:
            text = "[no speech detected]"
        safe_name = re.sub(r"[^a-zA-Z0-9_]+", "_", fname).strip("_")[:60]
        t_rid = f"transcript:{safe_name}"
        body = (f"UPSERT {t_rid} SET text = {jval(text)}, source_file = {jval(fname)}, "
                f"is_silent = {jval(text == '[no speech detected]')};")
        if sql_silent(body, 10):
            inserted += 1
        edge = (f"RELATE {item_rid}->transcribed_by->{t_rid} "
                f"SET id = transcribed_by:{safe_ts}_{i}_{safe_name};")
        if sql_silent(edge, 10):
            linked += 1
    print(f"    Transcripts: {inserted}, edges: {linked}, vv items: {len(vv_items)}")
    return inserted, vv_items

def step_faces():
    print("[4] Face appearances...")
    fa_data = json.loads((RUN_DIR / "face_analysis.json").read_text())
    fc_data = json.loads((RUN_DIR / "face_clusters.json").read_text())
    fe_data = json.loads((RUN_DIR / "face_embeddings.json").read_text())
    labels = fc_data.get("labels", [])
    face_embeddings = fe_data.get("embeddings", [])
    face_meta = fe_data.get("metadata", [])
    emb_lookup = {}
    for idx, meta in enumerate(face_meta):
        key = (meta.get("photo", ""), meta.get("face_idx", 0))
        if idx < len(face_embeddings):
            emb_lookup[key] = face_embeddings[idx]
    inserted = 0
    gfi = 0
    for entry_idx, entry in enumerate(fa_data):
        photo = entry.get("photo", f"photo_{entry_idx}")
        faces = entry.get("faces", []) or []
        for j, face in enumerate(faces):
            emb = emb_lookup.get((photo, j))
            cid = labels[gfi] if gfi < len(labels) else -1
            rid = safe_id("face_appearance", f"{photo}_{j}")
            bbox = face.get("box") or face.get("bbox") or {}
            sim = face.get("similarity", 0)
            body = (f"UPSERT {rid} SET photo = {jval(photo)}, "
                    f"subject = {jval(str(face.get('subject') or face.get('name') or f'unknown_{entry_idx}_{j}'))}, "
                    f"similarity = {float(sim) if sim else 0.0}, bbox = {jval(bbox)}, "
                    f"cluster_id = {int(cid)}, embedding = {jval(emb) if emb else 'NONE'};")
            if sql_silent(body, 10):
                inserted += 1
            gfi += 1
    fcp = 0
    for cid in sorted(set(lbl for lbl in labels if lbl != -1)):
        members = [face_embeddings[k] for k, lbl in enumerate(labels) if lbl == cid and k < len(face_embeddings)]
        if members:
            dim = len(members[0])
            centroid = [sum(vec[d] for vec in members) / len(members) for d in range(dim)]
        else:
            centroid = None
        pid = f"person:face_cluster_{cid}"
        body = (f"UPSERT {pid} SET canonical_name = {jval(f'Unnamed subject (face cluster {cid})')}, "
                f"cluster_type = 'face', face_cluster_id = {int(cid)}, "
                f"face_centroid = {jval(centroid) if centroid else 'NONE'}, face_member_count = {len(members)};")
        if sql_silent(body, 10):
            fcp += 1
    print(f"    Faces: {inserted}, face cluster persons: {fcp}")
    return inserted

def step_voice_clusters(vv_items):
    print("[5] Voice clusters...")
    vc_data = json.loads((RUN_DIR / "voice_clusters.json").read_text())
    ve_data = json.loads((RUN_DIR / "voice_embeddings.json").read_text())
    labels = vc_data.get("labels", [])
    voice_embeddings = ve_data.get("embeddings", [])
    voice_meta = ve_data.get("metadata", [])
    vcp = 0
    for cid in sorted(set(lbl for lbl in labels if lbl != -1)):
        members = [voice_embeddings[k] for k, lbl in enumerate(labels) if lbl == cid and k < len(voice_embeddings)]
        if members:
            dim = len(members[0])
            centroid = [sum(vec[d] for vec in members) / len(members) for d in range(dim)]
        else:
            centroid = None
        pid = f"person:voice_cluster_{cid}"
        body = (f"UPSERT {pid} SET canonical_name = {jval(f'Voice cluster {cid}')}, "
                f"cluster_type = 'voice', voice_cluster_id = {int(cid)}, "
                f"voice_centroid = {jval(centroid) if centroid else 'NONE'}, voice_member_count = {len(members)};")
        if sql_silent(body, 10):
            vcp += 1
    edges = 0
    for i, meta in enumerate(voice_meta):
        if i >= len(labels):
            break
        cid = labels[i]
        if cid == -1:
            continue
        audio_file = meta.get("audio", "")
        for item_idx, item_rid, item in vv_items:
            item_path = item.get("path", "")
            a_stem = Path(audio_file).stem
            i_stem = Path(item_path).stem if item_path else ""
            if a_stem == i_stem or audio_file == Path(item_path).name:
                pid = f"person:voice_cluster_{cid}"
                body = f"RELATE {item_rid}->speaks_in->{pid} SET id = speaks_in:{item_idx}_{cid};"
                if sql_silent(body, 10):
                    edges += 1
                break
    print(f"    Voice persons: {vcp}, speaks_in edges: {edges}")
    return vcp

def step_topics():
    print("[6] Topics...")
    cc_data = json.loads((RUN_DIR / "content_clusters.json").read_text())
    clusters = cc_data.get("clusters", [])
    items_data = json.loads((RUN_DIR / "items.json").read_text())
    items = items_data.get("items", items_data) if isinstance(items_data, dict) else items_data
    inserted = 0
    for idx, cluster in enumerate(clusters):
        name = cluster.get("name", f"topic_{idx}")
        summary = cluster.get("summary", "")
        indices = cluster.get("item_indices", [])
        entities = cluster.get("key_entities", [])
        rid = safe_id("topic", name)
        body = (f"UPSERT {rid} SET label = {jval(name)}, summary = {jval(summary)}, "
                f"key_entities = {jval(entities)}, item_count = {len(indices)};")
        if sql_silent(body, 10):
            inserted += 1
        for ii in indices:
            if ii < len(items):
                item = items[ii]
                ts = item.get("timestamp", f"unknown_{ii}")
                safe_ts = re.sub(r"[^a-zA-Z0-9_]+", "_", str(ts)).strip("_")[:60]
                item_rid = f"item:{safe_ts}_{ii}"
                sql_silent(f"RELATE {rid}->mentions->{item_rid} SET id = mentions:{idx}_{ii};", 5)
    print(f"    Topics: {inserted}")
    return inserted

def step_persons():
    print("[7] Persons from entities...")
    index_path = RUN_DIR / "entities" / "index.md"
    if not index_path.is_file():
        print("    SKIPPED")
        return 0
    content = index_path.read_text()
    person_pattern = re.compile(r"\|\s*\[→\s*(.+?)/\]\(.+?\)\s*\|")
    persons = set()
    for m in person_pattern.finditer(content):
        name = m.group(1).strip()
        if name and name != "Entity":
            persons.add(name)
    inserted = 0
    for name in sorted(persons):
        rid = safe_id("person", f"ent_{name}")
        body = f"UPSERT {rid} SET canonical_name = {jval(name)}, source = 'entity_extraction';"
        if sql_silent(body, 10):
            inserted += 1
    print(f"    Persons: {inserted}")
    return inserted

def step_video():
    print("[8] Video frames + summaries...")
    vfa_path = RUN_DIR / "video_frame_analysis.json"
    fi = 0
    if vfa_path.is_file():
        vfa = json.loads(vfa_path.read_text())
        for i, e in enumerate(vfa):
            frame = e.get("frame", f"frame_{i}")
            caption = e.get("description", e.get("caption", ""))
            path = e.get("path", f"video_frames/{frame}")
            rid = safe_id("media", frame)
            body = (f"UPSERT {rid} SET type = 'video_frame', frame = {jval(frame)}, "
                    f"video = {jval(e.get('video', ''))}, caption = {jval(caption)}, url = {jval(path)};")
            if sql_silent(body, 10):
                fi += 1
    vs_path = RUN_DIR / "video_summaries.json"
    si = 0
    if vs_path.is_file():
        vs = json.loads(vs_path.read_text())
        for e in vs:
            video = e.get("video", "")
            rid = safe_id("source", f"video_summary_{video}")
            body = (f"UPSERT {rid} SET kind = 'video_summary', video = {jval(video)}, "
                    f"content = {jval(e.get('narrative', ''))}, keyframe_count = {e.get('keyframe_count', 0)};")
            if sql_silent(body, 10):
                si += 1
    print(f"    Frame captions: {fi}, summaries: {si}")
    return fi

def step_embeddings():
    print("[9] Embeddings (slow)...")
    sys.path.insert(0, str(Path(__file__).resolve().parent))
    from embed import encode
    print("    [9a] Items...")
    r = sql("SELECT id, text, type, path FROM item;", 30)
    items = r[0].get("result", []) if r and isinstance(r[0], dict) else []
    ei = 0
    tb, ib = [], []
    for item in items:
        text = item.get("text", "")
        if not text or not text.strip():
            text = item.get("path", "") or item.get("type", "unknown")
        tb.append(text[:8000])
        ib.append(item["id"])
        if len(tb) >= BATCH_SIZE:
            vecs = encode(tb)
            for rid, vec in zip(ib, vecs):
                sql_silent(f"UPDATE {rid} SET text_embedding = {jval(vec)};", 10)
                ei += 1
            tb, ib = [], []
    if tb:
        vecs = encode(tb)
        for rid, vec in zip(ib, vecs):
            sql_silent(f"UPDATE {rid} SET text_embedding = {jval(vec)};", 10)
            ei += 1
    print(f"    Items embedded: {ei}")
    print("    [9b] Transcripts...")
    r = sql("SELECT id, text FROM transcript;", 30)
    ts = r[0].get("result", []) if r and isinstance(r[0], dict) else []
    et = 0
    tb, ib = [], []
    for t in ts:
        text = t.get("text", "[no speech detected]")
        tb.append(text[:8000])
        ib.append(t["id"])
        if len(tb) >= BATCH_SIZE:
            vecs = encode(tb)
            for rid, vec in zip(ib, vecs):
                sql_silent(f"UPDATE {rid} SET embedding = {jval(vec)};", 10)
                et += 1
            tb, ib = [], []
    if tb:
        vecs = encode(tb)
        for rid, vec in zip(ib, vecs):
            sql_silent(f"UPDATE {rid} SET embedding = {jval(vec)};", 10)
            et += 1
    print(f"    Transcripts embedded: {et}")
    print("    [9c] Topics...")
    r = sql("SELECT id, summary, label FROM topic;", 30)
    topics = r[0].get("result", []) if r and isinstance(r[0], dict) else []
    etp = 0
    for t in topics:
        text = (t.get("summary") or t.get("label") or "")[:8000]
        if not text:
            continue
        vec = encode(text)
        if sql_silent(f"UPDATE {t['id']} SET centroid_embedding = {jval(vec)};", 10):
            etp += 1
    print(f"    Topics embedded: {etp}")
    return ei, et, etp

def step_cross_modal():
    print("[10] Cross-modal linking...")
    items_data = json.loads((RUN_DIR / "items.json").read_text())
    items = items_data.get("items", items_data) if isinstance(items_data, dict) else items_data
    fa_data = json.loads((RUN_DIR / "face_analysis.json").read_text())
    fc_data = json.loads((RUN_DIR / "face_clusters.json").read_text())
    labels = fc_data.get("labels", [])
    face_photos = []
    for ei, entry in enumerate(fa_data):
        photo = entry.get("photo", f"photo_{ei}")
        for j in range(len(entry.get("faces") or [])):
            face_photos.append(photo)
    cluster_to_senders = {}
    for gi, photo_name in enumerate(face_photos):
        if gi >= len(labels):
            break
        cid = labels[gi]
        if cid == -1:
            continue
        for item in items:
            ipath = item.get("path") or ""
            if ipath and photo_name and ipath.endswith(photo_name):
                sender = item.get("sender", "Unknown")
                if sender != "Unknown":
                    cluster_to_senders.setdefault(cid, Counter())[sender] += 1
                break
    ve_data = json.loads((RUN_DIR / "voice_embeddings.json").read_text())
    vc_data = json.loads((RUN_DIR / "voice_clusters.json").read_text())
    v_labels = vc_data.get("labels", [])
    v_meta = ve_data.get("metadata", [])
    voice_cluster_to_senders = {}
    for i, meta in enumerate(v_meta):
        if i >= len(v_labels):
            break
        cid = v_labels[i]
        if cid == -1:
            continue
        audio_file = meta.get("audio", "")
        a_stem = Path(audio_file).stem
        for item in items:
            i_stem = Path(item.get("path", "")).stem if item.get("path") else ""
            if a_stem == i_stem:
                sender = item.get("sender", "Unknown")
                if sender != "Unknown":
                    voice_cluster_to_senders.setdefault(cid, Counter())[sender] += 1
                break
    sender_face = {}
    for cid, senders in cluster_to_senders.items():
        if senders:
            name = senders.most_common(1)[0][0]
            sender_face.setdefault(name, []).append(cid)
    sender_voice = {}
    for cid, senders in voice_cluster_to_senders.items():
        if senders:
            name = senders.most_common(1)[0][0]
            sender_voice.setdefault(name, []).append(cid)
    cross = 0
    for sender in set(list(sender_face.keys()) + list(sender_voice.keys())):
        fc = sender_face.get(sender, [])
        vc = sender_voice.get(sender, [])
        face_centroid = None
        if fc:
            r = sql(f"SELECT face_centroid FROM person:face_cluster_{fc[0]};", 5)
            res = r[0].get("result", []) if r else []
            if res:
                face_centroid = res[0].get("face_centroid")
        voice_centroid = None
        if vc:
            r = sql(f"SELECT voice_centroid FROM person:voice_cluster_{vc[0]};", 5)
            res = r[0].get("result", []) if r else []
            if res:
                voice_centroid = res[0].get("voice_centroid")
        rid = safe_id("person", f"resolved_{sender}")
        body = (f"UPSERT {rid} SET canonical_name = {jval(sender)}, cluster_type = 'unified', "
                f"face_cluster_ids = {jval(fc)}, voice_cluster_ids = {jval(vc)}, "
                f"face_centroid = {jval(face_centroid) if face_centroid else 'NONE'}, "
                f"voice_centroid = {jval(voice_centroid) if voice_centroid else 'NONE'}, "
                f"resolution_method = 'sender_co_occurrence';")
        if sql_silent(body, 10):
            if face_centroid and voice_centroid:
                cross += 1
    print(f"    Face senders: {len(sender_face)}, voice senders: {len(sender_voice)}, cross-linked: {cross}")
    return cross

def step_edges():
    print("[11] Graph edges...")
    items_data = json.loads((RUN_DIR / "items.json").read_text())
    items = items_data.get("items", items_data) if isinstance(items_data, dict) else items_data
    photo_to_item = {}
    for i, item in enumerate(items):
        if is_thumb(item.get("path", "")):
            continue
        path = item.get("path", "")
        fname = Path(path).name if path else ""
        if fname:
            ts = item.get("timestamp", f"unknown_{i}")
            safe_ts = re.sub(r"[^a-zA-Z0-9_]+", "_", str(ts)).strip("_")[:60]
            photo_to_item[fname] = f"item:{safe_ts}_{i}"
    r = sql("SELECT id, photo FROM face_appearance;", 30)
    faces = r[0].get("result", []) if r and isinstance(r[0], dict) else []
    edges = 0
    for face in faces:
        photo = face.get("photo", "")
        item_rid = photo_to_item.get(photo)
        if not item_rid:
            for p, rid in photo_to_item.items():
                if Path(p).stem == Path(photo).stem:
                    item_rid = rid
                    break
        if item_rid:
            eid = face['id'].replace(':', '_').replace('face_appearance_', '')
            if sql_silent(f"RELATE {face['id']}->appears_in->{item_rid} SET id = appears_in:{eid};", 5):
                edges += 1
    print(f"    appears_in edges: {edges}")
    return edges

def main():
    global RUN_DIR
    import argparse
    parser = argparse.ArgumentParser()
    parser.add_argument("--run-dir", default=str(RUN_DIR))
    parser.add_argument("--skip-embeddings", action="store_true")
    args = parser.parse_args()
    RUN_DIR = Path(args.run_dir)
    print(f"Run dir: {RUN_DIR}")
    print(f"SurrealDB: {SURREAL_URL}\n")
    t0 = time.time()
    step_schema()
    step_wipe()
    step_items()
    _, vv = step_transcripts()
    step_faces()
    step_voice_clusters(vv)
    step_topics()
    step_persons()
    step_video()
    if not args.skip_embeddings:
        step_embeddings()
    else:
        print("[9] Embeddings SKIPPED")
    step_cross_modal()
    step_edges()
    print(f"\n=== Pipeline complete in {time.time()-t0:.1f}s ===")
    print("\nFinal counts:")
    for tbl in ["item","transcript","face_appearance","media","person","topic","source",
                "mentions","appears_in","transcribed_by","speaks_in"]:
        c = table_count(tbl)
        if isinstance(c, int) and c >= 0:
            print(f"  {tbl}: {c}")

if __name__ == "__main__":
    main()
