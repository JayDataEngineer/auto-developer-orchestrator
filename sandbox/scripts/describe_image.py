#!/usr/bin/env python3
"""
describe_image.py — vision inference backbone for pux-mcpserver.

Backbone script (System A — read-only at runtime, ships with the sandbox
image at /usr/local/bin/describe_image.py). The Go describe_image tool
in backend/internal/mcpserver/vision_tool.go shells out to this script.

Loads Qwen3.5-2B-ONNX-OPT (fp16 vision) from
/sandbox/workspace/.pux/models/Qwen3.5-2B-ONNX-OPT/, runs inference on the
given image, prints the description as JSON to stdout. Errors go to stderr
with a non-zero exit code.

The model is OPTIONAL. If the model directory is missing or incomplete,
the script exits 2 with a friendly stderr message — the Go tool translates
that into a "vision unavailable, run scripts/bootstrap-vision.sh" result.

Usage:
    describe_image.py --image /sandbox/workspace/foo.png
    describe_image.py --image /sandbox/workspace/foo.png --prompt "What text is on the sign?"
    describe_image.py --image-url https://example.com/foo.png

Output (success):
    {"description": "...", "model": "Qwen3.5-2B-ONNX-OPT"}

Output (model missing):
    exit 2, stderr: "model not found at <path>; run scripts/bootstrap-vision.sh"

Output (inference error):
    exit 1, stderr: error message
"""
from __future__ import annotations

import argparse
import json
import os
import sys
from pathlib import Path

# Default model location — bind-mounted from <project>/.pux/models/...
DEFAULT_MODEL_DIR = "/sandbox/workspace/.pux/models/Qwen3.5-2B-ONNX-OPT"

# Sentinel files whose presence means the model is fully downloaded.
REQUIRED_FILES = [
    "genai_config.json",
    "preprocessor_config.json",
    "model.onnx",
    "vision_encoder_fp16.onnx",
]

# Default prompt — generic "describe this image." Specialized prompts via --prompt.
DEFAULT_PROMPT = "Describe this image concisely. Focus on text, UI elements, and key visual features."


def parse_args() -> argparse.Namespace:
    p = argparse.ArgumentParser(description=__doc__.split("\n")[0] if __doc__ else "")
    src = p.add_mutually_exclusive_group(required=True)
    src.add_argument("--image", help="Path to image file (sandbox-absolute)")
    src.add_argument("--image-url", help="URL of image to download and describe")
    p.add_argument(
        "--model-dir",
        default=os.environ.get("PUX_VISION_MODEL_DIR", DEFAULT_MODEL_DIR),
        help=f"Model directory (default: {DEFAULT_MODEL_DIR})",
    )
    p.add_argument("--prompt", default=DEFAULT_PROMPT, help="Question / instruction for the model")
    p.add_argument("--max-tokens", type=int, default=512, help="Max output tokens")
    return p.parse_args()


def check_model(model_dir: Path) -> tuple[bool, str]:
    """Return (ready, reason). reason is empty when ready."""
    if not model_dir.exists():
        return False, f"model not found at {model_dir}; run scripts/bootstrap-vision.sh"
    for f in REQUIRED_FILES:
        if not (model_dir / f).exists():
            return False, f"model incomplete: missing {f}; re-run scripts/bootstrap-vision.sh"
    return True, ""


def fetch_url(url: str) -> bytes:
    """Download image bytes from URL. Uses stdlib urllib (no extra deps)."""
    from urllib.request import urlopen

    with urlopen(url, timeout=30) as resp:  # noqa: S310 — model-supplied URL is trusted operator input
        return resp.read()


def run_inference(model_dir: Path, image_bytes: bytes, prompt: str, max_tokens: int) -> str:
    """Load model and run inference. Returns description text.

    Uses onnxruntime-genai (the canonical Qwen3.5 runner). Imports are lazy
    so the script can still print the "model missing" message when the
    Python deps aren't installed yet — keeps the error path observable.
    """
    try:
        import onnxruntime_genai as og
    except ImportError as e:
        sys.stderr.write(
            f"onnxruntime-genai not installed in sandbox: {e}\n"
            "Rebuild sandbox image (task build) to pick up the dependency.\n"
        )
        sys.exit(3)

    # Load model. og.Model takes the directory path; it picks up genai_config.json
    # and the .onnx files automatically.
    model = og.Model(str(model_dir))
    processor = model.create_multimodal_processor()
    tokenizer_stream = processor.create_stream()

    # Build the multimodal prompt. Qwen3-VL uses <|vision_start|>...<|vision_end|>
    # tokens around the image; the processor handles the alignment.
    image = og.Images.from_bytes(image_bytes)
    prompt_text = f"<|im_start|>user\n<|vision_start|><|image|><|vision_end|>{prompt}<|im_end|>\n<|im_start|>assistant\n"
    inputs = processor(image, prompt_text)

    # Generate tokens until EOS or max_tokens.
    params = og.GeneratorParams(model)
    params.set_inputs(inputs)
    params.search_options["max_length"] = max_tokens

    output_tokens: list[str] = []
    gen = og.Generator(model, params)
    try:
        while not gen.is_done():
            gen.compute_logits()
            gen.generate_next_token()
            token = gen.get_next_tokens()[0]
            output_tokens.append(token)
            tokenizer_stream.process(token)
    finally:
        del gen

    return tokenizer_stream.text if hasattr(tokenizer_stream, "text") else "".join(
        # Fallback: decode each token via the processor's tokenizer
        processor.decode([int(t) if t.isdigit() else 0 for t in output_tokens])
    )


def main() -> int:
    args = parse_args()
    model_dir = Path(args.model_dir)

    ready, reason = check_model(model_dir)
    if not ready:
        sys.stderr.write(reason + "\n")
        return 2

    # Acquire image bytes.
    if args.image:
        image_path = Path(args.image)
        if not image_path.exists():
            sys.stderr.write(f"image not found: {image_path}\n")
            return 1
        image_bytes = image_path.read_bytes()
    else:
        try:
            image_bytes = fetch_url(args.image_url)
        except Exception as e:
            sys.stderr.write(f"failed to fetch {args.image_url}: {e}\n")
            return 1

    # Run inference.
    try:
        description = run_inference(model_dir, image_bytes, args.prompt, args.max_tokens)
    except SystemExit:
        raise
    except Exception as e:
        sys.stderr.write(f"inference failed: {e}\n")
        return 1

    # Emit JSON result on stdout — Go tool parses this.
    result = {
        "description": description.strip(),
        "model": model_dir.name,
    }
    json.dump(result, sys.stdout)
    sys.stdout.write("\n")
    return 0


if __name__ == "__main__":
    sys.exit(main())
