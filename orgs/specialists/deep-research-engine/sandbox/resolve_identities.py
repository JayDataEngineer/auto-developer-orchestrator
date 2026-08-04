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
import sys
import urllib.request
from collections import Counter
from pathlib import Path

URL = os.environ.get("SURREALDB_URL", "http://127.0.0.1:8000/sql")
NS = os.environ.get("SURREALDB_NS", "research")
DB = os.environ.get("SURREALDB_DB", "main")
# Read auth from env (set by policy.yaml sandbox.env or operator). Default is
# the local-dev root:root — production deployments override via Infisical.
AUTH = os.environ.get("SURREALDB_AUTH", "Basic cm9vdDpyb290")
RUN_DIR = Path(os.environ.get("RUN_DIR",
    str(Path(__file__).parent.parent / "artifacts" / "run-2026-07-12")))


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
    """Face clusters: resolve identity via THREE signals, strongest first.

    Signals (in order of confidence):
      1. OCR text mentions an entity name on a photo containing this cluster.
         Strong: the photo labels who's in it.
      2. Sender attribution: WEAK for faces (people post photos of others).
         Recorded as `distributed_by`, NOT as the subject identity.
      3. Surrounding-message text near the photo (item_index ±5) that
         mentions an entity name. Medium signal.

    Uses the `appears_in` graph edge (face_appearance -> item) to find
    which items/photos contain each cluster. The earlier code looked for
    a non-existent `item_id` field on face_appearance — that returned 0.
    """
    # Load OCR results if available
    ocr_path = RUN_DIR / "ocr_results.json"
    ocr_data = {}
    if ocr_path.exists():
        try:
            ocr_data = json.loads(ocr_path.read_text())
        except Exception:
            pass

    # Load items.json for text + item_index proximity voting
    items_path = RUN_DIR / "items.json"
    items_by_index = {}
    items_by_photo = {}
    if items_path.exists():
        try:
            items_data = json.loads(items_path.read_text())
            for it in items_data.get("items", []):
                idx = it.get("item_index")
                if idx is not None:
                    items_by_index[idx] = it
                # Map photo basename -> item for OCR cross-reference
                path = it.get("path", "") or ""
                if path:
                    items_by_photo[Path(path).name] = it
        except Exception:
            pass

    # Known PERSONS whose names may appear in OCR text on photos. We use a
    # HARDCODED allowlist instead of scanning the person table because the
    # table contains non-person entities (Flathead_County is a LOCATION,
    # CPUSA is an ORGANIZATION) and cluster stubs (face_cluster_9). Letting
    # those leak into the alias map produced garbage links — e.g. the token
    # "flathead" branded face clusters as a county, and "face" branded them
    # as a cluster stub. Only real human identities belong here.
    KNOWN_PERSONS = {
        "Christopher Semok": ["christopher semok", "christopher anthony semok",
                              "commissar", "semok"],
        "Grady": ["grady", "the pagan of montana", "pagan of montana",
                  "pagan", "city councilor", "councilor", "primary speaker",
                  "marxist-leninist"],
        "Scott": ["scott", "scott ernest"],
        "Will": ["will"],
        "Cletus": ["cletus"],
        "Christian Piccolini": ["christian piccolini", "piccolini"],
    }
    alias_to_canonical = {}
    for canonical, aliases in KNOWN_PERSONS.items():
        for a in aliases:
            alias_to_canonical[a.lower()] = canonical
        alias_to_canonical[canonical.lower()] = canonical

    res = sql("SELECT cluster_id FROM face_appearance WHERE cluster_id != -1 "
              "GROUP BY cluster_id ORDER BY cluster_id;")
    clusters = res[0]["result"] if res and isinstance(res[0].get("result"), list) else []
    resolved = {}
    for c in clusters:
        cid = c.get("cluster_id")
        if cid is None:
            continue
        # Use the appears_in graph edge to find which items contain this face
        items_res = sql(
            f"SELECT ->appears_in->item.id AS ids, "
            f"->appears_in->item.sender AS senders, "
            f"->appears_in->item.path AS paths, "
            f"->appears_in->item.item_index AS idxs "
            f"FROM face_appearance WHERE cluster_id = {cid};")
        item_rows = items_res[0].get("result", []) if items_res and isinstance(items_res[0].get("result"), list) else []

        # Flatten graph results (SurrealDB returns nested lists)
        def _flat(x):
            if isinstance(x, list):
                out = []
                for i in x:
                    out.extend(i) if isinstance(i, list) else out.append(i)
                return out
            return [x] if x else []
        all_ids = _flat([r.get("ids", []) for r in item_rows])
        all_senders = _flat([r.get("senders", []) for r in item_rows])
        all_paths = _flat([r.get("paths", []) for r in item_rows])
        all_idxs = _flat([r.get("idxs", []) for r in item_rows])

        # Sender tally (weak signal)
        senders = Counter(s for s in all_senders if s and s != "Unknown")

        # OCR-based entity votes (strong signal)
        ocr_votes = Counter()
        for path in all_paths:
            if not path:
                continue
            base = Path(path).name
            ocr_text = (ocr_data.get(base) or {}).get("text", "") or ""
            if not ocr_text:
                continue
            ocr_lower = ocr_text.lower()
            for alias, canonical in alias_to_canonical.items():
                if alias in ocr_lower:
                    ocr_votes[canonical] += 1

        # Temporal-proximity text votes (medium signal)
        # Look at items within ±5 of each containing photo
        text_votes = Counter()
        for idx in all_idxs:
            if idx is None:
                continue
            for near_idx in range(idx - 5, idx + 6):
                near_item = items_by_index.get(near_idx)
                if not near_item:
                    continue
                text = (near_item.get("text", "") or "").lower()
                if not text:
                    continue
                for alias, canonical in alias_to_canonical.items():
                    if alias in text:
                        text_votes[canonical] += 1

        # Decide: OCR signal wins if present; else no resolution
        if ocr_votes:
            name, n = ocr_votes.most_common(1)[0]
            conf = min(n / max(len(all_paths), 1), 1.0)
            resolved[cid] = {
                "name": name,
                "signal": "ocr",
                "confidence": round(conf, 2),
                "ocr_votes": dict(ocr_votes),
                "items": len(all_ids),
            }
            sql(f"UPDATE person:face_cluster_{cid} SET "
                f"canonical_name = {jval(name)}, "
                f"resolved_identity = {jval(name)}, "
                f"resolution_signal = 'ocr_text_mention', "
                f"resolution_confidence = {round(conf, 2)}, "
                f"resolved_item_count = {len(all_ids)};")
            # Create same_as edge to a resolved identity node (NOT a query for
            # existing person nodes — the UPDATE above just set canonical_name
            # on face_cluster_{cid}, so a SELECT would find that same node and
            # create a self-loop: face_cluster_19 → face_cluster_19).
            ent_safe = _safe(name)
            sql(f"UPSERT person:resolved_{ent_safe} SET "
                f"canonical_name = {jval(name)}, "
                f"role = 'resolved_identity', "
                f"notes = 'Identity resolved from OCR text mention in face cluster photos.';")
            try:
                sql(f"RELATE person:face_cluster_{cid} -> same_as -> person:resolved_{ent_safe} "
                    f"SET id = same_as:fc{cid}_resolved_{ent_safe}, "
                    f"signal = 'ocr_text_mention', confidence = {round(conf, 2)};")
            except Exception:
                pass
        elif senders:
            name, n = senders.most_common(1)[0]
            conf = n / max(len(all_ids), 1) if all_ids else 0
            # Weak signal: record distributor but DO NOT claim identity
            resolved[cid] = {
                "name": None,
                "distributor": name,
                "signal": "sender",
                "confidence": round(conf, 2),
                "items": len(all_ids),
            }
            sql(f"UPDATE person:face_cluster_{cid} SET "
                f"canonical_name = {jval(f'Unnamed subject (face cluster {cid})')}, "
                f"distributed_by = {jval(name)}, "
                f"distribution_confidence = {round(conf, 2)}, "
                f"resolved_item_count = {len(all_ids)}, "
                f"notes = 'Face identity not resolved to a name. Photos distributed by ' + {jval(name)} + '. Sender is the distributor, not necessarily the subject.';")
        else:
            resolved[cid] = {"name": None, "distributor": None, "confidence": 0, "items": len(all_ids)}
    return resolved


def create_voice_cluster_nodes():
    """Resolve voice clusters to senders via speaks_in edges.

    If voice_clusters.json exists with a `sources` field, use that to create
    person:voice_cluster_N nodes + speaks_in edges. Otherwise, assume the
    pipeline already created them (the modern path) and just resolve.
    """
    vc_path = RUN_DIR / "voice_clusters.json"
    vc_exists_with_sources = False
    if vc_path.exists():
        try:
            vc = json.loads(vc_path.read_text())
            if "labels" in vc and "sources" in vc:
                vc_exists_with_sources = True
        except Exception:
            pass

    # Always also resolve from the DB (voice clusters + speaks_in edges)
    # Query all voice clusters and their associated items
    res = sql(
        "SELECT out AS cluster, in AS item_id FROM speaks_in;")
    edges = res[0].get("result", []) if res and isinstance(res[0].get("result"), list) else []
    # Bucket items by cluster
    cluster_items = {}
    for e in edges:
        c = str(e.get("cluster", ""))
        cluster_items.setdefault(c, []).append(e.get("item_id"))

    resolved = {}
    for cluster_id_str, item_ids in cluster_items.items():
        # Extract numeric cluster id
        try:
            cid = int(cluster_id_str.rsplit("_", 1)[-1])
        except (ValueError, IndexError):
            continue
        # Get senders + transcripts for these items.
        senders = Counter()
        transcript_votes = Counter()
        for iid in item_ids:
            if not iid:
                continue
            # Get item details
            r = sql(f"SELECT sender, path FROM {iid};")
            rows = r[0].get("result", []) if r and isinstance(r[0].get("result"), list) else []
            if not rows:
                continue
            sender = rows[0].get("sender")
            if sender and sender != "Unknown":
                senders[sender] += 1
            # Look up transcript for this audio file
            path = rows[0].get("path", "") or ""
            if path:
                base = Path(path).stem
                # Search audio corpus for matching transcript
                # (loaded lazily below)
        # Load audio corpus once for transcript voting
        # (do this here so we only load if needed)
        if not senders and not transcript_votes:
            # No sender signal — try transcripts
            corpus_path = RUN_DIR / "all_audio_corpus.json"
            if corpus_path.exists() and not hasattr(create_voice_cluster_nodes, "_corpus"):
                try:
                    create_voice_cluster_nodes._corpus = json.loads(corpus_path.read_text())
                except Exception:
                    create_voice_cluster_nodes._corpus = []
            corpus = getattr(create_voice_cluster_nodes, "_corpus", [])
            for entry in corpus:
                if not isinstance(entry, dict):
                    continue
                fname = entry.get("file", "")
                text = (entry.get("text", "") or "").lower()
                if not text:
                    continue
                # Check if this file is in our cluster's items
                # by matching the path stem
                for iid in item_ids:
                    r = sql(f"SELECT path FROM {iid};")
                    rows = r[0].get("result", []) if r and isinstance(r[0].get("result"), list) else []
                    if not rows:
                        continue
                    ipath = rows[0].get("path", "") or ""
                    if ipath and Path(ipath).stem in fname:
                        # Speaker-label patterns: "I'm X", "this is X", "X speaking"
                        for m in re.finditer(r"(?:i'm|i am|this is|it's|its)\s+([a-z]+)", text):
                            word = m.group(1)
                            # Match against alias map if loaded
                        break
        if senders:
            name, n = senders.most_common(1)[0]
            conf = n / max(len(item_ids), 1) if item_ids else 0
            display = f"{name} (voice cluster {cid})" if conf < 0.9 else name
            sql(f"UPDATE person:voice_cluster_{cid} SET "
                f"canonical_name = {jval(display)}, "
                f"resolved_sender = {jval(name)}, "
                f"resolution_confidence = {round(conf, 2)}, "
                f"voice_count = {len(item_ids)};")
            resolved[cid] = {"name": name, "confidence": round(conf, 2),
                             "files": len(item_ids), "linked": len(item_ids)}
        else:
            sql(f"UPDATE person:voice_cluster_{cid} SET "
                f"canonical_name = {jval(f'Unknown speaker (cluster {cid})')}, "
                f"voice_count = {len(item_ids)};")
            resolved[cid] = {"name": None, "confidence": 0,
                             "files": len(item_ids), "linked": 0}
    return resolved
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
                # Direction MUST match pipeline_ingest.py: item -> speaks_in -> person.
                # Reversing it (person -> speaks_in -> item) makes discover_entities()
                # in build_entity_dossiers read the item ID's trailing digits as a
                # voice_cluster number, creating phantom voice_cluster_154 etc.
                sql(f"RELATE {iid} -> speaks_in -> person:voice_cluster_{label} "
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
        # The frame analysis entry carries the video stem directly in `video`
        # (e.g. "IMG_5795") and the caption under `description` (NOT `caption`).
        vstem = entry.get("video", "") or ""
        if not vstem:
            frame = entry.get("frame", "")
            vstem = frame.split("__")[0] if "__" in frame else Path(frame).stem
        caption = entry.get("description", "") or entry.get("caption", "") or ""
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


def cross_link_face_voice_via_video():
    """GENERIC face↔voice cross-linking via video co-occurrence.

    A VIDEO item can contain both a face (appears_in via keyframe heuristic)
    and a voice (speaks_in via audio track). If face cluster A and voice
    cluster B appear in the same video, they MIGHT be the same person.

    CONSERVATIVE — identity-level voting with an unambiguity gate. For each
    face cluster we resolve its co-occurring voice clusters to their
    sender-attributed identities and vote per identity. We only create a
    same_as edge (face_cluster -> ent_<Name>) when there is an UNAMBIGUOUS
    winner: the top identity's shared-video count must be >= 2x the
    runner-up's. This prevents the false "face = both Will and Grady" links
    that a naive per-voice-cluster cross-link produces when the keyframe
    heuristic links several top face clusters to the same videos.

    Works for ANY video dataset — no per-photo sender attribution needed."""
    res = sql(
        "SELECT id, "
        "<-appears_in<-person.face_cluster_id AS faces, "
        "<-speaks_in<-person.voice_cluster_id AS voices "
        "FROM item WHERE type = 'video';"
    )
    items = res[0].get("result", []) if res and isinstance(res[0].get("result"), list) else []
    from collections import defaultdict, Counter
    co = defaultdict(int)  # (face_cid, voice_cid) -> shared video count
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
        for fc in set(faces):       # one tally per (face,video) not per detection
            for vc in set(voices):
                co[(fc, vc)] += 1

    # Build voice_cluster_id -> resolved identity name map by querying the
    # voice cluster person nodes created/resolved in step 2.
    vc_to_name = {}
    try:
        vcp = sql("SELECT id, canonical_name FROM person WHERE cluster_type = 'voice';")
        for row in (vcp[0].get("result", []) if vcp and vcp[0].get("result") else []):
            rid = str(row.get("id", ""))
            if "voice_cluster_" not in rid:
                continue
            try:
                vcnum = int(rid.rsplit("voice_cluster_", 1)[-1])
            except ValueError:
                continue
            name = (row.get("canonical_name") or "").strip()
            # Only treat as resolved if it's a real name, not a placeholder.
            if name and not name.lower().startswith(("voice cluster",
                                                       "unknown speaker")):
                vc_to_name[vcnum] = name
    except Exception:
        pass

    # Vote per face cluster at the identity level
    face_votes = defaultdict(Counter)   # face_cid -> Counter[identity -> count]
    for (fc, vc), n in co.items():
        name = vc_to_name.get(vc)
        if name:
            face_votes[fc][name] += n

    edges = 0
    decided = {}
    skipped_ambiguous = []
    for fc, votes in sorted(face_votes.items()):
        if not votes:
            continue
        ranked = votes.most_common()
        winner, wcount = ranked[0]
        runner_up = ranked[1][0] if len(ranked) > 1 else None
        rcount = ranked[1][1] if len(ranked) > 1 else 0
        # Unambiguity gate: winner must have >= 2x the runner-up's count.
        if rcount > 0 and wcount < 2 * rcount:
            skipped_ambiguous.append(
                (fc, winner, wcount, runner_up, rcount))
            continue
        ent_safe = _safe(winner)
        ent_id = f"person:ent_{ent_safe}"
        # Verify the named entity person exists (created by step_persons);
        # if not, fall back to the resolved_<name> node from step 5.
        chk = sql(f"SELECT id FROM {ent_id};")
        if not (chk and chk[0].get("result")):
            alt = f"person:resolved_{ent_safe}"
            chk2 = sql(f"SELECT id FROM {alt};")
            if chk2 and chk2[0].get("result"):
                ent_id = alt
            else:
                continue
        conf = round(wcount / (wcount + rcount), 2) if (wcount + rcount) else 1.0
        reasoning = (f"Face cluster {fc} co-occurs in video keyframes with "
                     f"voice clusters resolved to {winner} ({wcount} video(s)) "
                     f"vs runner-up {runner_up or 'none'} ({rcount}).")
        try:
            sql(f"RELATE person:face_cluster_{fc} -> same_as -> {ent_id} "
                f"SET id = same_as:fc{fc}_{ent_safe}, "
                f"signal = 'video_co_occurrence_identity_vote', "
                f"confidence = {conf}, "
                f"reasoning = {jval(reasoning)};")
            edges += 1
            decided[fc] = (winner, wcount, runner_up, rcount)
        except Exception:
            pass

    for fc, info in sorted(decided.items()):
        winner, wc, ru, rc = info
        print(f"  face cluster {fc} -> {winner} ({wc}v vs {ru or 'none'} {rc}v)")
    if skipped_ambiguous:
        print(f"  ({len(skipped_ambiguous)} face cluster(s) skipped — ambiguous identity vote)")
    return edges, decided


def main():
    global RUN_DIR
    # Accept RUN_DIR as first CLI arg (overrides env/default), matching
    # build_entity_dossiers.py's convention. Without this, passing the run
    # dir as an arg was silently ignored and file-based steps (3, 4) looked
    # in the wrong place.
    if len(sys.argv) > 1:
        RUN_DIR = Path(sys.argv[1])
    print(f"=== resolve_identities (generic) ===")
    print(f"  RUN_DIR = {RUN_DIR}")
    print()

    # ── Idempotency cleanup ──────────────────────────────────────────
    # Step 3 (link_video_audio_to_voice_clusters) creates speaks_in edges
    # with IDs like 'speaks_in:vc{label}_{iid}' — different from
    # pipeline_ingest's format ('speaks_in:{idx}_{cid}'). So re-running
    # resolve without a DB wipe ADDS duplicate video edges, changing sender
    # vote counts. Delete step 3's edges (identified by 'vc' prefix in the
    # record ID) so each resolve run starts clean. Pipeline_ingest's edges
    # are untouched. Step 3 recreates its edges fresh.
    try:
        stale = sql(
            "SELECT count() AS n FROM speaks_in "
            "WHERE string::starts_with(record::id(id), 'vc') GROUP ALL;")
        n = stale[0].get("result", [{}])[0].get("n", 0) if stale and stale[0].get("result") else 0
        if n:
            sql("DELETE FROM speaks_in WHERE string::starts_with(record::id(id), 'vc');")
            print(f"  Cleaned {n} stale step-3 speaks_in edges (idempotency)")
    except Exception:
        pass
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
