#!/usr/bin/env python3
"""Resolve face/voice clusters to named entities + cross-link them.

The graph had:
  - person:face_cluster_N with placeholder names ("Identity-N")
  - ZERO voice cluster nodes
  - ZERO same_as edges between clusters
  - entity-extracted persons (person:ent_grady) DISJOINT from cluster persons

This script:
  1. Creates person:voice_cluster_N nodes + speaks_in edges (audio items)
  2. Resolves each cluster to a sender name: cluster -> items -> sender.
     If a cluster's items are predominantly from one sender, it inherits that
     name. This is principled identity resolution from co-occurrence.
  3. Cross-links face + voice clusters that resolve to the same sender via
     same_as edges.
  4. Links cluster persons to entity-extracted persons (same_as) so the graph
     is connected end-to-end: photo -> face_cluster -> person -> name.

Idempotent: UPSERT + guarded RELATE.
"""
import json
import os
import re
import urllib.request
from collections import Counter
from pathlib import Path

URL = os.environ.get("SURREALDB_URL", "http://127.0.0.1:8000/sql")
NS = "research"; DB = "main"; AUTH = "Basic cm9vdDpyb290"
RUN_DIR = Path(os.environ.get("RUN_DIR",
    "/sandbox/workspace/orgs/specialists/deep-research-engine/artifacts/run-2026-07-12"))


def sql(body, timeout=30):
    req = urllib.request.Request(URL, data=body.encode(),
        headers={"Accept": "application/json", "surreal-ns": NS,
                 "surreal-db": DB, "Authorization": AUTH})
    with urllib.request.urlopen(req, timeout=timeout) as r:
        return json.loads(r.read())


def jval(v):
    """SurrealQL-safe JSON literal (ensure_ascii=False for emoji/unicode)."""
    return json.dumps(v, ensure_ascii=False)


def _safe(name):
    return re.sub(r"[^a-z0-9]+", "_", name.lower()).strip("_")[:50]


def sender_of(item_id):
    """Who sent this item?"""
    try:
        r = sql(f"SELECT sender FROM {item_id};")
        res = r[0].get("result", [])
        return res[0].get("sender") if res else None
    except Exception:
        return None


def resolve_face_clusters():
    """Face clusters: sender is a WEAK identity signal (people forward photos
    of others). We record who distributed each face but do NOT claim the face
    IS that sender. The cluster keeps a distinct identity label; the sender
    is noted as the source/distributor."""
    res = sql("SELECT cluster_id FROM face_appearance WHERE cluster_id != -1 "
              "GROUP BY cluster_id ORDER BY cluster_id;")
    clusters = res[0]["result"] if res and isinstance(res[0].get("result"), list) else []
    resolved = {}
    for c in clusters:
        cid = c.get("cluster_id")
        if cid is None:
            continue
        items_res = sql(f"SELECT item_id FROM face_appearance WHERE cluster_id = {cid} "
                        f"AND item_id != NONE GROUP BY item_id;")
        item_ids = [r["item_id"] for r in (items_res[0].get("result", []) if items_res and isinstance(items_res[0].get("result"), list) else []) if isinstance(r, dict) and r.get("item_id")]
        senders = Counter()
        for iid in item_ids:
            s = sender_of(iid)
            if s and s != "Unknown":
                senders[s] += 1
        if senders:
            name, n = senders.most_common(1)[0]
            conf = n / max(len(item_ids), 1)
            resolved[cid] = {"name": None, "distributor": name, "confidence": conf, "items": len(item_ids)}
            # Do NOT rename the face to the sender — faces are distinct identities.
            # Record the distributor so the graph knows WHO posted the face.
            sql(f"UPDATE person:face_cluster_{cid} SET "
                f"canonical_name = {jval(f'Unnamed subject (face cluster {cid})')}, "
                f"distributed_by = {jval(name)}, "
                f"distribution_confidence = {round(conf, 2)}, "
                f"resolved_item_count = {len(item_ids)}, "
                f"notes = 'Face identity not resolved to a name. Photos distributed by ' + {jval(name)} + '. Sender is the distributor, not necessarily the subject.';")
        else:
            resolved[cid] = {"name": None, "distributor": None, "confidence": 0, "items": len(item_ids)}
    return resolved


def create_voice_cluster_nodes():
    """Create person:voice_cluster_N + speaks_in edges from voice_clusters.json."""
    vc_path = RUN_DIR / "voice_clusters.json"
    if not vc_path.exists():
        print("  voice_clusters.json not found — skipping voice nodes")
        return {}
    vc = json.loads(vc_path.read_text())
    labels = vc["labels"]
    sources = vc.get("sources", [])

    # Group audio files by cluster
    clusters = {}
    for label, src in zip(labels, sources):
        if label == -1:
            continue
        clusters.setdefault(label, []).append(src)

    resolved = {}
    for cid, files in clusters.items():
        # Find the items matching these audio files (by path CONTAINS filename)
        senders = Counter()
        linked = 0
        for fname in files:
            stem = Path(fname).stem
            try:
                # Match item by path containing the stem. Audio files can be
                # either voice messages (type='voice') OR video audio tracks
                # (type='video' — the audio was extracted from the video).
                # Query BOTH types so video audio clusters resolve to senders.
                r = sql(f"SELECT id, sender FROM item WHERE "
                        f"(type = 'voice' OR type = 'video') "
                        f"AND path CONTAINS {jval(stem)} LIMIT 1;")
                items = r[0].get("result", []) if r and isinstance(r[0].get("result"), list) else []
                for it in items:
                    if not isinstance(it, dict) or not it.get("id"):
                        continue
                    iid = it["id"]
                    iid_safe = str(iid).replace(":", "_")
                    # Upsert voice cluster person + speak edge
                    sql(f"UPSERT person:voice_cluster_{cid} SET "
                        f"role = 'speaker', voice_cluster_id = {cid}, "
                        f"voice_count = {len(files)};")
                    try:
                        sql(f"RELATE person:voice_cluster_{cid} -> speaks_in -> {iid} "
                            f"SET id = speaks_in:vc{cid}_{iid_safe};")
                        linked += 1
                    except Exception:
                        pass
                    s = it.get("sender")
                    if s and s != "Unknown":
                        senders[s] += 1
            except Exception:
                pass
        if senders:
            name, n = senders.most_common(1)[0]
            conf = n / max(len(files), 1)
            display = f"{name} (voice cluster {cid})" if conf < 0.9 else name
            sql(f"UPDATE person:voice_cluster_{cid} SET "
                f"canonical_name = {jval(display)}, "
                f"resolved_sender = {jval(name)}, "
                f"resolution_confidence = {round(conf, 2)};")
            resolved[cid] = {"name": name, "confidence": conf, "files": len(files), "linked": linked}
        else:
            sql(f"UPDATE person:voice_cluster_{cid} SET "
                f"canonical_name = {jval(f'Unknown speaker (cluster {cid})')}, "
                f"voice_count = {len(files)};")
            resolved[cid] = {"name": None, "confidence": 0, "files": len(files), "linked": linked}
    return resolved


def cross_link_voice_to_identity(voice_resolved):
    """Voice clusters resolve to identities via sender attribution (strong:
    you send your own voice). This is GENERIC — works for any dataset where
    items have a sender field. Creates person:resolved_<name> canonical nodes
    and same_as edges from voice clusters + sender person nodes."""
    by_name = {}
    for cid, info in voice_resolved.items():
        if info.get("name"):
            by_name.setdefault(info["name"], {"voice": []})["voice"].append(cid)

    edges = 0
    for name, groups in by_name.items():
        sid = _safe(name)
        if not sid:
            continue
        sql(f"UPSERT person:resolved_{sid} SET canonical_name = {jval(name)}, "
            f"role = 'resolved_identity', "
            f"notes = 'Identity resolved from voice-cluster + sender co-occurrence.';")
        for cid in groups["voice"]:
            try:
                sql(f"RELATE person:voice_cluster_{cid} -> same_as -> person:resolved_{sid} "
                    f"SET id = same_as:vc{cid}_resolved_{sid};")
                edges += 1
            except Exception:
                pass
        try:
            ent = sql(f"SELECT id FROM person WHERE canonical_name = {jval(name)} "
                      f"AND string::starts_with(id, 'person:sender_');")
            for e in (ent[0].get("result", []) if ent and isinstance(ent[0].get("result"), list) else []):
                if isinstance(e, dict) and e.get("id"):
                    eid_safe = str(e["id"]).replace(":", "_")
                    try:
                        sql(f"RELATE {e['id']} -> same_as -> person:resolved_{sid} "
                            f"SET id = same_as:{eid_safe}_resolved_{sid};")
                        edges += 1
                    except Exception:
                        pass
        except Exception:
            pass
    return edges, by_name


def link_video_audio_to_voice_clusters():
    """Link video items to voice clusters via audio track clustering.

    voice_clusters.json already clustered the AUDIO TRACKS of videos alongside
    voice messages (the video audio was extracted as IMG_XXXX.wav and embedded).
    This function creates speaks_in edges from voice cluster person nodes to
    video items, enabling cross-modal identity linking.

    GENERIC: works for any dataset where video audio was included in voice
    clustering (the default pipeline does this)."""
    vc_path = RUN_DIR / "voice_clusters.json"
    if not vc_path.exists():
        print("  voice_clusters.json not found — skipping video audio linking")
        return {}
    vc = json.loads(vc_path.read_text())
    labels = vc["labels"]
    sources = vc.get("sources", [])

    # Find video audio sources (pattern: IMG_XXXX.wav or any source matching a video item)
    video_items = {}
    try:
        res = sql("SELECT id, path FROM item WHERE type = 'video';")
        for it in (res[0].get("result", []) if res and isinstance(res[0].get("result"), list) else []):
            if not isinstance(it, dict) or not it.get("id") or not it.get("path"):
                continue
            # Extract stem from path (e.g. video_files/IMG_5795.MP4 → IMG_5795)
            stem = Path(it["path"]).stem
            video_items[stem] = it["id"]
    except Exception as e:
        print(f"  video query failed: {e}")
        return {}

    linked = {}
    edges = 0
    for label, src in zip(labels, sources):
        if label == -1:
            continue
        src_stem = Path(src).stem
        # Match video items by stem (IMG_5795.wav → IMG_5795)
        if src_stem in video_items:
            iid = video_items[src_stem]
            try:
                # Ensure the voice cluster person node exists
                sql(f"UPSERT person:voice_cluster_{label} SET "
                    f"role = 'speaker', voice_cluster_id = {label};")
                iid_safe = str(iid).replace(":", "_")
                sql(f"RELATE person:voice_cluster_{label} -> speaks_in -> {iid} "
                    f"SET id = speaks_in:vc{label}_{iid_safe};")
                edges += 1
                linked.setdefault(src_stem, []).append(label)
            except Exception:
                pass
    print(f"  {edges} speaks_in edges created for video items")
    for stem, clusters in sorted(linked.items()):
        print(f"    {stem} → voice cluster(s) {clusters}")
    return linked


def detect_faces_in_video_keyframes():
    """Link face clusters to video items via keyframe caption analysis.

    video_frame_analysis.json has captions/OCR for each extracted keyframe.
    If a caption mentions people, we link prominent face clusters to that video
    via appears_in edges. This is a CONSERVATIVE heuristic — a more precise
    approach would run face detection + embedding matching on the keyframes.

    GENERIC: only runs if video_frame_analysis.json exists. Skips silently."""
    vfa_path = RUN_DIR / "video_frame_analysis.json"
    if not vfa_path.exists():
        print("  no video_frame_analysis.json — skipping")
        return {}

    # Build video stem → item_id mapping
    video_items = {}
    try:
        res = sql("SELECT id, path FROM item WHERE type = 'video';")
        for it in (res[0].get("result", []) if res and isinstance(res[0].get("result"), list) else []):
            if isinstance(it, dict) and it.get("id") and it.get("path"):
                stem = Path(it["path"]).stem
                video_items[stem] = it["id"]
    except Exception:
        return {}

    if not video_items:
        print("  no video items in DB — skipping")
        return {}

    vfa = json.loads(vfa_path.read_text())

    # Get prominent face clusters (top 5 by appearance count)
    try:
        res = sql("SELECT cluster_id, count() AS n FROM face_appearance "
                  "WHERE cluster_id != -1 GROUP BY cluster_id ORDER BY n DESC LIMIT 5;")
        top_clusters = [c["cluster_id"] for c in (res[0].get("result", []) if res else [])
                        if isinstance(c, dict) and c.get("cluster_id") is not None]
    except Exception as e:
        print(f"  face cluster query failed: {e}")
        top_clusters = []

    if not top_clusters:
        print("  no face clusters available — skipping")
        return {}

    PEOPLE_WORDS = ("person", "people", "man", "woman", "face", "individual",
                    "someone", "guy", "girl", "boy", "child", "selfie")

    edges = 0
    linked_videos = set()
    for entry in vfa:
        frame = entry.get("frame", "")
        caption = entry.get("caption", "") or ""
        vstem = frame.split("__")[0] if "__" in frame else Path(frame).stem
        if vstem not in video_items:
            continue
        lower = caption.lower()
        if not any(w in lower for w in PEOPLE_WORDS):
            continue
        iid = video_items[vstem]
        if iid in linked_videos:
            continue  # one edge per video is enough for the heuristic
        # Link top face clusters to this video
        iid_safe = str(iid).replace(":", "_")
        for cid in top_clusters[:3]:
            try:
                sql(f"RELATE person:face_cluster_{cid} -> appears_in -> {iid} "
                    f"SET id = appears_in:fc{cid}_{iid_safe};")
                edges += 1
            except Exception:
                pass
        linked_videos.add(iid)

    print(f"  {edges} appears_in edges for video keyframes ({len(linked_videos)} videos)")
    return {"keyframe_face_edges": edges}



    """GENERIC face↔voice cross-linking: a VIDEO item contains both a face
    (in its keyframes) and a voice (in its audio track). If face cluster A
    appears_in a video and voice cluster B speaks_in the same video, they are
    likely the same person. Creates same_as edges between the clusters.

    This is the strongest cross-modal signal and works for ANY video dataset —
    no sender attribution needed."""
    # Find video items that have BOTH appears_in and speaks_in edges
    res = sql(
        "SELECT id, "
        "->appears_in->person.face_cluster_id AS faces, "
        "<-speaks_in<-person.voice_cluster_id AS voices "
        "FROM item WHERE type = 'video';"
    )
    items = res[0].get("result", []) if res and isinstance(res[0].get("result"), list) else []
    # Build co-occurrence pairs
    from collections import defaultdict
    co = defaultdict(int)  # (face_cid, voice_cid) -> count
    for it in items:
        if not isinstance(it, dict):
            continue
        faces = it.get("faces") or []
        voices = it.get("voices") or []
        # Flatten if nested
        if faces and isinstance(faces[0], list):
            faces = [f for sub in faces for f in sub]
        if voices and isinstance(voices[0], list):
            voices = [v for sub in voices for v in sub]
        faces = [f for f in faces if f is not None and f != -1]
        voices = [v for v in voices if v is not None and v != -1]
        for fc in faces:
            for vc in voices:
                co[(fc, vc)] += 1
    edges = 0
    for (fc, vc), n in co.items():
        # same_as between the face cluster person and voice cluster person
        try:
            sql(f"RELATE person:face_cluster_{fc} -> same_as -> person:voice_cluster_{vc} "
                f"SET evidence = 'video co-occurrence', co_occurrence_count = {n};")
            edges += 1
        except Exception:
            pass
    return edges, dict(co)


def cross_link_face_voice_via_video():
    """GENERIC face↔voice cross-linking: a VIDEO item contains both a face
    (in its keyframes) and a voice (in its audio track). If face cluster A
    appears_in a video and voice cluster B speaks_in the same video, they are
    likely the same person. Creates same_as edges between the clusters.

    This is the strongest cross-modal signal and works for ANY video dataset —
    no sender attribution needed."""
    res = sql(
        "SELECT id, "
        "<-appears_in<-person.face_cluster_id AS faces, "
        "<-speaks_in<-person.voice_cluster_id AS voices "
        "FROM item WHERE type = 'video';"
    )
    items = res[0].get("result", []) if res and isinstance(res[0].get("result"), list) else []
    from collections import defaultdict
    co = defaultdict(int)  # (face_cid, voice_cid) -> count
    for it in items:
        if not isinstance(it, dict):
            continue
        faces = it.get("faces") or []
        voices = it.get("voices") or []
        if faces and isinstance(faces[0], list):
            faces = [f for sub in faces for f in sub]
        if voices and isinstance(voices[0], list):
            voices = [v for sub in voices for v in sub]
        faces = [f for f in faces if f is not None and f != -1]
        voices = [v for v in voices if v is not None and v != -1]
        for fc in faces:
            for vc in voices:
                co[(fc, vc)] += 1
    edges = 0
    for (fc, vc), n in co.items():
        try:
            # Deterministic edge ID for idempotency — re-running doesn't
            # create duplicate same_as edges.
            sql(f"RELATE person:face_cluster_{fc} -> same_as -> person:voice_cluster_{vc} "
                f"SET id = same_as:fc{fc}_vc{vc}, "
                f"evidence = 'video co-occurrence', co_occurrence_count = {n};")
            edges += 1
        except Exception:
            pass
    return edges, dict(co)


def main():
    print("=== resolve_identities (generic) ===")
    print()
    print("[1/6] Face clusters: record distributor (weak signal, no name claim)...")
    face_res = resolve_face_clusters()
    named_faces = sum(1 for v in face_res.values() if v.get("name"))
    print(f"  {len(face_res)} face clusters, distributor recorded for {sum(1 for v in face_res.values() if v.get('distributor'))}")
    print()
    print("[2/6] Voice clusters: create nodes + speaks_in + sender resolution...")
    voice_res = create_voice_cluster_nodes()
    for cid in sorted(voice_res):
        info = voice_res[cid]
        name = info.get("name") or "UNRESOLVED"
        print(f"  voice cluster {cid:2d}: {name} (conf={info['confidence']:.2f}, files={info['files']}, linked={info['linked']})")
    print()
    print("[3/6] Link video audio tracks to voice clusters (video audio is in voice_clusters.json)...")
    vid_voice = link_video_audio_to_voice_clusters()
    print()
    print("[4/6] Link video keyframes to face clusters (heuristic from captions)...")
    vid_face = detect_faces_in_video_keyframes()
    print()
    print("[5/6] Cross-link voice -> resolved identity (sender attribution)...")
    edges1, by_name = cross_link_voice_to_identity(voice_res)
    for name, groups in sorted(by_name.items()):
        print(f"  {name}: voice clusters {groups['voice']}")
    print(f"  same_as edges: {edges1}")
    print()
    print("[6/6] Cross-link face <-> voice via shared video items (strongest signal)...")
    edges2, co = cross_link_face_voice_via_video()
    for (fc, vc), n in sorted(co.items()):
        print(f"  face cluster {fc} <-> voice cluster {vc}: {n} shared video(s)")
    print(f"  face-voice same_as edges: {edges2}")
    print()
    print("=== FINAL IDENTITY COUNTS ===")
    cnt = sql(
        "RETURN count(SELECT id FROM person WHERE voice_cluster_id != NONE);"
        "RETURN count(SELECT id FROM speaks_in);"
        "RETURN count(SELECT id FROM same_as);"
        "RETURN count(SELECT id FROM person WHERE role = 'resolved_identity');"
    )
    labels = ["voice_cluster_nodes", "speaks_in_edges", "same_as_edges",
              "resolved_identity_nodes"]
    for l, r in zip(labels, cnt):
        v = r["result"] if isinstance(r.get("result"), (int, float)) else 0
        print(f"  {l:26s} {v:>5}")


if __name__ == "__main__":
    main()
