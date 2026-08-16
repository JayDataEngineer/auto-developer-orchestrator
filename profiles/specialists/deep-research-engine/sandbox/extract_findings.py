#!/usr/bin/env python3
"""Extract all pre-processed artifacts into structured research findings files.

Reads from the run directory and writes human-readable findings to
artifacts/research/. This is the GATHER step — deterministic, no reasoning.
The DRE specialists read these files for synthesis/audit/publish.
"""
import json
import os
import sys
from pathlib import Path
from datetime import datetime

def load_json(path):
    with open(path) as f:
        return json.load(f)

def main():
    run_dir = Path(sys.argv[1]) if len(sys.argv) > 1 else Path(
        "profiles/specialists/deep-research-engine/artifacts/run-2026-07-12"
    )
    research_dir = Path(
        "profiles/specialists/deep-research-engine/artifacts/research"
    )
    research_dir.mkdir(parents=True, exist_ok=True)

    ts = datetime.utcnow().strftime("%Y-%m-%dT%H:%M:%SZ")

    # ── items.json ──
    items_data = load_json(run_dir / "items.json")
    items = items_data["items"]
    stats = items_data["stats"]

    # ── Text messages ──
    msgs = [it for it in items if it.get("type") == "message"]
    lines = [
        f"<!-- pux:agent=extract_findings pux:saved={ts} pux:stage=research -->",
        "",
        "# Text Message Findings",
        "",
        f"Source: Telegram export `{items_data['source']}` format.",
        f"Date range: {stats['date_range']['earliest']} to {stats['date_range']['latest']}",
        f"Senders: {', '.join(stats['senders'])}",
        f"Total items: {stats['total']} (media: {stats['media']}, orphan media: {items_data['orphan_media_count']})",
        "",
        "## All Text Messages (13 total)",
        "",
    ]
    for i, m in enumerate(msgs):
        sender = m.get("sender", "Unknown")
        text = m.get("text", "")
        lines.append(f"### MSG-{i+1} [{sender}]")
        lines.append(f"Timestamp: {m.get('timestamp', '?')}")
        lines.append(f"Forwarded: {m.get('forwarded', False)}")
        lines.append("")
        lines.append(text)
        lines.append("")

    (research_dir / "text_findings.md").write_text("\n".join(lines))

    # ── Audio summaries ──
    try:
        summaries = load_json(run_dir / "audio_summaries.json")
        lines = [
            f"<!-- pux:agent=extract_findings pux:saved={ts} pux:stage=research -->",
            "",
            "# Audio/Voice Message Findings",
            "",
            f"Total audio segments: {len(summaries)}",
            f"Source: pre-processed from Telegram voice messages",
            "",
        ]
        for i, s in enumerate(summaries):
            lines.append(f"## AUDIO-{i+1}")
            lines.append(f"Source file: {s.get('source', '?')}")
            lines.append(f"Chunk: {s.get('chunk', '?')}")
            lines.append("")
            lines.append(s.get("summary", ""))
            lines.append("")
            lines.append("---")
            lines.append("")

        (research_dir / "audio_findings.md").write_text("\n".join(lines))
    except FileNotFoundError:
        pass

    # ── OCR results ──
    try:
        ocr = load_json(run_dir / "ocr_results.json")
        lines = [
            f"<!-- pux:agent=extract_findings pux:saved={ts} pux:stage=research -->",
            "",
            "# OCR Findings (Screenshots & Documents)",
            "",
            f"Total OCR results: {len(ocr)}",
            "",
            "## OCR'd Text by Photo",
            "",
        ]
        with_text = 0
        for photo, result in sorted(ocr.items()):
            text = ""
            if isinstance(result, dict):
                text = result.get("text", result.get("full_text", ""))
            elif isinstance(result, str):
                text = result
            if text and text.strip():
                with_text += 1
                lines.append(f"### {photo}")
                lines.append("```")
                lines.append(text[:2000])
                lines.append("```")
                lines.append("")
            else:
                lines.append(f"### {photo} — (no text detected)")
                lines.append("")

        lines.insert(4, f"Photos with text: {with_text}")
        (research_dir / "ocr_findings.md").write_text("\n".join(lines))
    except FileNotFoundError:
        pass

    # ── Video frame analysis ──
    try:
        vfa = load_json(run_dir / "video_frame_analysis.json")
        lines = [
            f"<!-- pux:agent=extract_findings pux:saved={ts} pux:stage=research -->",
            "",
            "# Video Frame Analysis Findings",
            "",
            f"Total frames analyzed: {len(vfa)}",
            "",
        ]
        # Group by video
        by_video = {}
        for frame in vfa:
            vid = frame.get("video", "unknown")
            by_video.setdefault(vid, []).append(frame)

        for vid, frames in sorted(by_video.items()):
            lines.append(f"## Video: {vid}")
            lines.append(f"Frames analyzed: {len(frames)}")
            lines.append("")
            for fr in frames:
                desc = fr.get("description", fr.get("error", ""))
                lines.append(f"### Frame: {fr.get('frame', '?')}")
                lines.append(f"Path: {fr.get('path', '?')}")
                lines.append(desc[:1500])
                lines.append("")

        (research_dir / "video_findings.md").write_text("\n".join(lines))
    except FileNotFoundError:
        pass

    # ── Face clusters ──
    try:
        fc = load_json(run_dir / "face_clusters.json")
        fe = load_json(run_dir / "face_embeddings.json")
        lines = [
            f"<!-- pux:agent=extract_findings pux:saved={ts} pux:stage=research -->",
            "",
            "# Face Clustering Findings",
            "",
            f"Face embeddings: {len(fe) if isinstance(fe, list) else fe.get('embedding_count', '?')}",
            f"Cluster count: {fc.get('cluster_count' if 'cluster_count' in fc else 'count', '?')}",
            f"Success: {fc.get('success', '?')}",
            "",
            "## Cluster Details",
            "",
        ]
        labels = fc.get("labels", [])
        if labels:
            # Count members per cluster
            from collections import Counter
            counts = Counter(labels)
            for cluster_id in sorted(counts.keys()):
                if cluster_id == -1 or cluster_id == "-1":
                    lines.append(f"### Noise/outliers: {counts[cluster_id]} items")
                else:
                    lines.append(f"### Cluster {cluster_id}: {counts[cluster_id]} items")
                lines.append("")

        (research_dir / "face_cluster_findings.md").write_text("\n".join(lines))
    except (FileNotFoundError, Exception) as e:
        print(f"Face cluster extraction: {e}")

    # ── Voice clusters ──
    try:
        vc = load_json(run_dir / "voice_clusters.json")
        lines = [
            f"<!-- pux:agent=extract_findings pux:saved={ts} pux:stage=research -->",
            "",
            "# Voice Clustering Findings",
            "",
            f"Success: {vc.get('success', '?')}",
            f"Cluster count: {vc.get('count', '?')}",
            "",
            "## Cluster Details",
            "",
        ]
        labels = vc.get("labels", [])
        if labels:
            from collections import Counter
            counts = Counter(labels)
            for cluster_id in sorted(counts.keys()):
                if str(cluster_id) in ("-1", "-1.0"):
                    lines.append(f"### Noise/outliers: {counts[cluster_id]} items")
                else:
                    lines.append(f"### Voice Cluster {cluster_id}: {counts[cluster_id]} segments")
                lines.append("")

        (research_dir / "voice_cluster_findings.md").write_text("\n".join(lines))
    except (FileNotFoundError, Exception) as e:
        print(f"Voice cluster extraction: {e}")

    # ── Object detection ──
    try:
        od = load_json(run_dir / "object_detection.json")
        lines = [
            f"<!-- pux:agent=extract_findings pux:saved={ts} pux:stage=research -->",
            "",
            "# Object Detection Findings (YOLOv8)",
            "",
            f"Total images analyzed: {len(od)}",
            "",
        ]
        for photo, result in sorted(od.items()):
            if isinstance(result, dict):
                detections = result.get("detections", result.get("labels", []))
                if detections:
                    lines.append(f"### {photo}")
                    lines.append(f"Detections: {detections}")
                    lines.append("")

        (research_dir / "object_detection_findings.md").write_text("\n".join(lines))
    except (FileNotFoundError, Exception) as e:
        print(f"Object detection extraction: {e}")

    # ── _INDEX.md ──
    findings_files = sorted(research_dir.glob("*.md"))
    # Remove old non-relevant files
    findings_files = [f for f in findings_files if f.name != "gvisor-findings.md"]
    lines = [
        f"<!-- pux:agent=extract_findings pux:saved={ts} pux:stage=research -->",
        "",
        "# Research Findings Index",
        "",
        f"Run directory: `{run_dir}`",
        f"Dataset: Telegram chat export (Montana extremist networks, March 2026)",
        f"Pre-processing: {stats['total']} items "
        f"({sum(1 for it in items if it.get('type') == 'photo')} photos, "
        f"{sum(1 for it in items if it.get('type') == 'voice')} voice, "
        f"{sum(1 for it in items if it.get('type') == 'video')} video, "
        f"{sum(1 for it in items if it.get('type') == 'message')} text)",
        "",
        "## Findings Files",
        "",
    ]
    for f in findings_files:
        if f.name == "_INDEX.md":
            continue
        lines.append(f"- [`{f.name}`](./{f.name}) — {f.stat().st_size} bytes")
    lines.append("")
    lines.append("## Source Artifacts (in run directory)")
    lines.append(f"- `{run_dir}/items.json` — parsed chat items")
    lines.append(f"- `{run_dir}/audio_summaries.json` — 37 audio segment summaries")
    lines.append(f"- `{run_dir}/ocr_results.json` — OCR text from 101 photos")
    lines.append(f"- `{run_dir}/video_frame_analysis.json` — 76 video frame descriptions")
    lines.append(f"- `{run_dir}/face_clusters.json` — face clustering (16 clusters)")
    lines.append(f"- `{run_dir}/voice_clusters.json` — voice clustering (7 clusters)")
    lines.append(f"- `{run_dir}/object_detection.json` — YOLOv8 detections")
    lines.append(f"- `{run_dir}/image_classification.json` — photo categories")

    (research_dir / "_INDEX.md").write_text("\n".join(lines))

    print(f"Findings extracted to {research_dir}/")
    for f in sorted(research_dir.glob("*.md")):
        if f.name != "gvisor-findings.md":
            print(f"  {f.name}: {f.stat().st_size:,} bytes")


if __name__ == "__main__":
    main()
