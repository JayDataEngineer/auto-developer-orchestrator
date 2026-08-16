# CHARACTER_PIPELINE

How character assets move through the Game Studio art pipeline on the Ray
cluster. Read this before running a character build — both the renderer (batch
manifests) and the technical-artist (single-asset iteration).

## Stage DAG

Default order (each arrow is a dependent stage):

```
generate → sheet → outfit → emotions → sprites_static → sprites_animated → video → trellis → lora → state → godot
```

The character manifest / config YAML carries a `stages:` list — honor it and
skip stages not listed. The DAG executor is `departments.art.scripts.run_build`
(always `--dry-run` first). Music / SFX / texture / prop / layout assets bypass
this DAG — they run through `stages_floor` (one stage = one Ray job).

## Per-stage primary / fallback

Each stage has a primary path and a fallback that triggers automatically on
primary failure:

| Stage | YAML key | Primary | Fallback |
|-------|----------|---------|----------|
| generate | `generation.source` | Z-Image generate | Import existing |
| sprites_animated | `packages.sprites.motion_strategy` | HY-Motion | LLM pose guessing |
| sprites_static | `packages.sprites.body_reference` | Anny mesh | TRELLIS 3D |
| sheet | `packages.sheet.face_refinement` | Skip | FaceDetailer |

## VNCCS — character consistency

Characters stay consistent across sprites via **reference latents, not
ControlNet**:

- `VNCCS_QWEN_Encoder` VAE-encodes the character + reference image as latents.
- Latents are injected at timestep zero with **quadratic weighting**.
- Key params (spec these in the manifest's `params.vnccs:` block):
  `image1_name`, `image2_name` (character + style reference), `weight1`,
  `weight2` (default 0.6 / 0.4), `target_size`, `vl_size`, `instruction`.

If VNCCS params are missing from a character build, report it — don't guess.
Full reference: `docs/pipeline/vnccs_reference.md`.

## Known bug

`CharacterCreator` ignores `new_character_name` and registers the character as
`"None"`. Don't rely on the registered name persisting — track characters by
output path.
