#!/usr/bin/env python3
"""Entity extraction client for Pux sandbox workers.

Standalone CLI — extracts named entities from text using LLM.
No DRE engine dependencies.

Usage:
    python3 entity_extract.py extract --text "John met Sarah in Paris on Tuesday"
    python3 entity_extract.py extract --input document.txt
    python3 entity_extract.py extract --input chunks.json --field content
    python3 entity_extract.py batch --input chunks.json --output entities.json

Environment:
    LLM_API_URL    OpenAI-compatible /v1/chat/completions endpoint. REQUIRED.
                   The sandbox policy injects this from the harness model tier
                   (``sandbox.llm: <role>`` in policy.yaml → resolved via
                   models.yaml); for standalone use, export it yourself.
    LLM_MODEL      Model id (e.g. "mimo-v2.5"). REQUIRED. Same injection path.
    LLM_API_KEY    API key. Optional (some local endpoints need none).
"""

import argparse
import json
import os
import re
import sys
from pathlib import Path


EXTRACTION_PROMPT = """Extract all named entities from the following text.

Return a JSON object with exactly these fields:
- "people": list of person names mentioned (real or referenced)
- "organizations": list of organizations, groups, movements, or affiliations
- "topics": list of topics, subjects, or themes discussed
- "locations": list of geographic locations mentioned
- "dates": list of dates or time references

Text:
{content}

Respond with ONLY the JSON object, no other text."""


def _llm_env_required():
    """The sandbox policy (``sandbox.llm: <role>``) injects these from the
    harness model tier (models.yaml). This script is a DUMB CONSUMER — it does
    not re-resolve the provider, model, or key. Fail loud if the vars are
    missing so a misconfigured sandbox surfaces immediately, not as a silent
    fallback to a dead endpoint."""
    url = os.environ.get("LLM_API_URL", "")
    model = os.environ.get("LLM_MODEL", "")
    if not url or not model:
        print(
            "ERROR: LLM_API_URL and LLM_MODEL must be set. The sandbox policy\n"
            "injects them via `sandbox.llm: <role>` (resolved from models.yaml).\n"
            "For standalone use:\n"
            '  export LLM_API_URL="https://opencode.ai/zen/go/v1/chat/completions"\n'
            '  export LLM_MODEL="mimo-v2.5"\n'
            '  export LLM_API_KEY="<key>"',
            file=sys.stderr,
        )
        sys.exit(1)
    return url, model


def call_llm(prompt, model=None, temperature=0.1, max_tokens=10000, disable_thinking=True):
    """Call LLM API and return response text.

    Handles reasoning models (e.g. mimo-v2.5) that emit chain-of-thought into
    `reasoning_content` before the final answer. With a low token budget the
    entire budget is spent on reasoning and `content` returns null.

    disable_thinking=True (default) sends `thinking: {type: disabled}` which
    skips the reasoning pass entirely. Entity extraction is a structured task
    that doesn't need chain-of-thought — without it, each call is ~1-2s instead
    of 10-30s. We still fall back to `reasoning_content` when `content` is
    empty, then retry once with a doubled budget if both are empty.
    """
    import urllib.request

    url, default_model = _llm_env_required()
    headers = {
        "Content-Type": "application/json",
        # Cloudflare bot-integrity (error 1010) bans the default
        # ``Python-urllib/3.x`` signature. A neutral identifier passes.
        "User-Agent": "pux-harness-sandbox/1.0",
    }
    api_key = os.environ.get("LLM_API_KEY", "")
    if api_key:
        headers["Authorization"] = f"Bearer {api_key}"

    def _do_request(budget):
        payload = {
            "messages": [{"role": "user", "content": prompt}],
            "model": model or default_model,
            "temperature": temperature,
            "max_tokens": budget,
        }
        # thinking:{type:disabled} turns off the reasoning pass on reasoning
        # models (mimo-v2.5 via opencode-go / z.ai coding endpoint). Without
        # this, mimo spends its entire token budget on chain-of-thought.
        if disable_thinking:
            payload["thinking"] = {"type": "disabled"}
        data = json.dumps(payload).encode()
        req = urllib.request.Request(url, data=data, headers=headers)
        with urllib.request.urlopen(req, timeout=180) as resp:
            return json.loads(resp.read())

    try:
        result = _do_request(max_tokens)
        msg = result["choices"][0]["message"]
        content = msg.get("content")
        if not content:
            content = msg.get("reasoning_content") or ""
        if not content:
            result = _do_request(max_tokens * 2)
            msg = result["choices"][0]["message"]
            content = msg.get("content") or msg.get("reasoning_content") or ""
        if not content:
            print("ERROR: LLM returned empty content and reasoning_content "
                  "(finish_reason={}). Increase max_tokens or use a non-reasoning model."
                  .format(result["choices"][0].get("finish_reason")), file=sys.stderr)
            sys.exit(1)
        return content
    except Exception as e:
        print(f"ERROR: LLM API failed: {e}", file=sys.stderr)
        sys.exit(1)


def parse_json_response(text):
    """Try to extract JSON from LLM response text."""
    # Direct parse
    try:
        result = json.loads(text)
        return validate_result(result)
    except (json.JSONDecodeError, Exception):
        pass

    # Try extracting from markdown code blocks
    json_match = re.search(r'```(?:json)?\s*(\{.*?\})\s*```', text, re.DOTALL)
    if json_match:
        try:
            result = json.loads(json_match.group(1))
            return validate_result(result)
        except (json.JSONDecodeError, Exception):
            pass

    # Try finding first { ... } block
    brace_match = re.search(r'\{[^{}]*\}', text, re.DOTALL)
    if brace_match:
        try:
            result = json.loads(brace_match.group(0))
            return validate_result(result)
        except (json.JSONDecodeError, Exception):
            pass

    print("WARNING: Could not parse entity extraction response as JSON", file=sys.stderr)
    return empty_result()


def empty_result():
    return {"people": [], "organizations": [], "topics": [], "locations": [], "dates": []}


def validate_result(data):
    """Validate and normalize extraction result."""
    result = empty_result()
    for key in ["people", "organizations", "topics", "locations", "dates"]:
        val = data.get(key, [])
        if isinstance(val, list):
            result[key] = [str(v).strip() for v in val if str(v).strip()]
    return result


def extract_entities(content, model=None):
    """Extract named entities from text content using LLM."""
    if not content or len(content.strip()) < 10:
        return empty_result()

    prompt = EXTRACTION_PROMPT.format(content=content[:8000])
    raw = call_llm(prompt, model=model, temperature=0.1)
    return parse_json_response(raw)


def load_text(path, field=None):
    """Load text from a file. Supports .txt and .json formats."""
    raw = Path(path).read_text(encoding="utf-8")

    if path.endswith(".json"):
        data = json.loads(raw)
        if isinstance(data, list):
            # Array of objects — extract text from specified field or content/text
            texts = []
            for item in data:
                if isinstance(item, str):
                    texts.append(item)
                elif isinstance(item, dict):
                    text = item.get(field or "content", item.get("text", ""))
                    if text:
                        texts.append(str(text))
            return "\n\n---\n\n".join(texts)
        elif isinstance(data, dict):
            return data.get(field or "content", data.get("text", raw))
    return raw


def cmd_extract(args):
    """Extract entities from text or file."""
    if args.input:
        content = load_text(args.input, field=args.field)
    elif args.text:
        content = args.text
    else:
        print("ERROR: provide --text or --input", file=sys.stderr)
        sys.exit(1)

    result = extract_entities(content, model=args.model)
    print(json.dumps(result, indent=2))

    total = sum(len(v) for v in result.values())
    print(f"\n({total} entities extracted)", file=sys.stderr)


def cmd_batch(args):
    """Extract entities from each item in a JSON array."""
    chunks = json.loads(Path(args.input).read_text(encoding="utf-8"))

    if not isinstance(chunks, list):
        print("ERROR: --input must be a JSON array", file=sys.stderr)
        sys.exit(1)

    results = []
    for i, chunk in enumerate(chunks):
        if isinstance(chunk, str):
            content = chunk
        elif isinstance(chunk, dict):
            content = chunk.get("content", chunk.get("text", ""))
        else:
            content = str(chunk)

        if not content or len(content.strip()) < 10:
            results.append(empty_result())
            continue

        entities = extract_entities(content, model=args.model)
        if isinstance(chunk, dict):
            chunk["entities"] = entities
            results.append(chunk)
        else:
            results.append({"text": content, "entities": entities})

        if (i + 1) % 10 == 0:
            print(f"Processed {i + 1}/{len(chunks)}...", file=sys.stderr)

    output_path = args.output
    if output_path:
        Path(output_path).write_text(json.dumps(results, indent=2, ensure_ascii=False))
        print(json.dumps({"status": "ok", "items": len(results), "output": output_path}))
    else:
        print(json.dumps(results, indent=2, ensure_ascii=False))

    print(f"\n({len(results)} items processed)", file=sys.stderr)


def main():
    parser = argparse.ArgumentParser(description="Entity extraction for Pux sandbox")
    sub = parser.add_subparsers(dest="command")

    p = sub.add_parser("extract", help="Extract entities from text or file")
    p.add_argument("--text", help="Text to extract entities from")
    p.add_argument("--input", help="Input file (.txt or .json)")
    p.add_argument("--field", help="Field name for JSON input (default: content)")
    p.add_argument("--model", help="LLM model to use")

    p = sub.add_parser("batch", help="Extract entities from JSON array")
    p.add_argument("--input", required=True, help="JSON file with text items")
    p.add_argument("--output", help="Output JSON file (default: stdout)")
    p.add_argument("--model", help="LLM model to use")

    args = parser.parse_args()
    if not args.command:
        parser.print_help()
        sys.exit(1)

    commands = {"extract": cmd_extract, "batch": cmd_batch}
    commands[args.command](args)


if __name__ == "__main__":
    main()
