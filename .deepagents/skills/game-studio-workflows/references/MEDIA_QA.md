# MEDIA_QA

How to use the media MCP server (media-mcp) to vibe-check gameplay screenshots, QA generated art, and batch-process playtest captures.

The `media` server comes from the shared catalog (`profiles/_shared/tool_servers.yaml`) and is NOT armed by default — the org must declare `{kind: mcp, ref: media}` in its org.yaml. Its tools surface as `media_*` (analyze_image, detect_objects, tag_image, classify_nsfw, etc.).

## Vibe Report Format

Every QA cycle writes `/sandbox/workspace/qa/cycle-N/vibe.json`:

```json
{
  "cycle": N,
  "captured_at": "2026-...",
  "viewport_shot": "viewport.png",
  "scores": {
    "art_direction": 1-5,
    "tone_consistency": 1-5,
    "technical_quality": 1-5
  },
  "issues": [
    {"severity": "low|med|high", "area": "player sprite", "note": "..."},
    ...
  ],
  "recommendation": "iterate|yield|abort",
  "next_focus": "what the next cycle should fix"
}
```

`recommendation`:
- `iterate` → the CTO kicks off cycle N+1
- `yield` → the CTO stops, marks task_run success
- `abort` → the CTO stops, marks task_run failed

## How to Score

Run media tools against the screenshot:

```
media_analyze_image(
    imageSource="file:///sandbox/workspace/qa/cycle-N/viewport.png",
    prompt="Score this 2.5D cyberpunk survival-horror screenshot 1-5 on: art direction, tone consistency (lonely/ominous), technical quality (composition, lighting). List any visual issues."
)
```

Aggregate the response into `scores`. Don't copy the model's prose verbatim — distill it.

## Tone Check (Game Studio Specific)

Game Studio is 2.5D cyberpunk survival-horror. The tone is **lonely, ominous, hyperreal** — NOT:
- Cartoonish
- Bright / saturated (except small accent colors)
- Cute
- Heroic

Use `tag_image` to get objective labels. If tags include "cute", "cartoon", "anime" (in a chibi sense), or "heroic", mark `tone_consistency` low.

## NSFW Guard

Always run `classify_nsfw` on every screenshot:

```
media_classify_nsfw(imageSource="file:///...")
```

If `unsafe` score > 0.3, mark `tone_consistency` low, log to `/sandbox/workspace/qa/cycle-N/rejected.json`, and recommend `iterate` with stricter prompt.

## Object Detection for Composition

Use `detect_objects` to verify expected entities are in frame:

```
media_detect_objects(
    imageSource="file:///...",
    confidence=0.4
)
```

If the player sprite is missing or off-screen, that's a `high` severity issue regardless of other scores.

## Batch Playtest Review

When the CTO hands you 5+ screenshots from a playtest:

1. Run `analyze_image` on each (in sequence — don't parallelize, MCP server may rate-limit)
2. Aggregate scores into a single vibe.json
3. For outliers (one shot scored 2 while others scored 4), call out the specific issue rather than averaging down

## What the CTO Reads

The CTO reads `vibe.json` and decides:
- `iterate` + `next_focus: "player sprite contrast"` → next cycle the technical_artist gets "boost player sprite contrast against backgrounds"
- `yield` + scores ≥4 → task_run marked success, files listed
- `abort` + issues list → task_run marked failed, issues surfaced to user

Don't bury the lede — `recommendation` is the only field that drives the loop. Everything else is context.
