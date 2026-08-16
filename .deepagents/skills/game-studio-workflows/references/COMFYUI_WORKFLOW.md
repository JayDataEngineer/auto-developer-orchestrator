# COMFYUI_WORKFLOW

How to drive ComfyUI on the Ray cluster for VFX, sprite post-processing, and batch texture ops that Forge's single-shot API can't express.

## Health Check

```bash
python3 /sandbox/comfyui_client.py health
```

If `COMFYUI_DOWN`: Forge can cover most needs — fall back to FORGE_WORKFLOW. Only ComfyUI gives you node-graph control.

## When to Use ComfyUI vs Forge

| Use Forge (forge_client.py) when | Use ComfyUI when |
|-----------------------------------|------------------|
| Single-shot image gen | Multi-step pipeline (generate → upscale → img2img) |
| Simple prompt → image | Iterative refinement with controlnets |
| Default sampler settings OK | Need specific samplers, schedulers, CFG curves |
| Fast iteration | Batch consistency (same seed family across many images) |

## Post a Workflow File

Build a workflow in the ComfyUI UI, save as JSON, drop it in `/sandbox/workspace/art/workflows/`:

```bash
python3 /sandbox/comfyui_client.py post-workflow \
    --file /sandbox/workspace/art/workflows/char_refine.json
```

The response includes `prompt_id` — poll `/queue` or `/history/{prompt_id}` for completion.

## Post Inline Workflow JSON

For workflows the agent constructs programmatically (rare — usually you want a file):

```bash
python3 /sandbox/comfyui_client.py post-prompt \
    --workflow '{"3":{"class_type":"KSampler","inputs":{"seed":42,"cfg":7,"model":["4",0]}}}'
```

## Common Workflows for Game Studio

| Workflow file | Purpose |
|----------------|---------|
| `char_refine.json` | Take Forge character output → img2img refine at higher steps |
| `sprite_sheet.json` | Multi-pose sprite sheet for walk cycles |
| `texture_tileable.json` | Seamless tileable textures (Make Tileable node) |
| `upscale_4x.json` | Latent upscale for hero environment art |

If a workflow file is missing, ask the technical_artist to author it. Don't hand-roll ComfyUI JSON in a single prompt — it's too error-prone.

## Polling for Output

ComfyUI is async — the POST returns immediately. To check completion:

```bash
# Queue depth
python3 /sandbox/comfyui_client.py queue

# Once prompt_id is done, output is in /workspace/output/ on the cluster
# Retrieve via the Ray proxy: GET $COMFYUI_URL/view?filename=<name>&type=output
```

## Failure Modes

| Failure | Recovery |
|---------|----------|
| `COMFYUI_DOWN` | Fall back to Forge; document what workflow couldn't run |
| Workflow JSON parse error | Validate with `jq . <file>` before posting |
| Queue stuck > 5min | Cancel via `DELETE /queue` on the proxy, retry once, then abort |
| Missing node (custom node not installed) | Log the node name to `/sandbox/workspace/art/missing_nodes.txt`; use Forge instead |

## Endpoint

`COMFYUI_URL` env var (default `http://localhost:30080/image/comfyui`; override via sandbox env when pointing at a remote cluster). Routes under that: `/prompt`, `/queue`, `/history`, `/view`, `/system_stats`.
