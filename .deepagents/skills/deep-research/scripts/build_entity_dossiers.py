#!/usr/bin/env python3
"""Build subject-based entity dossiers — one folder per entity with full report
+ associated images, videos, audio, and text.

DESIGN
------
The OLD entities/ folder was organized by MODALITY (face_clusters/,
voice_clusters/, text_and_scenes/, video_frames/) — just raw pipeline output
dumped into folders. Useless for an analyst.

The NEW entities/ folder is organized by SUBJECT:

    entities/
        Grady_The_Pagan_of_Montana/
            Grady_The_Pagan_of_Montana.md    # full dossier
            photos/                          # photos OF this person (face cluster)
            videos/                          # videos mentioning/showing them
            audio/                           # audio OF this person (voice cluster)
            text/                            # plaintext messages mentioning them
        CPUSA/
            CPUSA.md
            images/  videos/  audio/  text/
        Christopher_Anthony_Semok/
            ...

Media files are SYMLINKED (not copied) to avoid duplicating a 1.2 GB dataset.

GENERIC: reads RUN_DIR from env (or arg). Works on any dataset.
IDEMPOTENT: wipes entities/ on each run, rebuilds cleanly.
"""
import json
import os
import re
import shutil
import sys
import urllib.request
from collections import defaultdict
from pathlib import Path

URL = os.environ.get("SURREALDB_URL", "http://127.0.0.1:8000/sql")
if not URL.endswith("/sql"):
    URL = URL.rstrip("/") + "/sql"
URL = URL.replace("/mcp", "/sql")
NS = os.environ.get("SURREALDB_NS", "research")
DB = os.environ.get("SURREALDB_DB", "main")
AUTH = os.environ.get("SURREALDB_AUTH", "Basic cm9vdDpyb290")


def sql(body, timeout=30):
    req = urllib.request.Request(
        URL, data=body.encode(),
        headers={"Accept": "application/json", "surreal-ns": NS,
                 "surreal-db": DB, "Authorization": AUTH},
    )
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            return json.loads(r.read())
    except Exception as e:
        print(f"  SQL error: {e}", file=sys.stderr)
        return []


def slug(name):
    """Filesystem-safe entity folder name."""
    s = re.sub(r"[^A-Za-z0-9]+", "_", name).strip("_")
    return s[:80] if s else "unknown"


def find_media(filename, data_root):
    """Find a media file by name anywhere under data_root. Returns Path or None."""
    if not filename:
        return None
    # Strip directory components — just the basename
    base = Path(filename).name
    # Direct search
    for p in data_root.rglob(base):
        if p.is_file():
            return p
    # Try without extension changes
    stem = Path(base).stem
    for p in data_root.rglob(f"{stem}.*"):
        if p.is_file() and p.suffix.lower() in ('.jpg', '.jpeg', '.png', '.mp4',
                                                  '.mov', '.m4v', '.webm',
                                                  '.m4a', '.wav', '.ogg', '.mp3'):
            return p
    return None


def load_artifacts(run_dir):
    """Load all JSON artifacts we'll mine for entity evidence."""
    arts = {}
    for name in ['all_audio_corpus.json', 'audio_summaries.json', 'audio_chunks.json',
                 'face_analysis.json', 'face_clusters.json',
                 'video_frame_analysis.json', 'items.json']:
        p = run_dir / name
        if p.exists():
            try:
                arts[name] = json.loads(p.read_text())
            except Exception:
                pass
    # video_summaries is under entities/video_frames/
    vs = run_dir / "entities" / "video_frames" / "video_summaries.json"
    if vs.exists():
        try:
            arts['video_summaries.json'] = json.loads(vs.read_text())
        except Exception:
            pass
    return arts


def discover_entities(run_dir, arts):
    """Discover entities from SurrealDB + artifacts. Returns list of dicts:

        {name, kind, slug, aliases, evidence_score, attributes}

    kind: person | organization | location | topic
    evidence_score: weighted count of mentions across modalities
    """
    entities = {}

    # --- From SurrealDB: person, organization, location, topic nodes ---
    for table, kind in [('person', 'person'), ('organization', 'organization'),
                        ('location', 'location'), ('topic', 'topic')]:
        res = sql(f"SELECT * FROM {table};")
        rows = res[0].get('result', []) if res and res[0].get('result') else []
        for row in rows:
            name = row.get('canonical_name') or row.get('name', '')
            if not name or name.startswith('Unnamed') or name.startswith('Identity-'):
                continue
            if name in entities:
                entities[name]['surreal_ids'].append(str(row.get('id', '')))
                continue
            entities[name] = {
                'name': name,
                'kind': kind,
                'slug': slug(name),
                'surreal_ids': [str(row.get('id', ''))],
                'role': row.get('role', ''),
                'notes': row.get('notes', ''),
                'item_count': row.get('item_count', 0),
                'voice_cluster_ids': [],
                'face_cluster_ids': [],
                'aliases': set(),
                'text_hits': 0,
                'audio_hits': 0,
                'video_hits': 0,
                'image_count': 0,
            }

    # --- Build alias map for key entities (hardcoded intelligence from the data) ---
    # These are known aliases discovered in the report + transcripts. Adding them
    # here means the evidence search will find ALL variants.
    # IMPORTANT: each canonical name maps to ALL its aliases. When multiple
    # DB rows match the same alias list (e.g. "Grady" and "(Grady)The Pagan
    # of Montana" are the same person), they get MERGED into one entity —
    # we don't want two folders for the same guy.
    alias_map = {
        'Grady': ['Grady', '(Grady)The Pagan of Montana', '(Grady) The Pagan of Montana',
                  'The Pagan of Montana', 'Primary Speaker', 'City Councilor',
                  'councilor', 'Marxist-Leninist'],
        'Scott': ['Scott', 'Scott Ernest', 'roommate'],
        'Christopher Semok': ['Christopher Semok', 'Christopher Anthony Semok',
                              'Commissar', 'Commissar ANTIFA', 'Semok'],
        'CPUSA': ['CPUSA', 'Communist Party', 'Communist Party USA',
                  'Communist Party of America'],
        'Patriot Front': ['Patriot Front'],
        'White Lives Matter': ['White Lives Matter', 'WLM', 'WLM Montana'],
        'Antifa': ['Antifa', 'ANTIFA'],
        'Flathead County': ['Flathead County', 'Flathead', 'Flathead Valley'],
        'Cletus': ['Cletus', 'completus'],
    }
    for ent_name, aliases in alias_map.items():
        # Find ALL existing entities that match ANY alias for this canonical
        # name, then merge them into the first match. This prevents the same
        # person from appearing under both "Grady" and "(Grady)The Pagan of
        # Montana" because the DB has them as separate rows.
        matched_list = []
        for alias in [ent_name] + aliases:
            for existing_name, existing_ent in list(entities.items()):
                if existing_name == alias or existing_name in aliases:
                    if existing_ent not in matched_list:
                        matched_list.append((existing_name, existing_ent))
        if matched_list:
            # Use the canonical name; merge others into it
            primary_name, primary = matched_list[0]
            # Rename to canonical if needed
            if primary_name != ent_name:
                # Move primary to canonical key, preserving its data
                entities[ent_name] = primary
                # Don't delete primary_name yet — we'll merge extras into it
            primary['aliases'].update(aliases)
            primary['aliases'].add(ent_name)
            # Merge any additional matches into primary
            for name, ent in matched_list[1:]:
                primary['aliases'].add(name)
                primary['surreal_ids'].extend(ent['surreal_ids'])
                primary['text_hits'] += ent['text_hits']
                primary['audio_hits'] += ent['audio_hits']
                primary['video_hits'] += ent['video_hits']
                for cid in ent.get('face_cluster_ids', []) or []:
                    if cid not in primary['face_cluster_ids']:
                        primary['face_cluster_ids'].append(cid)
                for cid in ent.get('voice_cluster_ids', []) or []:
                    if cid not in primary['voice_cluster_ids']:
                        primary['voice_cluster_ids'].append(cid)
                # Remove the duplicate
                if name != ent_name:
                    entities.pop(name, None)
            # Ensure 'name' field reflects canonical
            primary['name'] = ent_name
            primary['slug'] = slug(ent_name)
        else:
            # Create the entity if it doesn't exist but we have aliases for it
            entities[ent_name] = {
                'name': ent_name,
                'kind': 'person' if ent_name[0].isupper() and ' ' not in ent_name else
                        ('organization' if ent_name in ('CPUSA', 'Patriot Front',
                         'White Lives Matter', 'Antifa') else 'person'),
                'slug': slug(ent_name),
                'surreal_ids': [],
                'role': '',
                'notes': '',
                'item_count': 0,
                'voice_cluster_ids': [],
                'face_cluster_ids': [],
                'aliases': set(aliases),
                'text_hits': 0,
                'audio_hits': 0,
                'video_hits': 0,
                'image_count': 0,
            }

    # --- Traverse same_as edges: propagate cluster IDs to named entities ---
    # When identity resolution links a cluster to a named person via
    # `person:face_cluster_N -> same_as -> person:ent_X`, we want ent_X to
    # inherit that cluster_id so its dossier picks up the cluster's photos.
    #
    # This is the critical bridge between the resolver/agent writing edges
    # and the dossier builder surfacing cluster media under named entities.
    # Without this, Semok's photos/ stays empty even after the
    # agent correctly links face_cluster_2 -> same_as -> ent_Christopher_Semok.
    try:
        sa_res = sql(
            "SELECT in AS cluster_node, out AS target FROM same_as;")
        sa_rows = sa_res[0].get('result', []) if sa_res and sa_res[0].get('result') else []
    except Exception:
        sa_rows = []
    # Lookup index: map every normalized name variant (canonical, aliases,
    # underscore-flattened) to the entity dict. SurrealDB stores names with
    # underscores ('Christopher_Semok'); the alias_map uses spaces. We must
    # bridge both worlds.
    def _norm(s):
        return (s or '').lower().replace('_', ' ').strip()
    _ent_by_name = {}
    for ent in entities.values():
        # Canonical name + all aliases, normalized
        for n in [ent['name']] + list(ent.get('aliases', []) or []):
            _ent_by_name.setdefault(_norm(n), ent)
    # Also match the ent_<suffix> ID form (e.g. person:ent_Christopher_Semok
    # → 'christopher semok')
    for ent in entities.values():
        for sid in ent.get('surreal_ids', []) or []:
            sid_s = str(sid)
            if 'ent_' in sid_s:
                suffix = sid_s.split('ent_', 1)[-1]
                # Strip trailing table qualifiers if any
                suffix = suffix.rstrip('>').strip()
                _ent_by_name.setdefault(_norm(suffix), ent)
    propagated = 0
    for row in sa_rows:
        cluster_node = str(row.get('cluster_node', ''))
        target_node = str(row.get('target', ''))
        # Only consider cluster -> named direction
        if 'face_cluster_' not in cluster_node and 'voice_cluster_' not in cluster_node:
            continue
        try:
            # Look up the target person's canonical_name from SurrealDB
            tgt_res = sql(f"SELECT canonical_name FROM {target_node};")
            tgt_rows = tgt_res[0].get('result', []) if tgt_res and tgt_res[0].get('result') else []
            if not tgt_rows:
                continue
            tgt_name = tgt_rows[0].get('canonical_name', '') or ''
            tgt_ent = _ent_by_name.get(_norm(tgt_name))
            # Also try the ID suffix as a fallback
            if not tgt_ent and 'ent_' in str(target_node):
                suffix = str(target_node).split('ent_', 1)[-1].rstrip('>').strip()
                tgt_ent = _ent_by_name.get(_norm(suffix))
            if not tgt_ent:
                continue
            # Propagate the cluster id
            if 'face_cluster_' in cluster_node:
                try:
                    cid = int(cluster_node.rsplit('_', 1)[-1])
                except ValueError:
                    continue
                if cid not in tgt_ent['face_cluster_ids']:
                    tgt_ent['face_cluster_ids'].append(cid)
                    propagated += 1
            elif 'voice_cluster_' in cluster_node:
                try:
                    cid = int(cluster_node.rsplit('_', 1)[-1])
                except ValueError:
                    continue
                if cid not in tgt_ent['voice_cluster_ids']:
                    tgt_ent['voice_cluster_ids'].append(cid)
                    propagated += 1
        except Exception:
            continue
    if propagated:
        print(f"[discover_entities] propagated cluster IDs to {propagated} "
              f"named entities via same_as edges")

    # --- Score entities by evidence across modalities ---
    items = arts.get('items.json', [])
    if isinstance(items, dict):
        items = list(items.values())
    audio_corpus = arts.get('all_audio_corpus.json', [])
    if isinstance(audio_corpus, dict):
        audio_corpus = list(audio_corpus.values())
    audio_sums = arts.get('audio_summaries.json', [])
    video_sums = arts.get('video_summaries.json', {})
    if isinstance(video_sums, dict):
        video_sums = list(video_sums.values())
    else:
        video_sums = video_sums if video_sums else []

    for ent in entities.values():
        aliases_lower = [a.lower() for a in ent['aliases']] or [ent['name'].lower()]
        # Text hits
        for item in items:
            if not isinstance(item, dict):
                continue
            text = (item.get('text', '') or '').lower()
            if any(a in text for a in aliases_lower):
                ent['text_hits'] += 1
        # Audio hits (corpus + summaries)
        for entry in audio_corpus:
            if not isinstance(entry, dict):
                continue
            text = (entry.get('text', '') or entry.get('transcript', '') or '').lower()
            if any(a in text for a in aliases_lower):
                ent['audio_hits'] += 1
                break  # once per file
        for entry in audio_sums:
            if not isinstance(entry, dict):
                continue
            text = (entry.get('summary', '') or '').lower()
            if any(a in text for a in aliases_lower):
                ent['audio_hits'] += 1
                break
        # Video hits — search both the narrative field (video_summaries.json
        # uses `narrative`, not `summary`) and video_frame_analysis.json
        # descriptions. Without both, entities like Semok who appear in
        # video VLM descriptions score 0 and get treated as text-only.
        for entry in video_sums:
            if not isinstance(entry, dict):
                continue
            text = (entry.get('narrative', '') or entry.get('summary', '') or '').lower()
            if any(a in text for a in aliases_lower):
                ent['video_hits'] += 1
                break
        if not ent['video_hits']:
            vfa = arts.get('video_frame_analysis.json', [])
            if isinstance(vfa, dict):
                vfa = list(vfa.values())
            for entry in vfa:
                if not isinstance(entry, dict):
                    continue
                text = (entry.get('description', '') or '').lower()
                if any(a in text for a in aliases_lower):
                    ent['video_hits'] += 1
                    break
        # Image count (for people with face clusters) — sum across ALL
        # linked face clusters (a named entity may have several).
        if ent.get('face_cluster_ids'):
            fc = arts.get('face_clusters.json', {})
            fa = arts.get('face_analysis.json', [])
            if isinstance(fa, list):
                labels = fc.get('labels', [])
                ordered = []
                for entry in fa:
                    embs = entry.get('embeddings') or []
                    for _ in range(len(embs)):
                        ordered.append(entry['photo'])
                wanted = set(ent['face_cluster_ids'])
                ent['image_count'] = sum(1 for i, lbl in enumerate(labels)
                                         if lbl in wanted and i < len(ordered))

    # --- Add face-cluster pseudo-entities ---
    # Each face cluster is a PROVEN person (same face across multiple photos,
    # verified by embedding similarity) but their NAME is unknown unless
    # identity resolution has linked them to a person row.
    #
    # This is the honest representation: instead of dumping random
    # proximity-photos into named entity folders, we create one folder per
    # cluster showing all photos that face appears in. An investigator can
    # then visually identify "this is Semok" and propose the linkage.
    fc_res = sql(
        "SELECT cluster_id, count() AS n, "
        "->appears_in->item.path AS photo_paths, "
        "->appears_in->item.sender AS senders "
        "FROM face_appearance WHERE cluster_id >= 0 GROUP BY cluster_id;")
    fc_rows = fc_res[0].get('result', []) if fc_res and fc_res[0].get('result') else []
    def _flatten(x):
        if isinstance(x, list):
            out = []
            for i in x:
                if isinstance(i, list):
                    out.extend(i)
                else:
                    out.append(i)
            return out
        return [x] if x else []
    for row in fc_rows:
        cid = row.get('cluster_id')
        if cid is None:
            continue
        name = f"face_cluster_{cid}"
        photos = sorted(set(_flatten(row.get('photo_paths', []))))
        senders = sorted(set(_flatten(row.get('senders', []))))
        entities[name] = {
            'name': name,
            'kind': 'person',
            'slug': name,
            'surreal_ids': [],
            'role': 'unidentified person (face cluster)',
            'notes': (f'Unidentified person whose face appears in {len(photos)} photo(s). '
                      f'Senders who posted: {", ".join(senders) or "unknown"}. '
                      f'Identity resolution not yet performed — link to a named '
                      f'person via a same_as edge (see .deepagents/skills/deep-research/scripts/link_cluster.py) '
                      f'to populate that person\'s photos/ folder with this cluster.'),
            'item_count': row.get('n', 0),
            'voice_cluster_ids': [],
            'face_cluster_ids': [cid],
            'aliases': set([name]),
            'text_hits': 0,
            'audio_hits': 0,
            'video_hits': 0,
            'image_count': len(photos),
            'is_cluster': True,
            'cluster_photos': photos,
            'cluster_senders': senders,
        }

    # --- Add voice-cluster pseudo-entities ---
    # Same idea: a PROVEN speaker (same voice across multiple audio files)
    # whose name is unknown. NOTE: SurrealQL's GROUP BY drops non-aggregated
    # fields, so we fetch aggregates and details separately.
    vc_agg = sql(
        "SELECT out AS cluster, count() AS n FROM speaks_in GROUP BY out;")
    vc_agg_rows = vc_agg[0].get('result', []) if vc_agg and vc_agg[0].get('result') else []
    vc_detail = sql(
        "SELECT out AS cluster, in.path AS audio_paths, in.sender AS senders "
        "FROM speaks_in;")
    vc_detail_rows = vc_detail[0].get('result', []) if vc_detail and vc_detail[0].get('result') else []
    # Bucket details by cluster id
    vc_paths_by_cluster = {}
    vc_senders_by_cluster = {}
    for row in vc_detail_rows:
        cluster_id_str = str(row.get('cluster', ''))
        paths = _flatten(row.get('audio_paths', []))
        senders = _flatten(row.get('senders', []))
        vc_paths_by_cluster.setdefault(cluster_id_str, set()).update(paths)
        vc_senders_by_cluster.setdefault(cluster_id_str, set()).update(senders)
    for row in vc_agg_rows:
        cluster_id_str = str(row.get('cluster', ''))
        # Guard: only accept person:voice_cluster_* out values. Inverted edges
        # (out = item:...) would otherwise have their trailing timestamp digits
        # parsed as a cluster number, creating phantom voice_cluster_154 etc.
        if not cluster_id_str.startswith('person:voice_cluster_'):
            continue
        # cluster_id_str is like 'person:voice_cluster_3' — extract the numeric part
        try:
            vc_num = int(cluster_id_str.rsplit('_', 1)[-1])
        except (ValueError, IndexError):
            continue
        name = f"voice_cluster_{vc_num}"
        audio_paths = sorted(vc_paths_by_cluster.get(cluster_id_str, set()))
        senders = sorted(vc_senders_by_cluster.get(cluster_id_str, set()))
        entities[name] = {
            'name': name,
            'kind': 'person',
            'slug': name,
            'surreal_ids': [],
            'role': 'unidentified speaker (voice cluster)',
            'notes': (f'Unidentified speaker whose voice appears in {len(audio_paths)} '
                      f'audio/video file(s). Senders: {", ".join(senders) or "unknown"}.'),
            'item_count': row.get('n', 0),
            'voice_cluster_ids': [vc_num],
            'face_cluster_ids': [],
            'aliases': set([name]),
            'text_hits': 0,
            'audio_hits': 0,
            'video_hits': 0,
            'image_count': 0,
            'is_cluster': True,
            'cluster_audio': audio_paths,
            'cluster_senders': senders,
        }

    # Filter: only entities with some evidence
    # CLUSTERS are always kept — they represent proven identities even if
    # they have no text mentions.
    scored = []
    for ent in entities.values():
        ent['evidence_score'] = (ent['text_hits'] * 3 + ent['audio_hits'] * 2 +
                                 ent['video_hits'] * 2 + ent['image_count'] * 1 +
                                 min(ent['item_count'], 50))
        is_cluster = ent.get('is_cluster', False)
        passes = (
            is_cluster or
            ent['evidence_score'] >= 2 or
            ent['item_count'] >= 5 or
            ent['name'] in (
                'Grady', '(Grady)The Pagan of Montana', 'Scott Ernest', 'Scott',
                'Christopher Anthony Semok', 'Christopher Semok', 'Cletus',
                'CPUSA', 'Patriot Front', 'White Lives Matter', 'Antifa',
                'Flathead County', 'Will', 'Christian Piccolini')
        )
        if passes:
            ent['aliases'] = sorted(ent['aliases'])
            scored.append(ent)

    scored.sort(key=lambda e: e['evidence_score'], reverse=True)
    return scored


def gather_evidence(ent, run_dir, arts, data_root):
    """Gather all evidence (text excerpts, media files) for one entity."""
    aliases_lower = [a.lower() for a in ent['aliases']] or [ent['name'].lower()]
    evidence = {
        'text_excerpts': [],     # (sender, text, item_id) — proven by text search
        'cluster_photos': [],    # photos containing entity's face cluster (proven)
        'cluster_audio': [],     # audio of entity's voice cluster (proven)
        'audio_excerpts': [],    # transcript excerpts that mention the entity
        'video_files': [],       # video summaries mentioning the entity
    }

    # =====================================================================
    # PROVEN ATTRIBUTION ONLY — no proximity heuristics.
    #
    # The previous version put random "nearby" photos in each entity folder,
    # which was misleading: a photo posted within ±3 messages of a Semok
    # mention is almost never a photo OF Semok.
    #
    # THE CLUSTERING SYSTEM IS THE SOLE DETERMINER of which media appears
    # in an entity's folder:
    #   - Face cluster → photos OF this person (proven by face biometrics)
    #   - Voice cluster → audio OF this person speaking (proven by voice
    #     biometrics)
    #
    # Sender attribution ("who posted this") is NOT a media signal — it
    # answers "who sent this" not "who is this a photo of". Mixing them
    # just recreates the random-photos problem. DON'T DO IT.
    #
    # Face/voice clusters that have NO name yet appear as their own pseudo-
    # entities (face_cluster_N, voice_cluster_N) with all their media.
    # =====================================================================

    # --- (A) Text mentions ---
    res = sql("SELECT id, sender, type, path, text FROM item WHERE text != NONE;")
    items = res[0].get('result', []) if res and res[0].get('result') else []
    for item in items:
        text = item.get('text', '') or ''
        lower = (text or '').lower()
        if lower and any(a in lower for a in aliases_lower) and len(text.strip()) > 10:
            sender = item.get('sender', '?')
            for a in aliases_lower:
                idx = lower.find(a)
                if idx >= 0:
                    start = max(0, idx - 80)
                    end = min(len(text), idx + 200)
                    evidence['text_excerpts'].append(
                        (sender, text[start:end].replace('\n', ' ').strip(),
                         str(item.get('id', ''))))
                    break

    # --- (B) Cluster-linked media (THE determiner) ---
    # Fires when the entity has face_cluster_ids or voice_cluster_ids
    # linked to it. For pseudo-entities the list has one entry (their own
    # cluster). For named entities it has every cluster linked via same_as.
    if ent.get('face_cluster_ids'):
        _seen_photos = set()  # dedup by filename
        # Use precomputed list if this is a cluster pseudo-entity
        if ent.get('cluster_photos'):
            for p in ent['cluster_photos']:
                src = find_media(p, data_root)
                if src:
                    fname = Path(src).name
                    if fname not in _seen_photos:
                        _seen_photos.add(fname)
                        evidence['cluster_photos'].append(src)
        else:
            for cid in ent['face_cluster_ids']:
                try:
                    cid_i = int(cid)
                except (TypeError, ValueError):
                    continue
                cres = sql(
                    f"SELECT ->appears_in->item.path AS p FROM face_appearance "
                    f"WHERE cluster_id = {cid_i};")
                crows = cres[0].get('result', []) if cres and cres[0].get('result') else []
                for row in crows:
                    paths = row.get('p', []) or []
                    if isinstance(paths, str):
                        paths = [paths]
                    for p in paths:
                        src = find_media(p, data_root)
                        if src:
                            fname = Path(src).name
                            if fname not in _seen_photos:
                                _seen_photos.add(fname)
                                evidence['cluster_photos'].append(src)

    if ent.get('voice_cluster_ids'):
        _seen_audio = set()  # dedup by filename — _link also dedups, but this
                             # keeps evidence counts honest (no phantom duplicates
                             # from multi-embedding video items creating 2x edges)
        if ent.get('cluster_audio'):
            for p in ent['cluster_audio']:
                src = find_media(p, data_root)
                if src:
                    fname = Path(src).name
                    if fname not in _seen_audio:
                        _seen_audio.add(fname)
                        evidence['cluster_audio'].append((p, src))
        else:
            for vc in ent['voice_cluster_ids']:
                try:
                    vc_i = int(vc)
                except (TypeError, ValueError):
                    continue
                vres = sql(
                    f"SELECT in.path AS p FROM speaks_in WHERE out = person:voice_cluster_{vc_i};")
                vrows = vres[0].get('result', []) if vres and vres[0].get('result') else []
                for row in vrows:
                    paths = row.get('p', []) or []
                    if isinstance(paths, str):
                        paths = [paths]
                    for p in paths:
                        src = find_media(p, data_root)
                        if src:
                            fname = Path(src).name
                            if fname not in _seen_audio:
                                _seen_audio.add(fname)
                                evidence['cluster_audio'].append((p, src))

    # --- Audio excerpts from corpus ---
    corpus = arts.get('all_audio_corpus.json', [])
    if isinstance(corpus, dict):
        corpus = list(corpus.values())
    for entry in corpus:
        if not isinstance(entry, dict):
            continue
        text = entry.get('text', '') or entry.get('transcript', '') or ''
        lower = text.lower()
        if any(a in lower for a in aliases_lower):
            fname = entry.get('file', entry.get('id', '?'))
            for a in aliases_lower:
                idx = lower.find(a)
                if idx >= 0:
                    start = max(0, idx - 80)
                    end = min(len(text), idx + 200)
                    evidence['audio_excerpts'].append(
                        (fname, text[start:end].replace('\n', ' ').strip()))
                    break

    # --- Video evidence ---
    # Search TWO sources:
    #   1. video_summaries.json — per-video narratives (field is `narrative`,
    #      NOT `summary` — that was the old bug that dropped ALL video evidence)
    #   2. video_frame_analysis.json — per-keyframe VLM descriptions (field
    #      is `description`). This catches entities that appear in a single
    #      frame even if the overall narrative doesn't name them.
    seen_video = set()
    video_sums = arts.get('video_summaries.json', {})
    if isinstance(video_sums, dict):
        for vid, vdata in video_sums.items():
            if not isinstance(vdata, dict):
                continue
            # Try narrative (the real field), fall back to summary
            narrative = vdata.get('narrative', '') or vdata.get('summary', '') or ''
            lower = narrative.lower()
            if any(a in lower for a in aliases_lower):
                for a in aliases_lower:
                    idx = lower.find(a)
                    if idx >= 0:
                        start = max(0, idx - 100)
                        end = min(len(narrative), idx + 300)
                        key = (vid, a)
                        if key not in seen_video:
                            evidence['video_files'].append(
                                (vid, narrative[start:end].replace('\n', ' ').strip()))
                            seen_video.add(key)
                        break

    # Also search video_frame_analysis.json — VLM descriptions of keyframes.
    # These often catch entity names that the high-level narrative missed.
    vfa = arts.get('video_frame_analysis.json', [])
    if isinstance(vfa, dict):
        vfa = list(vfa.values())
    for entry in vfa:
        if not isinstance(entry, dict):
            continue
        desc = entry.get('description', '') or ''
        lower = desc.lower()
        if not any(a in lower for a in aliases_lower):
            continue
        vid = entry.get('file', entry.get('video', entry.get('id', '?')))
        for a in aliases_lower:
            idx = lower.find(a)
            if idx >= 0:
                start = max(0, idx - 100)
                end = min(len(desc), idx + 300)
                # Dedup by (video, alias) — a video can have many keyframes
                # all describing the same scene; one excerpt per alias is enough.
                key = (vid, a)
                if key not in seen_video:
                    evidence['video_files'].append(
                        (vid, f"[keyframe] {desc[start:end].replace(chr(10), ' ').strip()}"))
                    seen_video.add(key)
                break

    return evidence


def write_dossier(ent, evidence, out_dir, data_root):
    """Write the entity's markdown dossier + symlink media.

    Layout — clustering is the SOLE determiner of which media appears:
        photos/   — photos containing this entity's face (face cluster)
        audio/    — audio of this entity speaking (voice cluster)
        videos/   — videos whose VLM description mentions the entity
        text/     — plaintext mentions + transcript excerpts
    """
    ent_dir = out_dir / ent['slug']
    ent_dir.mkdir(parents=True, exist_ok=True)

    # Create subfolders lazily — only when they'll actually hold content.
    def _ensure(subdir):
        (ent_dir / subdir).mkdir(parents=True, exist_ok=True)

    # Symlink helper — ABSOLUTE targets so the entity tree is relocatable
    def _link(src_path, dst_dir):
        dst = dst_dir / Path(src_path).name
        if dst.exists() or dst.is_symlink():
            dst.unlink()
        try:
            dst.symlink_to(os.path.abspath(src_path))
            return f"{dst_dir.relative_to(ent_dir)}/{Path(src_path).name}"
        except Exception:
            return None

    photo_links = []
    if evidence.get('cluster_photos'):
        _ensure('photos')
        for src in evidence.get('cluster_photos', []):
            link = _link(src, ent_dir / 'photos')
            if link:
                photo_links.append(link)

    audio_links = []
    if evidence.get('cluster_audio'):
        _ensure('audio')
        for fname, src in evidence.get('cluster_audio', []):
            link = _link(src, ent_dir / 'audio')
            if link:
                audio_links.append(link)

    # Write text excerpts to files
    text_files = []
    if evidence['text_excerpts']:
        _ensure('text')
        tf = ent_dir / 'text' / 'mentions.md'
        lines = [f"# Text mentions of {ent['name']}\n"]
        for sender, excerpt, item_id in evidence['text_excerpts'][:50]:
            lines.append(f"## [{sender}] ({item_id})\n\n> {excerpt}\n")
        tf.write_text('\n'.join(lines))
        text_files.append('text/mentions.md')

    if evidence['audio_excerpts']:
        _ensure('text')
        af = ent_dir / 'text' / 'audio_mentions.md'
        lines = [f"# Audio transcript mentions of {ent['name']}\n"]
        for fname, excerpt in evidence['audio_excerpts'][:30]:
            lines.append(f"## {fname}\n\n> {excerpt}\n")
        af.write_text('\n'.join(lines))
        text_files.append('text/audio_mentions.md')

    # Write the dossier markdown
    kind_icon = {'person': '👤', 'organization': '🏛️',
                 'location': '📍', 'topic': '🏷️'}.get(ent['kind'], '📄')
    md = []
    md.append(f"# {kind_icon} {ent['name']}\n")
    md.append(f"**Kind:** {ent['kind']}  ")
    md.append(f"**Evidence score:** {ent['evidence_score']}  ")
    if ent['aliases']:
        md.append(f"**Aliases/Known-as:** {', '.join(ent['aliases'])}\n")
    else:
        md.append("")

    md.append("## Summary\n")
    if ent['notes']:
        md.append(f"{ent['notes']}\n")
    if ent['role']:
        md.append(f"**Role:** {ent['role']}  ")
    if ent['item_count']:
        md.append(f"**Items authored:** {ent['item_count']}\n")

    md.append("## Evidence Coverage\n")
    md.append("| Signal | Count | Source |\n|--------|-------|--------|\n")
    md.append(f"| Text mentions | {len(evidence['text_excerpts'])} | search of item.text |\n")
    md.append(f"| Audio transcript mentions | {len(evidence['audio_excerpts'])} | search of transcripts |\n")
    md.append(f"| Video mentions | {len(evidence['video_files'])} | VLM description of video keyframes |\n")
    md.append(f"| Photos (face cluster) | {len(photo_links)} | face biometric clustering |\n")
    md.append(f"| Audio (voice cluster) | {len(audio_links)} | voice biometric clustering |\n")

    if evidence['text_excerpts']:
        md.append("\n## Text Evidence (top excerpts)\n")
        for sender, excerpt, item_id in evidence['text_excerpts'][:10]:
            md.append(f"- **[{sender}]** \"{excerpt}\"  \n  `[{item_id}]`\n")

    if evidence['audio_excerpts']:
        md.append("\n## Audio Evidence (transcript excerpts)\n")
        for fname, excerpt in evidence['audio_excerpts'][:10]:
            md.append(f"- **{fname}:** \"{excerpt}\"\n")

    # Video evidence — symlink the actual files so a human can play them,
    # not just read the VLM description. Find each video by name under data/.
    video_links = []
    if evidence['video_files']:
        _ensure('videos')
        md.append("\n## Video Evidence\n")
        for vid, excerpt in evidence['video_files']:
            md.append(f"- **{vid}:** {excerpt}\n")
            # Try to find and symlink the actual video file
            src = find_media(vid, data_root)
            if src:
                link = _link(src, ent_dir / 'videos')
                if link:
                    video_links.append(link)

    # Section: photos OF this entity (face cluster proven)
    if photo_links:
        md.append(f"\n## Photos OF {ent['name']} ({len(photo_links)})\n")
        md.append("_Face cluster: biometric clustering proves this person appears in these photos._\n")
        for link in photo_links[:30]:
            md.append(f"![]({link})\n")

    # Section: audio OF this entity (voice cluster proven)
    if audio_links:
        md.append(f"\n## Audio OF {ent['name']} ({len(audio_links)})\n")
        md.append("_Voice cluster: biometric clustering proves this person speaks in these files._\n")
        for link in audio_links:
            md.append(f"- [{link}]({link})\n")

    if text_files:
        md.append("\n## Full Evidence Files\n")
        for tf in text_files:
            md.append(f"- [{tf}]({tf})\n")

    # Identity confidence note for resolved identities
    vc_ids = ent.get('voice_cluster_ids') or []
    fc_ids = ent.get('face_cluster_ids') or []
    if vc_ids or fc_ids:
        md.append("\n## Identity Resolution\n")
        if fc_ids:
            md.append(f"Face cluster ID(s): **{', '.join(str(c) for c in fc_ids)}**  \n")
        if vc_ids:
            md.append(f"Voice cluster ID(s): **{', '.join(str(c) for c in vc_ids)}**  \n")

    dossier_path = ent_dir / f"{ent['slug']}.md"
    dossier_path.write_text('\n'.join(md))
    return dossier_path


def main():
    run_dir = Path(os.environ.get("RUN_DIR",
        str(Path(__file__).parent.parent / "artifacts" / "run-2026-07-12")))
    if len(sys.argv) > 1:
        run_dir = Path(sys.argv[1])

    print(f"=== build_entity_dossiers: {run_dir} ===\n")

    # Find data root (walk up to find data/)
    workspace = run_dir
    for _ in range(6):
        if (workspace / "data").is_dir():
            break
        workspace = workspace.parent
    data_root = workspace / "data"
    if not data_root.is_dir():
        print(f"ERROR: no data/ dir found near {run_dir}")
        sys.exit(1)
    print(f"data root: {data_root}")

    # Load artifacts
    arts = load_artifacts(run_dir)
    print(f"loaded {len(arts)} artifacts: {', '.join(arts.keys())}")

    # Discover entities
    print("\n[1/3] Discovering entities...")
    entities = discover_entities(run_dir, arts)
    print(f"  {len(entities)} entities with evidence >= 2")

    # Wipe and recreate entities output dir
    out_dir = run_dir / "entities"
    # Preserve ONLY raw modality output under entities/raw/. We do NOT
    # blindly preserve everything — old entity folders (which may carry
    # the legacy sent_by/ or appears_in/ layout from prior runs) must be
    # discarded so the fresh build is authoritative. An explicit allowlist
    # keeps provenance without inheriting stale folder structures.
    RAW_ALLOWLIST = {
        'face_clusters', 'voice_clusters', 'text_and_scenes',
        'video_frames', 'audio_frames', 'scenes',
        'items.json', 'face_analysis.json', 'face_clusters.json',
        'voice_clusters.json', 'audio_summaries.json',
        'all_audio_corpus.json', 'audio_chunks.json',
        'video_frame_analysis.json', 'video_summaries.json',
        'content_clusters.json', 'transcripts.json',
    }
    raw_backup = run_dir / "entities_raw_backup"
    if out_dir.exists():
        if raw_backup.exists():
            shutil.rmtree(raw_backup)
        shutil.move(str(out_dir), str(raw_backup))
    out_dir.mkdir(parents=True, exist_ok=True)

    # Restore ONLY allowlisted modality items under raw/. Anything else
    # (old entity slug folders, clusters/, index.md, sent_by/, appears_in/,
    # nested raw/) is dropped — it gets regenerated fresh or is legacy.
    if raw_backup.exists():
        raw_dir = out_dir / "raw"
        raw_dir.mkdir(exist_ok=True)
        for item in raw_backup.iterdir():
            if item.name not in RAW_ALLOWLIST:
                continue
            dst = raw_dir / item.name
            if dst.exists():
                if dst.is_dir():
                    shutil.rmtree(dst)
                else:
                    dst.unlink()
            shutil.move(str(item), str(dst))
        shutil.rmtree(raw_backup)

    # Split entities into major (full folders) vs minor (collapsed into other/).
    #
    # Major = entities with substantial cross-modal evidence:
    #   - evidence_score >= 10 (multiple text hits, or text + audio + video)
    #   - OR has a face cluster (images)
    #   - OR is in the known-major hardcoded alias set
    #
    # Minor = everything else. These get a single line in other/minor_entities.md.
    # The ~30 major entities get full dossiers with media.
    MAJOR_THRESHOLD = 10
    KNOWN_MAJOR = {
        'Grady', '(Grady)The Pagan of Montana', 'Scott Ernest', 'Scott',
        'Christopher Anthony Semok', 'Christopher Semok', 'Cletus',
        'CPUSA', 'Patriot Front', 'White Lives Matter', 'Antifa',
        'Flathead County', 'Will', 'Christian Piccolini',
    }
    major_entities = []
    minor_entities = []
    for e in entities:
        is_major = (
            e['evidence_score'] >= MAJOR_THRESHOLD or
            bool(e.get('face_cluster_ids')) or
            bool(e.get('voice_cluster_ids')) or
            e.get('is_cluster') or
            e['name'] in KNOWN_MAJOR
        )
        if is_major:
            major_entities.append(e)
        else:
            minor_entities.append(e)

    print(f"\n[2/3] Building {len(major_entities)} major entity dossiers "
          f"(score >= {MAJOR_THRESHOLD}, face cluster, or known major)...")
    if minor_entities:
        print(f"  {len(minor_entities)} minor entities will be consolidated "
              f"into other/minor_entities.md")

    built = []
    # Cluster pseudo-entities go into clusters/ subfolder so the top-level
    # surface shows ONLY named entities (the human-browseable set). The
    # agent still reads clusters during identity resolution; this is purely
    # a layout fix so a human opening entities/ isn't flooded with 26
    # face_cluster_N / voice_cluster_N folders mixed in with the 10 real
    # named entities.
    clusters_dir = out_dir / "clusters"
    for ent in major_entities:
        evidence = gather_evidence(ent, run_dir, arts, data_root)
        ent_out_dir = clusters_dir if ent.get('is_cluster') else out_dir
        dossier = write_dossier(ent, evidence, ent_out_dir, data_root)
        built.append((ent, evidence, dossier))
        kind = ent['kind'][:4]
        n_clu_img = len(evidence.get('cluster_photos', []))
        n_clu_aud = len(evidence.get('cluster_audio', []))
        print(f"  [{kind}] {ent['name']:42s} score={ent['evidence_score']:4d}  "
              f"text={len(evidence['text_excerpts']):3d}  "
              f"photos={n_clu_img:3d} audio={n_clu_aud:3d}")

    # Write minor entities into a single consolidated file
    if minor_entities:
        other_dir = out_dir / "other"
        other_dir.mkdir(parents=True, exist_ok=True)
        minor_lines = [
            f"# Minor Entities — {run_dir.name}\n",
            f"**{len(minor_entities)} entities** with low evidence scores.\n",
            "These entities were mentioned in the source data but did not meet the "
            f"evidence threshold (score >= {MAJOR_THRESHOLD}) for a full dossier.\n",
            "They are listed here for completeness and searchability.\n",
            "| Entity | Kind | Score | Aliases |\n",
            "|--------|------|-------|---------|\n",
        ]
        for ent in sorted(minor_entities, key=lambda e: e['evidence_score'], reverse=True):
            aliases = ', '.join(ent.get('aliases', [])) if ent.get('aliases') else '—'
            minor_lines.append(
                f"| {ent['name']} | {ent['kind']} | {ent['evidence_score']} | {aliases} |\n"
            )
        (other_dir / "minor_entities.md").write_text(''.join(minor_lines))
        print(f"  Wrote {len(minor_entities)} minor entities to other/minor_entities.md")

    # Write master index
    print("\n[3/3] Writing master index...")
    named_built = [(e, ev, d) for e, ev, d in built if not e.get('is_cluster')]
    cluster_built = [(e, ev, d) for e, ev, d in built if e.get('is_cluster')]
    index_lines = [f"# Entity Index — {run_dir.name}\n"]
    index_lines.append(f"**{len(named_built)} named entities** + "
                       f"{len(cluster_built)} unresolved clusters "
                       f"({len(minor_entities)} minor in "
                       f"[other/](other/minor_entities.md)).\n")
    index_lines.append("Named entities have full dossiers. Clusters "
                       "(under [clusters/](clusters/)) are proven-same-face/voice "
                       "sets whose names haven't been resolved yet.\n")

    by_kind = defaultdict(list)
    for ent, ev, doss in named_built:
        by_kind[ent['kind']].append((ent, ev, doss))

    for kind in ('person', 'organization', 'location', 'topic'):
        items = by_kind.get(kind, [])
        if not items:
            continue
        kind_icon = {'person': '👤 People', 'organization': '🏛️ Organizations',
                     'location': '📍 Locations', 'topic': '🏷️ Topics'}[kind]
        index_lines.append(f"\n## {kind_icon} ({len(items)})\n")
        index_lines.append(
            "| Entity | Evidence | Text | Photos | Audio | Dossier |\n")
        index_lines.append(
            "|--------|----------|------|--------|-------|---------|\n")
        for ent, ev, doss in items:
            rel = doss.relative_to(out_dir)
            # Full relative path (e.g. "clusters/face_cluster_0/face_cluster_0.md")
            # so links work whether the entity is top-level or under clusters/.
            link_path = rel.as_posix()
            index_lines.append(
                f"| {ent['name']} | {ent['evidence_score']} | "
                f"{len(ev['text_excerpts'])} | "
                f"{len(ev.get('cluster_photos', []))} | "
                f"{len(ev.get('cluster_audio', []))} | "
                f"[→ {ent['slug']}/]({link_path}) |\n")

    # Cluster section — proven-same-face/voice sets awaiting identity resolution
    if cluster_built:
        index_lines.append("\n---\n\n## 🔬 Unresolved Clusters "
                           f"({len(cluster_built)})\n")
        index_lines.append("Proven-same-face/voice sets whose names haven't "
                           "been linked to a named entity yet. An investigator "
                           "(or the DRE agent) proposes labels via "
                           "`.deepagents/skills/deep-research/scripts/link_cluster.py`; see AGENTS.md Step 2.\n")
        index_lines.append("\n| Cluster | Kind | Photos | Audio | Dossier |\n")
        index_lines.append("|---------|------|--------|-------|---------|\n")
        for ent, ev, doss in sorted(cluster_built, key=lambda x: x[0]['name']):
            rel = doss.relative_to(out_dir)
            kind_label = 'face' if ent['name'].startswith('face_') else 'voice'
            n_photos = len(ev.get('cluster_photos', []))
            n_audio = len(ev.get('cluster_audio', []))
            index_lines.append(
                f"| {ent['name']} | {kind_label} | {n_photos} | {n_audio} | "
                f"[→ {ent['name']}/]({rel.as_posix()}) |\n")

    index_lines.append("\n---\n\n## Raw Modality Output\n")
    index_lines.append("Original pipeline-stage outputs preserved under "
                       "[raw/](raw/) for provenance.\n")

    if minor_entities:
        index_lines.append("\n## Minor Entities\n")
        index_lines.append(f"{len(minor_entities)} low-evidence entities in "
                           f"[other/minor_entities.md](other/minor_entities.md).\n")

    (out_dir / "index.md").write_text(''.join(index_lines))
    print(f"\n=== DONE: {len(built)} major dossiers + {len(minor_entities)} minor "
          f"in {out_dir} ===")
    print(f"  index: {out_dir / 'index.md'}")


if __name__ == "__main__":
    main()
