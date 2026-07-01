#!/usr/bin/env python3
"""Context Engine — autonomous pipeline that ingests raw data and produces
an intelligence report.

Composes existing tools:
  1. telegram_parser.py  — parse export → items.json (text + media refs)
  2. audio_client.py     — batch transcribe + diarize voice messages
  3. entity_extract.py   — pull people/orgs/topics/places out of all text
  4. surreal_client.py   — write knowledge graph (entities + media + edges)
  5. LLM synthesis       — read graph + raw items → intelligence report

Output:
  work_dir/
    items.json             — parsed Telegram items
    transcripts/           — one JSON per voice file
    entities.json          — extracted entities per item
    graph.json             — flat snapshot of SurrealDB state
    intelligence_report.md — LLM-synthesized narrative report

Usage:
  python3 context_engine.py run \\
      --input data/ChatExport_2026-03-13/ \\
      --work-dir /tmp/context-engine

Env:
  LLM_API_URL   OpenAI-compatible /v1/chat/completions endpoint
  LLM_MODEL     Model name for synthesis (default: qwen35-35b-a3b-vision)
  MEDIA_MCP_URL Media MCP container (default: http://localhost:8101)

Designed to run autonomously: no agent in the loop, no human prompts.
Failures are logged + skipped; pipeline continues.
"""

import argparse
import json
import os
import subprocess
import sys
import time
import urllib.request
from pathlib import Path


SANDBOX_DIR = Path(__file__).resolve().parent


# ---------------------------------------------------------------------------
# Pipeline steps

def step_survey(input_dir: Path):
    """Print file counts by type so the operator sees what's coming."""
    print(f"\n[1/6] Survey: {input_dir}")
    if not input_dir.exists():
        sys.exit(f"input dir not found: {input_dir}")

    exts = {
        ".ogg": "audio", ".mp3": "audio", ".wav": "audio", ".flac": "audio",
        ".m4a": "audio", ".opus": "audio", ".webm": "audio",
        ".jpg": "image", ".jpeg": "image", ".png": "image", ".webp": "image",
        ".json": "text", ".html": "text",
    }
    counts = {"audio": 0, "image": 0, "text": 0, "other": 0}
    for f in input_dir.rglob("*"):
        if not f.is_file():
            continue
        ext = f.suffix.lower()
        kind = exts.get(ext)
        if kind in ("audio", "image", "text"):
            counts[kind] += 1
        elif ext == ".tgs":  # animated stickers — skip
            continue
        else:
            counts["other"] += 1
    for k, v in counts.items():
        print(f"  {k}: {v}")
    return counts


def step_parse(input_dir: Path, work_dir: Path):
    """Run telegram_parser.py → items.json.

    Falls back to empty items list if input is just a bare directory of
    audio files (no Telegram export structure). The audio step handles
    finding files via directory scan in that case.
    """
    print("\n[2/6] Parse Telegram export")
    out = work_dir / "items.json"

    # Quick check: does this look like a Telegram export?
    has_tg_structure = (
        (input_dir / "messages.html").exists() or
        (input_dir / "result.json").exists() or
        any(input_dir.glob("*.html"))
    )
    if not has_tg_structure:
        print(f"  no Telegram export structure in {input_dir}; treating as bare audio dir")
        out.write_text("[]")
        return []

    cmd = [
        sys.executable, str(SANDBOX_DIR / "telegram_parser.py"),
        "parse", "--input", str(input_dir), "--output", str(out),
    ]
    _run(cmd)
    items = json.loads(out.read_text())
    # Parser returns {"status":"ok","items":[...]} or just [...]
    if isinstance(items, dict) and "items" in items:
        items = items["items"]
    print(f"  parsed {len(items)} items")
    return items


def step_audio(items: list, work_dir: Path, input_dir: Path, timeout: int, skip: bool = False):
    """Run audio_client.py over every voice file referenced in items.

    Returns dict {voice_filename: transcript_record}.

    If skip=True, loads any cached transcripts from work_dir/transcripts/.
    """
    print("\n[3/6] Audio transcription + diarization")
    out_dir = work_dir / "transcripts"
    out_dir.mkdir(parents=True, exist_ok=True)

    # Always load cached transcripts first
    transcripts = {}
    for cached in out_dir.glob("*.json"):
        try:
            rec = json.loads(cached.read_text())
            if rec.get("transcript"):
                # Index by audio basename (audio_client.py output stores full path)
                audio_path = rec.get("audio", cached.stem)
                transcripts[Path(audio_path).name] = rec
        except Exception:
            pass
    if transcripts:
        print(f"  loaded {len(transcripts)} cached transcripts")

    if skip:
        print(f"  --skip-audio: using cached transcripts only")
        return transcripts

    # Find voice files in items, fall back to scanning input_dir
    voice_files = []
    for item in items:
        if item.get("type") == "voice":
            rel = item.get("file") or item.get("path")
            if rel:
                p = (input_dir / rel).resolve()
                if p.exists():
                    voice_files.append(p)
    if not voice_files:
        # Fallback: scan for any audio files
        exts_audio = {".ogg", ".mp3", ".wav", ".flac", ".m4a", ".opus", ".webm"}
        voice_files = sorted(
            p for p in input_dir.rglob("*") if p.suffix.lower() in exts_audio
        )

    print(f"  found {len(voice_files)} voice files (processing uncached)")

    todo = []
    for vf in voice_files:
        out_file = out_dir / (vf.stem + ".json")
        if out_file.exists() and out_file.stat().st_size > 100:
            transcripts[vf.name] = json.loads(out_file.read_text())
            continue
        todo.append((vf, out_file))

    for i, (vf, out_file) in enumerate(todo):
        print(f"  [{i+1}/{len(todo)}] processing: {vf.name}")
        cmd = [
            sys.executable, str(SANDBOX_DIR / "audio_client.py"),
            "process", "--audio", str(vf), "--output", str(out_file),
            "--timeout", str(timeout),
        ]
        try:
            _run(cmd, check=False)
            if out_file.exists():
                transcripts[vf.name] = json.loads(out_file.read_text())
            else:
                print(f"    ERROR: no output for {vf.name}")
                transcripts[vf.name] = {"audio": str(vf), "error": "no output"}
        except Exception as e:
            print(f"    ERROR: {e}")
            transcripts[vf.name] = {"audio": str(vf), "error": str(e)}

    ok = sum(1 for r in transcripts.values() if r.get("transcript"))
    print(f"  transcribed {ok}/{len(transcripts)} successfully")
    return transcripts


def step_entities(items: list, transcripts: dict, work_dir: Path):
    """Extract entities from text items AND audio transcripts."""
    print("\n[4/6] Entity extraction (LLM)")
    chunks = []
    for item in items:
        if item.get("type") == "message" and item.get("text"):
            chunks.append({
                "id": f"msg_{item.get('id', len(chunks))}",
                "content": item["text"],
                "sender": item.get("sender", "Unknown"),
                "timestamp": item.get("date") or item.get("timestamp"),
            })
    for fname, rec in transcripts.items():
        if rec.get("transcript"):
            chunks.append({
                "id": f"audio_{fname}",
                "content": rec["transcript"],
                "speakers": rec.get("speakers", []),
                "duration_sec": rec.get("duration_sec"),
            })

    out_chunks = work_dir / "text_chunks.json"
    out_chunks.write_text(json.dumps(chunks, indent=2, ensure_ascii=False))
    print(f"  {len(chunks)} text chunks to process")

    out_entities = work_dir / "entities.json"
    cmd = [
        sys.executable, str(SANDBOX_DIR / "entity_extract.py"),
        "batch", "--input", str(out_chunks), "--output", str(out_entities),
    ]
    rc = _run(cmd, check=False)
    if rc != 0 or not out_entities.exists():
        print(f"  WARN: entity extraction failed (rc={rc}); skipping")
        return []
    entities = json.loads(out_entities.read_text())
    # entity_extract.py returns {"total": N, "results": [...]}
    if isinstance(entities, dict) and "results" in entities:
        entities = entities["results"]
    print(f"  extracted entities from {len(entities)} chunks")
    return entities


def step_store(items, transcripts, entities, work_dir):
    """Write knowledge graph to SurrealDB.

    Layout:
      person        (from extracted entities — unique by name)
      organization  (from entities)
      location      (from entities)
      topic         (from entities)
      media         (every voice file with transcript)
      transcript    (full text + speaker turns)
      source        (the Telegram chat export itself)

    Edges:
      transcript->mentions->person    (if entity came from that transcript)
      transcript->mentions->organization
      transcript->mentions->location
      transcript->mentions->topic
      transcript->extracted_from->media
    """
    print("\n[5/6] Write knowledge graph to SurrealDB")
    sc = SANDBOX_DIR / "surreal_client.py"

    # Init schema (idempotent)
    _run([sys.executable, str(sc), "init-schema"], check=False)

    # Source record
    _run([sys.executable, str(sc), "insert", "--table", "source",
          "--input", json.dumps({"name": "Telegram export",
                                  "kind": "telegram",
                                  "ingested_at": _now()})], check=False)

    # Collect unique entities across all chunks
    people = {}
    orgs = {}
    locations = {}
    topics = {}

    # Each chunk in `entities` corresponds positionally to chunks we sent.
    # But we passed chunks list; entity_extract batch returns one result per chunk.
    chunks_for_entities = []
    for item in items:
        if item.get("type") == "message" and item.get("text"):
            chunks_for_entities.append({"id": f"msg_{item.get('id')}",
                                         "content": item["text"]})
    for fname, rec in transcripts.items():
        if rec.get("transcript"):
            chunks_for_entities.append({"id": f"audio_{fname}",
                                         "content": rec["transcript"]})

    for ent_result, chunk in zip(entities, chunks_for_entities):
        # entity_extract batch returns {id, content, ..., entities: {people, orgs, ...}}
        ent = ent_result.get("entities", ent_result) if isinstance(ent_result, dict) else {}
        for p in ent.get("people", []):
            people.setdefault(p.lower(), {"name": p, "mentioned_in": []})["mentioned_in"].append(chunk["id"])
        for o in ent.get("organizations", []):
            orgs.setdefault(o.lower(), {"name": o, "mentioned_in": []})["mentioned_in"].append(chunk["id"])
        for loc in ent.get("locations", []):
            locations.setdefault(loc.lower(), {"name": loc, "mentioned_in": []})["mentioned_in"].append(chunk["id"])
        for t in ent.get("topics", []):
            topics.setdefault(t.lower(), {"name": t, "mentioned_in": []})["mentioned_in"].append(chunk["id"])

    print(f"  unique: {len(people)} people, {len(orgs)} orgs, {len(locations)} locations, {len(topics)} topics")

    # Insert entities — capture returned IDs
    def _insert(table, records):
        ids = {}
        for k, rec in records.items():
            payload = {"name": rec["name"], "mention_count": len(rec["mentioned_in"])}
            cmd = [sys.executable, str(sc), "insert", "--table", table,
                   "--input", json.dumps(payload)]
            try:
                out = subprocess.check_output(cmd, text=True, stderr=subprocess.DEVNULL)
                # Output looks like [{"id":"person:abc",...}] or {"id":...}
                parsed = json.loads(out)
                if isinstance(parsed, list) and parsed:
                    ids[k] = parsed[0]["id"]
                elif isinstance(parsed, dict) and "id" in parsed:
                    ids[k] = parsed["id"]
            except Exception as e:
                print(f"    WARN: insert {table} {rec['name']}: {e}")
        return ids

    person_ids = _insert("person", people)
    org_ids = _insert("organization", orgs)
    location_ids = _insert("location", locations)
    topic_ids = _insert("topic", topics)

    # Insert transcripts + media + edges
    for fname, rec in transcripts.items():
        if not rec.get("transcript"):
            continue
        # Media record
        media_payload = {
            "filename": fname,
            "kind": "audio",
            "duration_sec": rec.get("duration_sec"),
        }
        try:
            out = subprocess.check_output(
                [sys.executable, str(sc), "insert", "--table", "media",
                 "--input", json.dumps(media_payload)],
                text=True, stderr=subprocess.DEVNULL,
            )
            parsed = json.loads(out)
            media_id = parsed[0]["id"] if isinstance(parsed, list) else parsed["id"]
        except Exception as e:
            print(f"    WARN: insert media {fname}: {e}")
            continue

        # Transcript record
        transcript_payload = {
            "text": rec["transcript"],
            "speakers": rec.get("speakers", []),
            "num_speakers": len(rec.get("speakers", [])),
            "duration_sec": rec.get("duration_sec"),
            "turns": rec.get("turns", []),
        }
        try:
            out = subprocess.check_output(
                [sys.executable, str(sc), "insert", "--table", "transcript",
                 "--input", json.dumps(transcript_payload)],
                text=True, stderr=subprocess.DEVNULL,
            )
            parsed = json.loads(out)
            t_id = parsed[0]["id"] if isinstance(parsed, list) else parsed["id"]
        except Exception as e:
            print(f"    WARN: insert transcript {fname}: {e}")
            continue

        # Edges: transcript->extracted_from->media
        _run([sys.executable, str(sc), "relate",
              "--src", t_id, "--edge", "extracted_from", "--tgt", media_id],
             check=False)

        # Edges: transcript->mentions->{person,org,location,topic}
        # Find which entities appeared in this transcript
        chunk_key = f"audio_{fname}"
        for k, pid in person_ids.items():
            if chunk_key in people[k]["mentioned_in"]:
                _relate(sc, t_id, "mentions", pid)
        for k, oid in org_ids.items():
            if chunk_key in orgs[k]["mentioned_in"]:
                _relate(sc, t_id, "mentions", oid)
        for k, lid in location_ids.items():
            if chunk_key in locations[k]["mentioned_in"]:
                _relate(sc, t_id, "mentions", lid)
        for k, tid in topic_ids.items():
            if chunk_key in topics[k]["mentioned_in"]:
                _relate(sc, t_id, "mentions", tid)

    # Final stats
    stats = _run([sys.executable, str(sc), "stats"], capture=True, check=False)
    print(f"  graph stats:\n{stats}")
    return stats


def step_synthesize(items, transcripts, entities, work_dir, model=None):
    """LLM reads the entire processed corpus and writes an intelligence report."""
    print("\n[6/6] Synthesize intelligence report (LLM)")

    # Build context: clean, deduplicated, sorted by usefulness
    voice_summaries = []
    for fname, rec in sorted(transcripts.items()):
        if rec.get("transcript"):
            voice_summaries.append({
                "file": fname,
                "duration_sec": rec.get("duration_sec"),
                "speakers": rec.get("speakers", []),
                "transcript": rec["transcript"][:2000],  # cap for context
            })

    text_messages = []
    for item in items:
        if item.get("type") == "message" and item.get("text"):
            text_messages.append({
                "sender": item.get("sender", "Unknown"),
                "date": item.get("date") or item.get("timestamp"),
                "text": item["text"][:1000],
            })

    all_people = set()
    all_orgs = set()
    all_topics = set()
    all_locations = set()
    for ent_result in entities:
        ent = ent_result.get("entities", ent_result) if isinstance(ent_result, dict) else {}
        all_people.update(ent.get("people", []))
        all_orgs.update(ent.get("organizations", []))
        all_topics.update(ent.get("topics", []))
        all_locations.update(ent.get("locations", []))

    context = {
        "corpus_summary": {
            "voice_messages": len(voice_summaries),
            "text_messages": len(text_messages),
            "unique_people": sorted(all_people),
            "unique_organizations": sorted(all_orgs),
            "unique_topics": sorted(all_topics),
            "unique_locations": sorted(all_locations),
        },
        "voice_messages": voice_summaries,
        "text_messages": text_messages,
    }

    context_file = work_dir / "synthesis_context.json"
    context_file.write_text(json.dumps(context, indent=2, ensure_ascii=False))
    print(f"  context: {len(voice_summaries)} voice, {len(text_messages)} text → {context_file.name}")

    prompt = _SYNTHESIS_PROMPT.replace(
        "{voice_count}", str(len(voice_summaries))
    ).replace(
        "{text_count}", str(len(text_messages))
    ).replace(
        "{people_count}", str(len(all_people))
    ).replace(
        "{org_count}", str(len(all_orgs))
    ).replace(
        "{context_json}", json.dumps(context, indent=2, ensure_ascii=False)
    )

    print(f"  calling LLM (model={model or _default_model()})...")
    try:
        report = _call_llm(prompt, model=model, max_tokens=8000)
    except Exception as e:
        print(f"  ERROR: synthesis failed: {e}")
        report = f"# Intelligence Report (synthesis failed)\n\nError: {e}\n\nRaw context preserved at {context_file}\n"

    # LLMs sometimes wrap output in ```markdown ... ``` fences — strip them
    report = report.strip()
    if report.startswith("```"):
        first_nl = report.find("\n")
        if first_nl > 0:
            report = report[first_nl + 1:]
        if report.rstrip().endswith("```"):
            report = report.rstrip()[:-3].rstrip()

    out = work_dir / "intelligence_report.md"
    out.write_text(report)
    print(f"  wrote {out} ({len(report)} chars)")
    return report


_SYNTHESIS_PROMPT = """You are an intelligence analyst. Below is a JSON corpus of intercepted communications — voice message transcripts (with speaker labels) and text messages from a Telegram chat export.

Your task: produce a STRUCTURED INTELLIGENCE REPORT in Markdown.

Write the report as if you are briefing a senior decision-maker. Be specific, cite evidence (file names, speakers, quotes). Do NOT speculate beyond what's in the data — if something is unclear, say so.

Use this exact structure:

# Intelligence Report

## Executive Summary
(3-5 sentences: what is this chat about, who are the actors, what is the activity)

## Actors Identified
(List each person mentioned, with: role/affiliation if known, evidence — which voice messages mention them, quotes if available)

## Organizations & Groups
(Each org/group, with stated purpose and key members)

## Key Narratives & Themes
(2-4 major themes that emerge across the corpus. For each: summary + supporting evidence)

## Timeline of Significant Content
(Chronological or sequential events, conversations, or revelations. Cite file names + timestamps where available)

## Locations Mentioned
(Each location and context)

## Indicators & Warning Signs
(Anything concerning: threats, plans, references to violence, illegal activity, etc. Be factual — quote directly)

## Information Gaps
(What we don't know. Missing speakers, unidentified references, files that failed to process)

## Source Metadata
- Voice messages analyzed: {voice_count}
- Text messages analyzed: {text_count}
- Unique people mentioned: {people_count}
- Unique organizations: {org_count}

---

CORPUS (JSON):

```json
{context_json}
```
"""


# ---------------------------------------------------------------------------
# Helpers

def _now():
    import datetime
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


def _default_model():
    return os.environ.get("LLM_MODEL", "deepseek/deepseek-chat")


def _default_llm_url():
    """Pick an LLM endpoint in priority order: explicit > OpenRouter > local."""
    if os.environ.get("LLM_API_URL"):
        return os.environ["LLM_API_URL"]
    # Auto-detect from .env if OPENROUTER_API_KEY is set
    if os.environ.get("OPENROUTER_API_KEY"):
        base = os.environ.get("OPENROUTER_BASE_URL", "https://openrouter.ai/api/v1")
        return f"{base}/chat/completions"
    return "http://localhost:18080/v1/chat/completions"


def _run(cmd, capture=False, check=True):
    if capture:
        return subprocess.run(cmd, text=True, capture_output=True, check=check).stdout
    return subprocess.run(cmd, check=check).returncode


def _relate(sc, src, edge, tgt):
    subprocess.run(
        [sys.executable, str(sc), "relate",
         "--src", src, "--edge", edge, "--tgt", tgt],
        capture_output=True, check=False,
    )


def _call_llm(prompt, model=None, temperature=0.3, max_tokens=8000):
    url = _default_llm_url()
    headers = {"Content-Type": "application/json"}
    # OpenRouter requires Authorization + recommends HTTP-Referer for ranking
    api_key = os.environ.get("OPENROUTER_API_KEY") or os.environ.get("LLM_API_KEY")
    if api_key and "openrouter" in url:
        headers["Authorization"] = f"Bearer {api_key}"
        headers["HTTP-Referer"] = "https://github.com/deep-research-engine"
        headers["X-Title"] = "deep-research-engine"
    elif api_key:
        headers["Authorization"] = f"Bearer {api_key}"

    data = json.dumps({
        "messages": [{"role": "user", "content": prompt}],
        "model": model or _default_model(),
        "temperature": temperature,
        "max_tokens": max_tokens,
    }).encode()
    req = urllib.request.Request(url, data=data, headers=headers)
    with urllib.request.urlopen(req, timeout=600) as resp:
        result = json.loads(resp.read())
    return result["choices"][0]["message"]["content"]


# ---------------------------------------------------------------------------
# Main

def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    sub = ap.add_subparsers(dest="cmd", required=True)

    p_run = sub.add_parser("run", help="full pipeline: ingest → graph → report")
    p_run.add_argument("--input", required=True, help="Telegram export directory")
    p_run.add_argument("--work-dir", required=True, help="working directory for outputs")
    p_run.add_argument("--audio-timeout", type=int, default=900)
    p_run.add_argument("--model", default=None, help="LLM model for synthesis")
    p_run.add_argument("--skip-audio", action="store_true")
    p_run.add_argument("--skip-entities", action="store_true")

    args = ap.parse_args()

    work_dir = Path(args.work_dir).resolve()
    work_dir.mkdir(parents=True, exist_ok=True)
    input_dir = Path(args.input).resolve()

    step_survey(input_dir)
    items = step_parse(input_dir, work_dir)

    if args.skip_audio:
        transcripts = step_audio(items, work_dir, input_dir, args.audio_timeout, skip=True)
    else:
        transcripts = step_audio(items, work_dir, input_dir, args.audio_timeout)

    if args.skip_entities:
        entities = []
    else:
        entities = step_entities(items, transcripts, work_dir)

    step_store(items, transcripts, entities, work_dir)
    step_synthesize(items, transcripts, entities, work_dir, model=args.model)

    print(f"\n[done] artifacts in {work_dir}")
    print(f"  - {work_dir/'intelligence_report.md'}")
    print(f"  - {work_dir/'items.json'}")
    print(f"  - {work_dir/'transcripts/'}")
    print(f"  - {work_dir/'entities.json'}")


if __name__ == "__main__":
    main()
