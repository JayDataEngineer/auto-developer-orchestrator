# FORGE_WORKFLOW

How to call Forge on the Tech Noir Ray cluster for image / 3D / music / video generation.

## Health Check (always do this first)

```bash
python3 /sandbox/forge_client.py health
```

Expected: JSON with `vram_free_mb`, `gpu`, `loaded`. If you see "FORGE_DOWN", abort art cycle and tell studio-director.

## Generate an Image (character portrait, environment, prop texture)

```bash
python3 /sandbox/forge_client.py generate \
    "isometric 2.5D cyberpunk character, teal and crimson palette, neutral background" \
    --mode image \
    --out /sandbox/workspace/art/cycle-1/char_01.png
```

## Generate with Params (seed for reproducibility, dimensions)

```bash
python3 /sandbox/forge_client.py generate \
    "tileable texture: wet asphalt, top-down, cyberpunk neon reflections" \
    --mode image \
    --params '{"seed": 1337, "width": 1024, "height": 1024, "steps": 30}' \
    --out /sandbox/workspace/art/cycle-1/tile_asphalt.png
```

## Modes Available

| Mode | What it does | When to use |
|------|--------------|-------------|
| `image` (default) | Static image | Characters, environments, textures, props |
| `3d` | 3D model (.glb/.obj) | Hero props the camera circles |
| `music` | Audio clip | Soundtrack stingers, ambient beds |
| `video` | Short video clip (SLOW — minutes) | UI effects, animated logos. Use sparingly. |

## Tech Noir Art Direction

Always include in the prompt:
- **Palette hint**: "cyberpunk neon", "teal-and-crimson", "noir desaturated"
- **Lighting**: "rim-lit", "volumetric fog", "single-source practical"
- **Composition**: "isometric 2.5D", "side-scroller parallax", "documentary wide"
- **Mood**: "ominous", "lonely", "hyperreal"

Avoid:
- "Cute", "cartoonish" (wrong tone for survival horror)
- Real people's faces (use silhouettes / obscured)
- Trademarked logos or characters

## Throughput Discipline

- Max 8 image generations per cycle (GPU is shared)
- Max 1 video generation per cycle (slow + expensive)
- Don't retry a failed generation more than once — log and move on

## Failure Modes

| Failure | Recovery |
|---------|----------|
| `FORGE_DOWN` | Skip art cycle; route to gameplay_programmer with "no new art" note |
| `FORGE_TIMEOUT` (cold GPU > 3min) | Retry once after 30s, then abort cycle |
| Generation returned but file missing | Save the response JSON to `/sandbox/workspace/art/failures/` and continue |
| NSFW / inappropriate output | Discard, log to `/sandbox/workspace/art/rejected.json`, try a stricter prompt |

## Endpoint

`MCP_HUB_ENDPOINT` env var points at the Forge API (default `http://localhost:30080`; override via sandbox env when pointing at a remote cluster). Forge lives at `/forge/generate` under that. Don't hardcode the URL — read it from env in case the cluster moves.
