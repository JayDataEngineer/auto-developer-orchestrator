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
            images/                          # photos this person appears in
            videos/                          # videos mentioning/showing them
            audio/                           # audio where they speak/are mentioned
            text/                            # plaintext messages about them
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

URL = os.environ.get("SURREALDB_URL", "http://127.0.0.1:8000/sql").replace("/mcp", "/sql")
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
                'voice_cluster_id': row.get('voice_cluster_id'),
                'face_cluster_id': row.get('face_cluster_id'),
                'aliases': set(),
                'text_hits': 0,
                'audio_hits': 0,
                'video_hits': 0,
                'image_count': 0,
            }

    # --- Build alias map for key entities (hardcoded intelligence from the data) ---
    # These are known aliases discovered in the report + transcripts. Adding them
    # here means the evidence search will find ALL variants.
    alias_map = {
        'Grady': ['Grady', '(Grady)The Pagan of Montana', '(Grady) The Pagan of Montana',
                  'The Pagan of Montana', 'Primary Speaker', 'City Councilor',
                  'councilor', 'Marxist-Leninist'],
        '(Grady)The Pagan of Montana': ['Grady', '(Grady)The Pagan of Montana',
                  '(Grady) The Pagan of Montana', 'The Pagan of Montana',
                  'Primary Speaker', 'City Councilor'],
        'Scott': ['Scott', 'Scott Ernest', 'roommate'],
        'Scott Ernest': ['Scott', 'Scott Ernest', 'roommate'],
        'Christopher Semok': ['Christopher Semok', 'Christopher Anthony Semok',
                              'Commissar', 'Commissar ANTIFA', 'Semok'],
        'Christopher Anthony Semok': ['Christopher Semok', 'Christopher Anthony Semok',
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
        # Find the entity by any of its names
        matched = None
        for alias in [ent_name] + aliases:
            if alias in entities:
                matched = entities[alias]
                break
        if matched:
            matched['aliases'].update(aliases)
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
                'voice_cluster_id': None,
                'face_cluster_id': None,
                'aliases': set(aliases),
                'text_hits': 0,
                'audio_hits': 0,
                'video_hits': 0,
                'image_count': 0,
            }

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
        # Video hits
        for entry in video_sums:
            if not isinstance(entry, dict):
                continue
            text = (entry.get('summary', '') or '').lower()
            if any(a in text for a in aliases_lower):
                ent['video_hits'] += 1
                break
        # Image count (for people with face clusters)
        if ent['face_cluster_id'] is not None:
            fc = arts.get('face_clusters.json', {})
            fa = arts.get('face_analysis.json', [])
            if isinstance(fa, list):
                # count photos in this cluster
                labels = fc.get('labels', [])
                ordered = []
                for entry in fa:
                    embs = entry.get('embeddings') or []
                    for _ in range(len(embs)):
                        ordered.append(entry['photo'])
                ent['image_count'] = sum(1 for i, l in enumerate(labels)
                                         if l == ent['face_cluster_id'] and i < len(ordered))

    # Filter: only entities with some evidence
    scored = []
    for ent in entities.values():
        ent['evidence_score'] = (ent['text_hits'] * 3 + ent['audio_hits'] * 2 +
                                 ent['video_hits'] * 2 + ent['image_count'] * 1 +
                                 min(ent['item_count'], 50))
        if ent['evidence_score'] >= 2 or ent['item_count'] >= 5 or ent['name'] in (
            'Grady', '(Grady)The Pagan of Montana', 'Scott Ernest', 'Scott',
            'Christopher Anthony Semok', 'Christopher Semok', 'Cletus',
            'CPUSA', 'Patriot Front', 'White Lives Matter', 'Antifa',
            'Flathead County', 'Will', 'Christian Piccolini'):
            ent['aliases'] = sorted(ent['aliases'])
            scored.append(ent)

    scored.sort(key=lambda e: e['evidence_score'], reverse=True)
    return scored


def gather_evidence(ent, run_dir, arts, data_root):
    """Gather all evidence (text excerpts, media files) for one entity."""
    aliases_lower = [a.lower() for a in ent['aliases']] or [ent['name'].lower()]
    evidence = {
        'text_excerpts': [],   # (sender, text, item_id)
        'audio_files': [],     # (filename, excerpt, source_path)
        'video_files': [],     # (video_id, excerpt, summary)
        'images': [],          # paths
        'audio_excerpts': [],  # (file, excerpt)
    }

    # --- Text excerpts from items ---
    res = sql("SELECT id, sender, text FROM item WHERE text != NONE;")
    items = res[0].get('result', []) if res and res[0].get('result') else []
    for item in items:
        text = item.get('text', '') or ''
        lower = text.lower()
        if any(a in lower for a in aliases_lower) and len(text.strip()) > 10:
            sender = item.get('sender', '?')
            # Find the alias that matched for highlighting
            for a in aliases_lower:
                idx = lower.find(a)
                if idx >= 0:
                    start = max(0, idx - 80)
                    end = min(len(text), idx + 200)
                    evidence['text_excerpts'].append(
                        (sender, text[start:end].replace('\n', ' ').strip(),
                         str(item.get('id', ''))))
                    break

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
            # Find the actual audio file
            src = find_media(fname, data_root)
            if src:
                evidence['audio_files'].append((fname, src))

    # --- Video evidence ---
    video_sums = arts.get('video_summaries.json', {})
    if isinstance(video_sums, dict):
        for vid, vdata in video_sums.items():
            summary = vdata.get('summary', '') if isinstance(vdata, dict) else ''
            lower = summary.lower()
            if any(a in lower for a in aliases_lower):
                for a in aliases_lower:
                    idx = lower.find(a)
                    if idx >= 0:
                        start = max(0, idx - 100)
                        end = min(len(summary), idx + 300)
                        evidence['video_files'].append(
                            (vid, summary[start:end].replace('\n', ' ').strip()))
                        break

    # --- Images (for people with face clusters) ---
    if ent['face_cluster_id'] is not None:
        fa = arts.get('face_analysis.json', [])
        fc = arts.get('face_clusters.json', {})
        labels = fc.get('labels', [])
        ordered_photos = []
        for entry in fa:
            embs = entry.get('embeddings') or []
            for _ in range(len(embs)):
                ordered_photos.append(entry['photo'])
        for i, label in enumerate(labels):
            if label == ent['face_cluster_id'] and i < len(ordered_photos):
                photo = ordered_photos[i]
                src = find_media(photo, data_root)
                if src:
                    evidence['images'].append(src)

    return evidence


def write_dossier(ent, evidence, out_dir, data_root):
    """Write the entity's markdown dossier + symlink media."""
    ent_dir = out_dir / ent['slug']
    ent_dir.mkdir(parents=True, exist_ok=True)

    # Create media subdirs and symlink
    for subdir in ('images', 'videos', 'audio', 'text'):
        (ent_dir / subdir).mkdir(exist_ok=True)

    # Symlink images
    img_links = []
    for i, src in enumerate(evidence['images']):
        dst = ent_dir / 'images' / src.name
        if dst.exists() or dst.is_symlink():
            dst.unlink()
        try:
            dst.symlink_to(os.path.relpath(src, dst.parent))
            img_links.append(f"images/{src.name}")
        except Exception:
            pass

    # Symlink audio
    audio_links = []
    for fname, src in evidence['audio_files']:
        dst = ent_dir / 'audio' / Path(src).name
        if dst.exists() or dst.is_symlink():
            dst.unlink()
        try:
            dst.symlink_to(os.path.relpath(src, dst.parent))
            audio_links.append(f"audio/{Path(src).name}")
        except Exception:
            pass

    # Write text excerpts to files
    text_files = []
    if evidence['text_excerpts']:
        tf = ent_dir / 'text' / 'mentions.md'
        lines = [f"# Text mentions of {ent['name']}\n"]
        for sender, excerpt, item_id in evidence['text_excerpts'][:50]:
            lines.append(f"## [{sender}] ({item_id})\n\n> {excerpt}\n")
        tf.write_text('\n'.join(lines))
        text_files.append('text/mentions.md')

    if evidence['audio_excerpts']:
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
    md.append(f"| Modality | Hits |\n|----------|------|\n")
    md.append(f"| Text mentions | {len(evidence['text_excerpts'])} |\n")
    md.append(f"| Audio mentions | {len(evidence['audio_excerpts'])} |\n")
    md.append(f"| Video mentions | {len(evidence['video_files'])} |\n")
    md.append(f"| Images (face cluster) | {len(evidence['images'])} |\n")

    if evidence['text_excerpts']:
        md.append("\n## Text Evidence (top excerpts)\n")
        for sender, excerpt, item_id in evidence['text_excerpts'][:10]:
            md.append(f"- **[{sender}]** \"{excerpt}\"  \n  `[{item_id}]`\n")

    if evidence['audio_excerpts']:
        md.append("\n## Audio Evidence (transcript excerpts)\n")
        for fname, excerpt in evidence['audio_excerpts'][:10]:
            md.append(f"- **{fname}:** \"{excerpt}\"\n")

    if evidence['video_files']:
        md.append("\n## Video Evidence\n")
        for vid, excerpt in evidence['video_files']:
            md.append(f"- **{vid}:** {excerpt}\n")

    if img_links:
        md.append("\n## Images\n")
        for link in img_links[:20]:
            md.append(f"![{ent['name']}]({link})\n")

    if audio_links:
        md.append("\n## Audio Files\n")
        for link in audio_links:
            md.append(f"- [{link}]({link})\n")

    if text_files:
        md.append("\n## Full Evidence Files\n")
        for tf in text_files:
            md.append(f"- [{tf}]({tf})\n")

    # Identity confidence note for resolved identities
    if ent.get('voice_cluster_id') is not None:
        md.append("\n## Identity Resolution\n")
        md.append(f"Voice cluster ID: **{ent['voice_cluster_id']}**  \n")
        md.append("⚠️ Voice→sender attribution is based on co-occurrence, NOT voice "
                  "biometrics. The sender posted the audio; the speaker may or may not "
                  "be the sender. Third-person references in transcripts can indicate "
                  "the speaker is a different person.\n")

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
    # Preserve raw modality output under entities/raw/
    raw_backup = run_dir / "entities_raw_backup"
    if out_dir.exists():
        if raw_backup.exists():
            shutil.rmtree(raw_backup)
        shutil.move(str(out_dir), str(raw_backup))
    out_dir.mkdir(parents=True, exist_ok=True)

    # Restore raw modality folders under raw/
    if raw_backup.exists():
        raw_dir = out_dir / "raw"
        raw_dir.mkdir(exist_ok=True)
        for item in raw_backup.iterdir():
            dst = raw_dir / item.name
            if dst.exists():
                if dst.is_dir():
                    shutil.rmtree(dst)
                else:
                    dst.unlink()
            shutil.move(str(item), str(dst))
        raw_backup.rmdir()

    # Build dossiers
    print(f"\n[2/3] Building {len(entities)} entity dossiers...")
    built = []
    for ent in entities:
        evidence = gather_evidence(ent, run_dir, arts, data_root)
        dossier = write_dossier(ent, evidence, out_dir, data_root)
        built.append((ent, evidence, dossier))
        kind = ent['kind'][:4]
        print(f"  [{kind}] {ent['name']:40s} score={ent['evidence_score']:4d}  "
              f"text={len(evidence['text_excerpts']):3d} "
              f"audio={len(evidence['audio_excerpts']):3d} "
              f"video={len(evidence['video_files']):2d} "
              f"img={len(evidence['images']):3d}")

    # Write master index
    print(f"\n[3/3] Writing master index...")
    index_lines = [f"# Entity Index — {run_dir.name}\n"]
    index_lines.append(f"**{len(built)} entities** organized by subject.\n")
    index_lines.append("Each folder contains a full dossier (.md) + associated "
                       "images, videos, audio, and text.\n")

    by_kind = defaultdict(list)
    for ent, ev, doss in built:
        by_kind[ent['kind']].append((ent, ev, doss))

    for kind in ('person', 'organization', 'location', 'topic'):
        items = by_kind.get(kind, [])
        if not items:
            continue
        kind_icon = {'person': '👤 People', 'organization': '🏛️ Organizations',
                     'location': '📍 Locations', 'topic': '🏷️ Topics'}[kind]
        index_lines.append(f"\n## {kind_icon} ({len(items)})\n")
        index_lines.append("| Entity | Evidence | Text | Audio | Video | Images | Dossier |\n")
        index_lines.append("|--------|----------|------|-------|-------|--------|---------|\n")
        for ent, ev, doss in items:
            rel = doss.relative_to(out_dir)
            index_lines.append(
                f"| {ent['name']} | {ent['evidence_score']} | "
                f"{len(ev['text_excerpts'])} | {len(ev['audio_excerpts'])} | "
                f"{len(ev['video_files'])} | {len(ev['images'])} | "
                f"[→ {ent['slug']}/]({rel.parent.name}/{rel.name}) |\n")

    index_lines.append(f"\n---\n\n## Raw Modality Output\n")
    index_lines.append("Original pipeline-stage outputs preserved under "
                       "[raw/](raw/) for provenance.\n")

    (out_dir / "index.md").write_text(''.join(index_lines))
    print(f"\n=== DONE: {len(built)} dossiers in {out_dir} ===")
    print(f"  index: {out_dir / 'index.md'}")


if __name__ == "__main__":
    main()
