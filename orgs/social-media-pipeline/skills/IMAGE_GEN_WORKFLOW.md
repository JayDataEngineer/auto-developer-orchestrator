# IMAGE_GEN_WORKFLOW

How to call Forge on the Ray cluster to generate images.

## Health Check (always do this first)

```bash
python3 /sandbox/forge_client.py health
```

Expected output: JSON with `vram_free_mb`, `gpu`, `loaded`. If you see "FORGE DOWN", abort.

## Generate One Image

```bash
python3 /sandbox/forge_client.py generate \
    "isometric 3D neural network, cyberpunk neon, dark background" \
    --mode image \
    --out /sandbox/workspace/images/img_1.png
```

The client handles 3 response shapes:
1. URL string → downloads to `--out`
2. Base64 string → decodes to `--out`
3. Dict with `url`/`image`/`data` key → extracts + saves

## Generate With Params

```bash
python3 /sandbox/forge_client.py generate "portrait photo" \
    --mode image \
    --params '{"seed": 42, "width": 1024, "height": 1024, "steps": 30}' \
    --out /sandbox/workspace/images/img_2.png
```

Params are forwarded as-is to Forge. Common ones:
- `seed` — for reproducibility
- `width`, `height` — dimensions (Twitter: 1200x675 for in-stream, 1080x1080 for square)
- `steps` — diffusion steps (more = higher quality, slower)
- `negative_prompt` — what to avoid

## Prompt Engineering Tips

Good prompts for social media:
- **Concrete subject**: "isometric 3D render of X" not "an image of X"
- **Lighting**: "cinematic lighting", "golden hour", "neon glow"
- **Style**: "minimalist illustration", "oil painting", "photorealistic"
- **Aspect ratio hint**: include "wide composition" or "square composition" in prompt

Bad prompts:
- Vague: "something about AI"
- Trademarked characters (won't always work)
- Real people's names (ethical/legal)

## Modes Available

| Mode | What it does | Use when |
|------|--------------|----------|
| `image` (default) | Static image generation | Most cases |
| `3d` | 3D model generation | Need a rotatable asset |
| `music` | Audio clip generation | Background music |
| `video` | Short video clip | Animated content (slow — minutes) |

## Don't

- Don't generate >5 images per pipeline run (GPU is shared)
- Don't retry a failed generation more than once
- Don't skip the health check — it catches routing issues early

## Endpoint

`MCP_HUB_ENDPOINT` env var is set to `http://100.86.69.57:30080` in the sandbox. Forge lives at `/forge/generate` under that.
