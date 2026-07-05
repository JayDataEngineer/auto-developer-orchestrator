"""Native specialist tools — the ``pux_sandbox_*`` specialist surface.

Each tool is a LangChain StructuredTool named ``pux_sandbox_<name>``. This
module is the SINGLE source of specialists: ``graph.py`` calls
``build_native_specialists`` for every specialist, and the org contract
resolves agent ``tools:`` whitelists against ``SPECIALIST_TOOL_NAMES`` below.
These tools were first ported from the Go MCP bridge (Phases 8b–8f) and the
bridge was deleted in 8i — there is no longer a Go surface these mirror.

Result contract fidelity: each tool returns the SAME JSON the Go MCP server
marshaled into its tool-call text blocks (verified against the live bridge
during the port — e.g. ``list_skills`` returns ``{"skills": [...], "count": N}``
indented 2), so the agent-visible output is byte-equivalent pre/post port.

Batch 1 (8b/8c):
  - ``python``        — ``python3 -c <code>`` via docker exec (was Go's
                        SandboxPythonTool; 96 LOC).
  - ``list_skills``   — host FS walk of every skills root (the active org's
                        ``skills/`` first, then ``orgs/_shared/skills/``, then
                        other orgs'); org-local wins on name collision.
  - ``load_skill``    — host FS read of one ``SKILL.md`` body, resolved against
                        the same root order.

Batch 2 (8d):
  - ``describe_image`` — **driving-model-primary** (mimo-v2.5 native multimodal
                         via the OpenAI-compatible router) with the in-sandbox
                         ONNX (``/usr/local/bin/describe_image.py``,
                         Qwen3.5-2B-ONNX-OPT) as **fallback** on any primary
                         failure. The ONNX path is the original Go
                         DescribeImageTool contract (exit-code dispatch + 120s
                         timeout + the model-missing ``success:false`` — NOT an
                         error).

Batch 3 (8e):
  - ``browser_navigate`` / ``_click`` / ``_type`` / ``_screenshot`` /
    ``_evaluate`` — POST to the in-sandbox ``sb_server.py`` (127.0.0.1:9876)
    via ``curl`` run through docker exec (was Go's BrowserTool spec family).

Batch 4 (8f):
  - ``desktop_screenshot`` / ``_click`` / ``_type`` / ``_key`` — X11
    (``DISPLAY=:99``) via ``xdotool`` + ``desktop_observe.py`` through docker
    exec (was Go's DesktopTool spec family; pixel-coord contract — OCR drifts).

**Bug fixed by the port:** the Go skills package read ``<root>/skills/`` which
does not exist in this repo; the live ``list_skills`` returned ``count: 0``.
Probed before porting (2026-07-03); the Python port reads the real roots
(``orgs/_shared/skills`` + each ``orgs/<name>/skills``). Phase 15 retired the
old single ``.pi/skills`` root entirely — skills now follow the same org-local
+ ``_shared`` shape as agents.

Timeout note: the Go ``describe_image`` tool enforced a 120s deadline. The
shared ``DockerExecClient.exec(timeout=120)`` now enforces it (Phase 8d) and
raises ``ExecTimeout``, which this tool maps to ``reason:"timeout"``. The Go
``python`` tool's 60s deadline is not yet wired (fast calls dominate).
"""
from __future__ import annotations

import json
import logging
import shlex
from pathlib import Path

from langchain_core.messages import HumanMessage
from langchain_core.tools import StructuredTool
from pydantic import BaseModel, Field

from pux_harness.sandbox.docker_exec import DockerExecClient, ExecTimeout

log = logging.getLogger(__name__)

PUX_PREFIX = "pux_sandbox_"
PROJECT_ROOT = Path(__file__).resolve().parents[3]
SKILL_FILE = "SKILL.md"


def _skills_dirs(org: str | None = None) -> list[Path]:
    """Skills-ROOT directories to search, highest-priority first.

    With an ``org``: that org's ``skills/`` wins, then ``orgs/_shared/skills``,
    then every other org's skills (so a cross-org skill is still discoverable).
    Without an ``org`` (the offline ``--check`` smoke path): all roots in stable
    sorted order, no priority. Non-existent dirs are filtered out."""
    orgs = PROJECT_ROOT / "orgs"
    roots: list[Path] = []
    if org:
        roots.append(orgs / org / "skills")
    roots.append(orgs / "_shared" / "skills")
    seen = {str(r) for r in roots}
    for p in sorted(orgs.glob("*/skills")):
        if str(p) not in seen:
            roots.append(p)
            seen.add(str(p))
    return [r for r in roots if r.is_dir()]

# The complete set of unprefixed specialist names the harness implements
# natively (Phase 8i renamed this from the transitional ``PORTED_SPECIALISTS`` —
# there is no longer a Go bridge to have been "ported" from). ``build_native_
# specialists`` below returns exactly one ``pux_sandbox_<name>`` tool per entry;
# ``SPECIALIST_TOOL_NAMES`` is the prefixed form the org contract resolves
# ``tools:`` whitelists against.
SPECIALISTS: frozenset[str] = frozenset({
    "python", "list_skills", "load_skill", "describe_image",
    "browser_navigate", "browser_click", "browser_type", "browser_screenshot", "browser_evaluate",
    "browser_search", "browser_scroll", "browser_go_back", "browser_wait", "browser_find_text",
    "browser_extract", "browser_extract_images", "browser_save_screenshot", "browser_download",
    "browser_upload", "browser_tabs", "browser_new_tab", "browser_switch_tab", "browser_close_tab",
    "browser_dropdown_options", "browser_select_dropdown",
    "browser_save_session", "browser_restore_session",
    "desktop_screenshot", "desktop_click", "desktop_type", "desktop_key",
})

SPECIALIST_TOOL_NAMES: frozenset[str] = frozenset(
    {PUX_PREFIX + s for s in SPECIALISTS}
)


def _tail(text: str, n: int = 800) -> str:
    """Last ``n`` chars of ``text`` — keeps stderr tails (tracebacks, model
    messages) out of result envelopes without leaking megabytes. Mirrors the Go
    ``tailOutput`` helper."""
    return text if len(text) <= n else "..." + text[len(text) - n:]


def _result(obj: dict) -> str:
    """Serialize a tool-result dict to the exact JSON the Go bridge surfaced.

    The Go mcpserver marshals every tool result with
    ``json.MarshalIndent(v, "", "  ")`` (server.go:336) — 2-space indent AND
    sorted map keys at every level. Probed against the live bridge: list_skills
    emits ``'{\\n  "count": 0,\\n  "skills": []\\n}'``. ``sort_keys=True`` makes
    Python match Go's key ordering (Go sorts map keys; Python preserves
    insertion order by default) so the agent-visible output is byte-equivalent
    pre/post port. Verified 2026-07-03."""
    return json.dumps(obj, indent=2, sort_keys=True)


class _NoArgs(BaseModel):
    """Schema for argument-less tools (list_skills)."""


# --- python (8b) -----------------------------------------------------------

class _PythonArgs(BaseModel):
    code: str = Field(..., description="Python code to execute. Print output is captured and returned.")


_PYTHON_DESC = (
    "Execute Python code inside the sandbox. Print output is captured. "
    "Whatever the sandbox image ships with is available. Runs via docker exec "
    "(python3 -c)."
)


def _python_tool(exec_client: DockerExecClient) -> StructuredTool:
    def _run(code: str) -> str:
        if not code:
            return _result({"success": False, "error": "no code provided"})
        # shlex.quote hands arbitrary multi-line python to bash -c safely.
        out, exit_code = exec_client.exec(f"python3 -c {shlex.quote(code)}")
        if exit_code != 0:
            # Non-zero = traceback / syntax error. Surface output (stderr is
            # combined by exec) so the model can debug, mirroring the Go tool.
            return _result({"success": False, "error": f"python exited {exit_code}", "output": out})
        return _result({"success": True, "output": out})

    return StructuredTool(
        name=PUX_PREFIX + "python", description=_PYTHON_DESC,
        args_schema=_PythonArgs, func=_run,
    )


# --- skills (8c) — host FS at <project>/orgs/{_shared,<org>}/skills/ -------

def _parse_skill(raw: str) -> tuple[str, str]:
    """Pull (name, description) from SKILL.md frontmatter. Mirrors the Go
    ``skills.parse`` minimal key:value reader (no full YAML lib — two scalar
    fields). Absent frontmatter → empty strings (caller reports the file)."""
    name, desc = "", ""
    body = raw
    if raw.startswith("---"):
        parts = raw.split("---", 2)
        if len(parts) >= 3:
            fm = parts[1]
            body = parts[2]
            for line in fm.splitlines():
                line = line.strip()
                if not line or line.startswith("#") or ":" not in line:
                    continue
                key, _, val = line.partition(":")
                val = val.strip().strip('"').strip("'")
                if key.strip() == "name":
                    name = val
                elif key.strip() == "description":
                    desc = val
    return name, desc


_LIST_SKILLS_DESC = (
    "List SKILL.md files under the project's skills roots (the active org's "
    "skills first, then orgs/_shared/skills). Each skill is operator-authored "
    "markdown with model-facing instructions (debugging recipes, codebase "
    "conventions, domain knowledge). Call this when starting work on a project "
    "to see what specialized guidance is available; then call load_skill to "
    "read the ones that apply."
)


def _list_skills_tool(org: str | None = None) -> StructuredTool:
    def _run() -> str:
        items: list[dict] = []
        seen: set[str] = set()
        for root in _skills_dirs(org):
            for child in sorted(root.iterdir()):
                if not child.is_dir() or child.name in seen:
                    continue
                md = child / SKILL_FILE
                if not md.is_file():
                    continue
                seen.add(child.name)
                name, desc = _parse_skill(md.read_text())
                items.append({"name": name or child.name, "description": desc, "path": str(md)})
        return _result({"skills": items, "count": len(items)})

    return StructuredTool(
        name=PUX_PREFIX + "list_skills", description=_LIST_SKILLS_DESC,
        args_schema=_NoArgs, func=_run,
    )


class _LoadSkillArgs(BaseModel):
    name: str = Field(..., description="Skill name (the 'name' field from list_skills)")


_LOAD_SKILL_DESC = (
    "Load one skill's full markdown body by name (use list_skills first to "
    "discover names). Returns name, description, source path, and the markdown "
    "content. Read the content carefully — it carries operator-authored "
    "instructions specific to this project."
)


def _load_skill_tool(org: str | None = None) -> StructuredTool:
    def _run(name: str) -> str:
        if not name:
            return _result({"success": False, "error": "missing required parameter 'name'"})
        md: Path | None = None
        for root in _skills_dirs(org):
            candidate = root / name / SKILL_FILE
            if candidate.is_file():
                md = candidate
                break
        if md is None:
            # Match the Go tool's isError contract: a missing skill is an error
            # (not a silent empty body) so the model doesn't hallucinate content.
            return _result({"success": False, "error": f"skill {name!r} not found"})
        raw = md.read_text()
        nm, desc = _parse_skill(raw)
        # body = everything after the frontmatter block
        body = raw.split("---", 2)[2].strip() if raw.startswith("---") else raw.strip()
        return _result({"name": nm or name, "description": desc, "path": str(md), "content": body})

    return StructuredTool(
        name=PUX_PREFIX + "load_skill", description=_LOAD_SKILL_DESC,
        args_schema=_LoadSkillArgs, func=_run,
    )


# --- describe_image (8d) — driving-model PRIMARY, in-sandbox ONNX FALLBACK ----

_DESCRIBE_IMAGE_SCRIPT = "/usr/local/bin/describe_image.py"
_DESCRIBE_IMAGE_TIMEOUT = 120  # seconds; matches the Go tool's default
_IMAGE_FETCH_TIMEOUT = 60  # base64/curl round-trip to acquire bytes for the model

# Mirrors describe_image.py's DEFAULT_PROMPT so primary and fallback behavior
# are comparable for the same prompt.
_DEFAULT_VISION_PROMPT = (
    "Describe this image concisely. Focus on text, UI elements, and key visual features."
)

# Map common image extensions → MIME for the data: URL sent to the model.
_MIME_BY_EXT = {
    ".png": "image/png",
    ".jpg": "image/jpeg",
    ".jpeg": "image/jpeg",
    ".gif": "image/gif",
    ".webp": "image/webp",
    ".bmp": "image/bmp",
}

_VISION_UNAVAILABLE = (
    "Vision model is not downloaded. Run scripts/bootstrap-vision.sh from the "
    "host to enable. (This message means BOTH paths failed: the driving model "
    "could not describe the image, and the local ONNX fallback is not "
    "bootstrapped.)"
)
_VISION_DEPS_MISSING = (
    "Sandbox image is missing onnxruntime-genai. Rebuild with `task build` "
    "after pulling latest sandbox/Dockerfile."
)


def _image_mime(name: str) -> str:
    """MIME for an image path/URL by extension (default ``image/png``)."""
    return _MIME_BY_EXT.get(Path(name).suffix.lower(), "image/png")


def _model_name(model: object) -> str:
    """The model id of a ChatOpenAI instance (``.model_name`` / ``.model``)."""
    return getattr(model, "model_name", None) or getattr(model, "model", None) or "model"


def _acquire_image_b64(
    exec_client: DockerExecClient, image_path: str | None, image_url: str | None,
) -> tuple[str, str]:
    """Fetch image bytes from the sandbox (path or URL) → ``(base64, mime)``.

    Routed through ``docker exec`` so the sandbox fs + egress policy apply
    uniformly to both the model-primary path (here) and the ONNX fallback
    (``describe_image.py`` re-reads the same source). Raises on any failure —
    the caller (primary path) catches and falls back to ONNX."""
    if image_path:
        cmd = f"base64 -w0 {shlex.quote(image_path)}"
        mime = _image_mime(image_path)
    else:
        # -L follows redirects; egress ACLs in the sandbox apply.
        cmd = f"curl -s -L --max-time 30 {shlex.quote(image_url or '')} | base64 -w0"
        mime = _image_mime(image_url or "")
    out, exit_code = exec_client.exec(cmd, timeout=_IMAGE_FETCH_TIMEOUT)
    b64 = (out or "").strip()
    if exit_code != 0 or not b64:
        raise RuntimeError(f"image fetch exit {exit_code}: {_tail(out, 200)}")
    return b64, mime


def _invoke_primary_vision(model: object, b64: str, mime: str, prompt: str | None) -> str:
    """Send the image to the driving model as a native multimodal message and
    return its description text. Raises on any failure (empty/null content,
    API error, non-multimodal model) — the caller falls back to ONNX."""
    msg = HumanMessage(content=[
        {"type": "text", "text": prompt or _DEFAULT_VISION_PROMPT},
        {"type": "image_url", "image_url": {"url": f"data:{mime};base64,{b64}"}},
    ])
    resp = model.invoke([msg])
    content = getattr(resp, "content", None)
    # Some providers return a list of content blocks rather than a bare string.
    if isinstance(content, list):
        content = "".join(
            block.get("text", "") for block in content if isinstance(block, dict)
        )
    text = (content or "").strip() if isinstance(content, str) else ""
    if not text:
        raise RuntimeError("primary model returned empty content")
    return text


class _DescribeImageArgs(BaseModel):
    image_path: str | None = Field(
        None, description="Absolute path to image file inside the sandbox "
        "(e.g. /sandbox/workspace/foo.png)"
    )
    image_url: str | None = Field(
        None, description="URL of image to download and describe. Mutually "
        "exclusive with image_path."
    )
    prompt: str | None = Field(
        None, description="Optional instruction for the model (default: generic "
        "description). e.g. 'what text is on the sign?'"
    )


_DESCRIBE_IMAGE_DESC = (
    "Describe an image. PRIMARY path: the driving model (mimo-v2.5) reads the "
    "image natively via multimodal input — fast, no local model load. FALLBACK "
    "path: if the driving model can't see the image (non-multimodal model, API "
    "error, empty output), an in-sandbox ONNX vision model "
    "(Qwen3.5-2B-ONNX-OPT) describes it locally. Pass either an in-sandbox "
    "image path OR a URL. The result's `source` field reports which path "
    "produced the description (`primary` | `fallback` | `onnx`)."
)


def _describe_image_tool(exec_client: DockerExecClient, model: object | None = None) -> StructuredTool:
    def _run(
        image_path: str | None = None,
        image_url: str | None = None,
        prompt: str | None = None,
    ) -> str:
        if not image_path and not image_url:
            return _result({"success": False, "error": "one of image_path or image_url is required"})
        if image_path and image_url:
            return _result({"success": False, "error": "image_path and image_url are mutually exclusive"})

        # PRIMARY: the driving model's native multimodal vision (mimo-v2.5 by
        # default). Any failure here (model not multimodal, rate limit, empty
        # output, fetch error) is caught and we fall through to the ONNX
        # fallback — `primary_error` is preserved on the fallback result so the
        # fallback is observable, never silent.
        primary_error: str | None = None
        if model is not None:
            try:
                b64, mime = _acquire_image_b64(exec_client, image_path, image_url)
                desc = _invoke_primary_vision(model, b64, mime, prompt)
                return _result({
                    "success": True,
                    "description": desc,
                    "model": _model_name(model),
                    "source": "primary",
                })
            except Exception as exc:
                primary_error = str(exc)
        pe = {"primary_error": _tail(primary_error, 300)} if primary_error else {}

        # FALLBACK: in-sandbox ONNX (Qwen3.5-2B-ONNX-OPT) via describe_image.py.
        # describe_image.py takes --image XOR --image-url; shlex.quote is safe
        # for paths/URLs carrying spaces or quotes (Go used its shQ helper).
        parts = [f"python3 {_DESCRIBE_IMAGE_SCRIPT}"]
        parts += ["--image", shlex.quote(image_path)] if image_path else ["--image-url", shlex.quote(image_url)]
        if prompt:
            parts += ["--prompt", shlex.quote(prompt)]
        cmd = " ".join(parts)
        try:
            out, exit_code = exec_client.exec(cmd, timeout=_DESCRIBE_IMAGE_TIMEOUT)
        except ExecTimeout:
            return _result({
                "success": False, "reason": "timeout",
                "error": f"describe_image timed out after {_DESCRIBE_IMAGE_TIMEOUT}s",
                **pe,
            })
        except Exception as exc:  # container vanished / docker API error
            return _result({"success": False, "reason": "exec_failed", "error": str(exc), **pe})

        # Exit-code dispatch — describe_image.py contract (faithful 1:1 with the
        # Go DescribeImageTool): 0=success, 1=inference error, 2=model missing,
        # 3=onnxruntime-genai absent. Model-missing is NOT an error (the model
        # is optional); the others are real failures the agent can react to.
        if exit_code == 0:
            try:
                parsed = json.loads(out)
            except json.JSONDecodeError:
                return _result({
                    "success": False, "reason": "malformed_output",
                    "error": f"describe_image returned non-JSON: {_tail(out, 400)}",
                    **pe,
                })
            # source distinguishes "model couldn't do it, ONNX saved it"
            # (fallback) from "no model threaded, ONNX only" (onnx — the
            # --check / offline path).
            return _result({
                "success": True,
                "description": parsed.get("description", ""),
                "model": parsed.get("model", ""),
                "source": "fallback" if primary_error else "onnx",
                **pe,
            })
        if exit_code == 2:
            return _result({
                "success": False, "reason": "unavailable",
                "explanation": _VISION_UNAVAILABLE, "detail": _tail(out),
                **pe,
            })
        if exit_code == 3:
            return _result({
                "success": False, "reason": "deps_missing",
                "explanation": _VISION_DEPS_MISSING, "detail": _tail(out),
                **pe,
            })
        return _result({"success": False, "reason": "inference_failed", "error": _tail(out), **pe})

    return StructuredTool(
        name=PUX_PREFIX + "describe_image", description=_DESCRIBE_IMAGE_DESC,
        args_schema=_DescribeImageArgs, func=_run,
    )


# --- browser (8e) — in-sandbox sb_server.py via curl ------------------------

_SB_SERVER_ADDR = "http://127.0.0.1:9876"  # supervisord runs sb_server.py here IN the container
_BROWSER_TIMEOUT = 60  # matches the Go BrowserToolConfig default


def _sb_post(exec_client: DockerExecClient, endpoint: str, body_obj: dict | None,
             *, timeout: int = _BROWSER_TIMEOUT) -> str:
    """POST ``body_obj`` to the in-sandbox sb_server.py endpoint, return the
    parsed JSON re-serialized the way the Go bridge surfaced it.

    Faithful 1:1 with Go ``browserBase.postJSON``: ``curl -s -S --max-time N
    -X POST <addr><endpoint> -H 'Content-Type: application/json' -d <body>``
    run via docker exec. ``body_obj=None`` sends no ``-d`` (the screenshot
    endpoint's ``{}`` is sent as a literal empty-object body). sb_server's
    response is parsed then re-emitted via ``_result`` (indent+sort_keys) so it
    matches the Go ``MarshalIndent`` of the same map."""
    max_time = max(1, timeout)  # Go: max(1, ceil(timeout.Seconds())); 60s -> 60
    parts = [
        "curl -s -S",
        f"--max-time {max_time}",
        "-X POST",
        f"{_SB_SERVER_ADDR}{endpoint}",
        "-H 'Content-Type: application/json'",
    ]
    body = ""
    if body_obj is not None:
        body = json.dumps(body_obj)  # compact request body; sb_server parses it
        parts += ["-d", shlex.quote(body)]
    cmd = " ".join(parts)
    try:
        out, exit_code = exec_client.exec(cmd, timeout=timeout)
    except ExecTimeout:
        return _result({"success": False, "reason": "timeout",
                        "error": f"browser {endpoint}: timed out after {timeout}s"})
    except Exception as exc:  # container vanished / docker API error
        return _result({"success": False, "reason": "exec_failed",
                        "error": f"browser {endpoint}: {exc}"})
    if exit_code != 0:
        # curl non-zero = connection refused / --max-time / DNS — sb_server may
        # not be running, or the page fetch hung. (HTTP 4xx/5xx are exit 0 with
        # the body, handled by the JSON parse below.)
        return _result({"success": False, "reason": "exec_failed",
                        "error": f"browser {endpoint}: curl exit {exit_code}",
                        "detail": _tail(out, 400)})
    try:
        parsed = json.loads(out)
    except json.JSONDecodeError:
        return _result({"success": False, "reason": "malformed_response",
                        "error": f"browser {endpoint}: non-JSON response",
                        "detail": _tail(out, 400)})
    # Surface the WHOLE sb_server payload (screenshot/links/text/element_map) —
    # a generic extractor would drop fields different endpoints need.
    return _result(parsed)


_BROWSER_NAVIGATE_DESC = (
    "Open a URL in the sandbox's persistent Chrome. Returns page title, URL, "
    "text snippet, and a base64 screenshot with Set-of-Marks labels on "
    "interactive elements. The session persists — subsequent browser_click / "
    "browser_type / browser_screenshot calls operate on this page until you "
    "navigate again."
)


class _BrowserNavigateArgs(BaseModel):
    url: str = Field(..., description="Absolute URL including scheme (https://example.com)")


def _browser_navigate_tool(exec_client: DockerExecClient) -> StructuredTool:
    def _run(url: str) -> str:
        if not url:
            return _result({"success": False, "error": "url is required"})
        return _sb_post(exec_client, "/navigate", {"url": url})

    return StructuredTool(
        name=PUX_PREFIX + "browser_navigate", description=_BROWSER_NAVIGATE_DESC,
        args_schema=_BrowserNavigateArgs, func=_run,
    )


_BROWSER_CLICK_DESC = (
    "Click an element on the current page. Pass either a SoM label (integer "
    "from the labeled screenshot) or a CSS selector string. Returns the "
    "post-click page state (URL, title, screenshot)."
)


class _BrowserClickArgs(BaseModel):
    index: int | None = Field(None, description="SoM label (numbered box on interactive elements from the last screenshot)")
    selector: str | None = Field(None, description="CSS selector (e.g. 'button#submit'). Used when index is omitted.")


def _browser_click_tool(exec_client: DockerExecClient) -> StructuredTool:
    def _run(index: int | None = None, selector: str | None = None) -> str:
        # Faithful to Go's marshalArgs: it marshals whatever the model passed
        # (index XOR selector per the schema oneOf); we build the body from the
        # present keys. Neither set → empty body (Go posts {}), sb_server errors.
        body: dict = {}
        if index is not None:
            body["index"] = index
        if selector is not None:
            body["selector"] = selector
        return _sb_post(exec_client, "/click", body)

    return StructuredTool(
        name=PUX_PREFIX + "browser_click", description=_BROWSER_CLICK_DESC,
        args_schema=_BrowserClickArgs, func=_run,
    )


_BROWSER_TYPE_DESC = (
    "Type text into a form field on the current page. Uses CDP character-by-"
    "character input (React-safe — fires real DOM events). Pass either a SoM "
    "label or CSS selector to identify the target input."
)


class _BrowserTypeArgs(BaseModel):
    text: str = Field(..., description="Text to type into the field")
    index: int | None = Field(None, description="SoM label of the target input")
    selector: str | None = Field(None, description="CSS selector of the target input")


def _browser_type_tool(exec_client: DockerExecClient) -> StructuredTool:
    def _run(text: str, index: int | None = None, selector: str | None = None) -> str:
        # Go validates text required AND (index or selector) before marshaling.
        if not text:
            return _result({"success": False, "error": "text is required"})
        if index is None and not selector:
            return _result({"success": False, "error": "either index or selector is required"})
        body = {"text": text}
        if index is not None:
            body["index"] = index
        if selector is not None:
            body["selector"] = selector
        return _sb_post(exec_client, "/type", body)

    return StructuredTool(
        name=PUX_PREFIX + "browser_type", description=_BROWSER_TYPE_DESC,
        args_schema=_BrowserTypeArgs, func=_run,
    )


_BROWSER_SCREENSHOT_DESC = (
    "Capture the current browser state as a labeled screenshot. Returns base64 "
    "PNG + SoM-numbered boxes on interactive elements. Use to re-orient after "
    "page updates, or to get fresh label numbers for clicking."
)


def _browser_screenshot_tool(exec_client: DockerExecClient) -> StructuredTool:
    def _run() -> str:
        # sb_server /read takes no args; Go posts "{}".
        return _sb_post(exec_client, "/read", {})

    return StructuredTool(
        name=PUX_PREFIX + "browser_screenshot", description=_BROWSER_SCREENSHOT_DESC,
        args_schema=_NoArgs, func=_run,
    )


_BROWSER_EVALUATE_DESC = (
    "Evaluate JavaScript on the current page, return the result. Power-tool "
    "escape hatch when navigate/click/type/screenshot don't fit (e.g. read "
    "window.__NEXT_DATA__, scroll to an element, fetch XHR). Runs in the page "
    "context — same-origin policy applies."
)


class _BrowserEvaluateArgs(BaseModel):
    code: str = Field(..., description="JavaScript expression to evaluate. Use 'return' for explicit values (e.g. 'return document.title')")


def _browser_evaluate_tool(exec_client: DockerExecClient) -> StructuredTool:
    def _run(code: str) -> str:
        if not code:
            return _result({"success": False, "error": "code is required"})
        return _sb_post(exec_client, "/evaluate", {"code": code})

    return StructuredTool(
        name=PUX_PREFIX + "browser_evaluate", description=_BROWSER_EVALUATE_DESC,
        args_schema=_BrowserEvaluateArgs, func=_run,
    )


_BROWSER_SEARCH_DESC = (
    "Search the web via DuckDuckGo and land on the results page. Returns the "
    "same labeled screenshot + page state as browser_navigate (the engine builds "
    "the DuckDuckGo URL for you). Use as the ENTRY POINT when you have a query "
    "but no URL. After searching, read the returned screenshot, pick a result by "
    "its SoM label, and browser_click it to open."
)


class _BrowserSearchArgs(BaseModel):
    query: str = Field(..., description="Natural-language search query (the engine URL-encodes it)")


def _browser_search_tool(exec_client: DockerExecClient) -> StructuredTool:
    def _run(query: str) -> str:
        if not query:
            return _result({"success": False, "error": "query is required"})
        return _sb_post(exec_client, "/search", {"query": query})

    return StructuredTool(
        name=PUX_PREFIX + "browser_search", description=_BROWSER_SEARCH_DESC,
        args_schema=_BrowserSearchArgs, func=_run,
    )


_BROWSER_SCROLL_DESC = (
    "Scroll the current page to reveal more content, then return a fresh "
    "labeled screenshot of the newly-visible region. Pass direction='down' or "
    "'up' for a viewport-sized jump; or set amount to a pixel count (e.g. 800) "
    "for a precise scroll. Essential on long pages — interactive elements below "
    "the fold have NO SoM label until you scroll them into view."
)


class _BrowserScrollArgs(BaseModel):
    direction: str = Field("down", description="'down' or 'up' (viewport-sized); ignored when amount>0")
    amount: int = Field(0, description="Pixel count to scroll (sign follows direction). 0 = use direction for a viewport jump.")


def _browser_scroll_tool(exec_client: DockerExecClient) -> StructuredTool:
    def _run(direction: str = "down", amount: int = 0) -> str:
        return _sb_post(exec_client, "/scroll", {"direction": direction, "amount": amount})

    return StructuredTool(
        name=PUX_PREFIX + "browser_scroll", description=_BROWSER_SCROLL_DESC,
        args_schema=_BrowserScrollArgs, func=_run,
    )


_BROWSER_GO_BACK_DESC = (
    "Navigate back to the previous page in history. Returns the prior page's "
    "labeled screenshot. Use when a navigation took you somewhere unhelpful and "
    "you want to undo it without re-searching or re-typing a URL."
)


def _browser_go_back_tool(exec_client: DockerExecClient) -> StructuredTool:
    def _run() -> str:
        return _sb_post(exec_client, "/go_back", {})

    return StructuredTool(
        name=PUX_PREFIX + "browser_go_back", description=_BROWSER_GO_BACK_DESC,
        args_schema=_NoArgs, func=_run,
    )


_BROWSER_WAIT_DESC = (
    "Pause for up to 30 seconds (server clamps; default 2) for async content to "
    "load, then return a fresh labeled screenshot. Use after navigate/click/type "
    "when the page is still loading or a JS render is in flight — a cheap way to "
    "let the DOM settle before re-reading. Prefer this over guessing that a "
    "screenshot is current."
)


class _BrowserWaitArgs(BaseModel):
    seconds: int = Field(2, description="How long to wait; server clamps to 30")


def _browser_wait_tool(exec_client: DockerExecClient) -> StructuredTool:
    def _run(seconds: int = 2) -> str:
        return _sb_post(exec_client, "/wait", {"seconds": seconds})

    return StructuredTool(
        name=PUX_PREFIX + "browser_wait", description=_BROWSER_WAIT_DESC,
        args_schema=_BrowserWaitArgs, func=_run,
    )


_BROWSER_FIND_TEXT_DESC = (
    "Scroll to and highlight the first occurrence of the given text on the "
    "current page (uses window.find). Returns a fresh labeled screenshot centered "
    "on the match. Use to locate specific information in a long page faster than "
    "scanning the whole screenshot."
)


class _BrowserFindTextArgs(BaseModel):
    text: str = Field(..., description="Substring to locate on the page")


def _browser_find_text_tool(exec_client: DockerExecClient) -> StructuredTool:
    def _run(text: str) -> str:
        if not text:
            return _result({"success": False, "error": "text is required"})
        return _sb_post(exec_client, "/find_text", {"text": text})

    return StructuredTool(
        name=PUX_PREFIX + "browser_find_text", description=_BROWSER_FIND_TEXT_DESC,
        args_schema=_BrowserFindTextArgs, func=_run,
    )


_BROWSER_EXTRACT_DESC = (
    "Extract structured text data from the current page: title, url, headings, "
    "paragraphs, lists, tables, and forms. The query is a free-text note of "
    "intent (defaults to 'extract all text content'). Returns {extracted:{...}}. "
    "Use to pull CLEAN text from an article or enumerate form fields, instead of "
    "OCR-ing the screenshot."
)


class _BrowserExtractArgs(BaseModel):
    query: str = Field("extract all text content", description="Free-text note of what you want (the engine extracts the same DOM structures regardless)")


def _browser_extract_tool(exec_client: DockerExecClient) -> StructuredTool:
    def _run(query: str = "extract all text content") -> str:
        return _sb_post(exec_client, "/extract", {"query": query})

    return StructuredTool(
        name=PUX_PREFIX + "browser_extract", description=_BROWSER_EXTRACT_DESC,
        args_schema=_BrowserExtractArgs, func=_run,
    )


_BROWSER_EXTRACT_IMAGES_DESC = (
    "List every <img> on the current page with its src + alt text. Returns "
    "{images:[{src,alt}], url}. Use to collect image URLs for downloading (pass "
    "a src to browser_download) or to inventory page media without parsing the "
    "screenshot."
)


def _browser_extract_images_tool(exec_client: DockerExecClient) -> StructuredTool:
    def _run() -> str:
        return _sb_post(exec_client, "/extract_images", {})

    return StructuredTool(
        name=PUX_PREFIX + "browser_extract_images", description=_BROWSER_EXTRACT_IMAGES_DESC,
        args_schema=_NoArgs, func=_run,
    )


_BROWSER_SAVE_SCREENSHOT_DESC = (
    "Save the current page as a clean PNG file at the given path (e.g. "
    "/tmp/evidence.png). DISTINCT from browser_screenshot (which returns a "
    "base64 SoM-labeled view for ACTING on the page): this writes an archival "
    "image to disk for evidence, attachments, or later describe_image analysis. "
    "Returns {screenshot_path, url}."
)


class _BrowserSaveScreenshotArgs(BaseModel):
    path: str | None = Field(None, description="Absolute sandbox path incl. .png extension. If omitted the engine generates one and returns it.")


def _browser_save_screenshot_tool(exec_client: DockerExecClient) -> StructuredTool:
    def _run(path: str | None = None) -> str:
        body: dict = {}
        if path:
            body["path"] = path
        return _sb_post(exec_client, "/screenshot", body)

    return StructuredTool(
        name=PUX_PREFIX + "browser_save_screenshot", description=_BROWSER_SAVE_SCREENSHOT_DESC,
        args_schema=_BrowserSaveScreenshotArgs, func=_run,
    )


_BROWSER_DOWNLOAD_DESC = (
    "Download a file from a direct URL to a path inside the sandbox (e.g. "
    "/tmp/report.pdf). Both url and path are required. Returns {url, path, size}. "
    "Use for direct file URLs (discovered via browser_extract_images or link "
    "hrefs) — NOT for pages that require interaction to produce the file."
)


class _BrowserDownloadArgs(BaseModel):
    url: str = Field(..., description="Direct file URL to fetch")
    path: str = Field(..., description="Absolute sandbox output path (incl. extension)")


def _browser_download_tool(exec_client: DockerExecClient) -> StructuredTool:
    def _run(url: str, path: str) -> str:
        if not url or not path:
            return _result({"success": False, "error": "url and path are both required"})
        return _sb_post(exec_client, "/download", {"url": url, "path": path})

    return StructuredTool(
        name=PUX_PREFIX + "browser_download", description=_BROWSER_DOWNLOAD_DESC,
        args_schema=_BrowserDownloadArgs, func=_run,
    )


_BROWSER_UPLOAD_DESC = (
    "Upload a local file into an <input type='file'> on the current page. "
    "Identify the input by CSS selector and pass a sandbox-absolute file_path "
    "(which must already exist). Returns {uploaded, selector, file}. Use to "
    "attach a resume/photo/document to a form whose upload UI can't be driven by "
    "browser_type."
)


class _BrowserUploadArgs(BaseModel):
    selector: str = Field(..., description="CSS selector of the <input type='file'>")
    file_path: str = Field(..., description="Absolute sandbox path of the file to upload (must exist)")


def _browser_upload_tool(exec_client: DockerExecClient) -> StructuredTool:
    def _run(selector: str, file_path: str) -> str:
        if not selector or not file_path:
            return _result({"success": False, "error": "selector and file_path are both required"})
        return _sb_post(exec_client, "/upload", {"selector": selector, "file_path": file_path})

    return StructuredTool(
        name=PUX_PREFIX + "browser_upload", description=_BROWSER_UPLOAD_DESC,
        args_schema=_BrowserUploadArgs, func=_run,
    )


_BROWSER_TABS_DESC = (
    "List all open browser tabs with their index, url, title, and which is "
    "active. Returns {tabs:[{index,url,title,active}]}. Use before "
    "browser_switch_tab to find the index of the tab you want, or to confirm how "
    "many tabs are open."
)


def _browser_tabs_tool(exec_client: DockerExecClient) -> StructuredTool:
    def _run() -> str:
        return _sb_post(exec_client, "/tabs", {})

    return StructuredTool(
        name=PUX_PREFIX + "browser_tabs", description=_BROWSER_TABS_DESC,
        args_schema=_NoArgs, func=_run,
    )


_BROWSER_NEW_TAB_DESC = (
    "Open a new browser tab to the given URL (default about:blank) and switch to "
    "it. Returns the new tab's labeled screenshot. Use to open a link without "
    "losing the current page, or to compare pages side-by-side."
)


class _BrowserNewTabArgs(BaseModel):
    url: str = Field("about:blank", description="URL to open in the new tab")


def _browser_new_tab_tool(exec_client: DockerExecClient) -> StructuredTool:
    def _run(url: str = "about:blank") -> str:
        return _sb_post(exec_client, "/new_tab", {"url": url})

    return StructuredTool(
        name=PUX_PREFIX + "browser_new_tab", description=_BROWSER_NEW_TAB_DESC,
        args_schema=_BrowserNewTabArgs, func=_run,
    )


_BROWSER_SWITCH_TAB_DESC = (
    "Switch to the browser tab at the given 0-based index. Returns that tab's "
    "labeled screenshot with fresh SoM labels. Use browser_tabs first to learn "
    "the index→url mapping."
)


class _BrowserSwitchTabArgs(BaseModel):
    index: int = Field(0, description="0-based tab index (see browser_tabs)")


def _browser_switch_tab_tool(exec_client: DockerExecClient) -> StructuredTool:
    def _run(index: int = 0) -> str:
        return _sb_post(exec_client, "/switch_tab", {"index": index})

    return StructuredTool(
        name=PUX_PREFIX + "browser_switch_tab", description=_BROWSER_SWITCH_TAB_DESC,
        args_schema=_BrowserSwitchTabArgs, func=_run,
    )


_BROWSER_CLOSE_TAB_DESC = (
    "Close the current browser tab and switch to the last remaining one (the "
    "engine refuses to close the final tab). Returns the now-active tab's "
    "labeled screenshot. Use to clean up after browser_new_tab."
)


def _browser_close_tab_tool(exec_client: DockerExecClient) -> StructuredTool:
    def _run() -> str:
        return _sb_post(exec_client, "/close_tab", {})

    return StructuredTool(
        name=PUX_PREFIX + "browser_close_tab", description=_BROWSER_CLOSE_TAB_DESC,
        args_schema=_NoArgs, func=_run,
    )


_BROWSER_DROPDOWN_OPTIONS_DESC = (
    "Read the options of a <select> dropdown. Identify the select element by SoM "
    "label (index) or CSS selector. Returns {selector, options, multiple, "
    "selected_count}. Call BEFORE browser_select_dropdown to learn the available "
    "option values and visible text."
)


class _BrowserDropdownOptionsArgs(BaseModel):
    index: int | None = Field(None, description="SoM label of the <select> element")
    selector: str | None = Field(None, description="CSS selector of the <select> element")


def _browser_dropdown_options_tool(exec_client: DockerExecClient) -> StructuredTool:
    def _run(index: int | None = None, selector: str | None = None) -> str:
        if index is None and not selector:
            return _result({"success": False, "error": "either index or selector is required"})
        body: dict = {}
        if index is not None:
            body["index"] = index
        if selector is not None:
            body["selector"] = selector
        return _sb_post(exec_client, "/dropdown_options", body)

    return StructuredTool(
        name=PUX_PREFIX + "browser_dropdown_options", description=_BROWSER_DROPDOWN_OPTIONS_DESC,
        args_schema=_BrowserDropdownOptionsArgs, func=_run,
    )


_BROWSER_SELECT_DROPDOWN_DESC = (
    "Choose an option in a <select> dropdown. Identify the select by SoM label "
    "(index) or CSS selector, then specify the option by its value attribute OR "
    "its visible text (exactly one). Returns the post-selection labeled "
    "screenshot. Use browser_dropdown_options first to discover the right value "
    "or text."
)


class _BrowserSelectDropdownArgs(BaseModel):
    index: int | None = Field(None, description="SoM label of the <select> element")
    selector: str | None = Field(None, description="CSS selector of the <select> element")
    value: str | None = Field(None, description="value attribute of the option to select (use XOR with text)")
    text: str | None = Field(None, description="Visible text of the option to select (use XOR with value)")


def _browser_select_dropdown_tool(exec_client: DockerExecClient) -> StructuredTool:
    def _run(index: int | None = None, selector: str | None = None,
             value: str | None = None, text: str | None = None) -> str:
        if index is None and not selector:
            return _result({"success": False, "error": "either index or selector is required"})
        if value is None and text is None:
            return _result({"success": False, "error": "either value or text is required"})
        body: dict = {}
        if index is not None:
            body["index"] = index
        if selector is not None:
            body["selector"] = selector
        if value is not None:
            body["value"] = value
        if text is not None:
            body["text"] = text
        return _sb_post(exec_client, "/select_dropdown", body)

    return StructuredTool(
        name=PUX_PREFIX + "browser_select_dropdown", description=_BROWSER_SELECT_DROPDOWN_DESC,
        args_schema=_BrowserSelectDropdownArgs, func=_run,
    )


_BROWSER_SAVE_SESSION_DESC = (
    "Save the current browser session (cookies + localStorage) to a JSON file "
    "(default /tmp/browser-session.json). Returns {saved, path, cookies, "
    "storage_items}. Call AFTER logging into an auth-heavy site so a later run "
    "can browser_restore_session without re-authenticating."
)


class _BrowserSaveSessionArgs(BaseModel):
    path: str = Field("/tmp/browser-session.json", description="Absolute sandbox path to write the session JSON")


def _browser_save_session_tool(exec_client: DockerExecClient) -> StructuredTool:
    def _run(path: str = "/tmp/browser-session.json") -> str:
        return _sb_post(exec_client, "/save_session", {"path": path})

    return StructuredTool(
        name=PUX_PREFIX + "browser_save_session", description=_BROWSER_SAVE_SESSION_DESC,
        args_schema=_BrowserSaveSessionArgs, func=_run,
    )


_BROWSER_RESTORE_SESSION_DESC = (
    "Restore a previously-saved browser session (cookies + localStorage) from a "
    "JSON file (default /tmp/browser-session.json). Returns {restored, path, "
    "cookies, storage_items}. Call right after browser_navigate to the site's "
    "domain, BEFORE other actions, to reuse saved auth."
)


class _BrowserRestoreSessionArgs(BaseModel):
    path: str = Field("/tmp/browser-session.json", description="Absolute sandbox path of a session JSON written by browser_save_session")


def _browser_restore_session_tool(exec_client: DockerExecClient) -> StructuredTool:
    def _run(path: str = "/tmp/browser-session.json") -> str:
        return _sb_post(exec_client, "/restore_session", {"path": path})

    return StructuredTool(
        name=PUX_PREFIX + "browser_restore_session", description=_BROWSER_RESTORE_SESSION_DESC,
        args_schema=_BrowserRestoreSessionArgs, func=_run,
    )


# --- desktop (8f) — X11 desktop via xdotool + desktop_observe.py ------------

_DISPLAY_ENV = "DISPLAY=:99"  # the sandbox's Xvfb display; prefixed on every xdotool cmd
_DESKTOP_TIMEOUT = 15  # matches the Go DesktopToolConfig default
_DESKTOP_OBSERVE = "/usr/local/bin/desktop_observe.py"


def _exec_desktop(exec_client: DockerExecClient, op: str, cmd: str,
                  *, timeout: int = _DESKTOP_TIMEOUT):
    """Run a desktop command; return ``(error_envelope | None, out, exit_code)``.

    On timeout / docker failure / non-zero exit, ``error_envelope`` is a
    ready-to-return JSON string (and the caller short-circuits). On success it
    is ``None`` and the caller synthesizes the result from ``out`` / its args.
    Mirrors Go's ``desktopBase.run`` (timeout + exec-failure → error), but the
    failure envelope is a JSON object (Go surfaced ``isError`` → ``"ERROR: …"``
    text via the bridge) — a deliberate, documented shape choice; the SUCCESS
    paths are byte-equivalent (the common case)."""
    try:
        out, exit_code = exec_client.exec(cmd, timeout=timeout)
    except ExecTimeout:
        return _result({"success": False, "reason": "timeout",
                        "error": f"desktop {op}: timed out after {timeout}s"}), "", 0
    except Exception as exc:
        return _result({"success": False, "reason": "exec_failed",
                        "error": f"desktop {op}: {exc}"}), "", 0
    if exit_code != 0:
        return _result({"success": False, "reason": "exec_failed",
                        "error": f"desktop {op}: exit {exit_code}",
                        "detail": _tail(out, 400)}), out, exit_code
    return None, out, exit_code


_DESKTOP_SCREENSHOT_DESC = (
    "Capture the sandbox desktop (X11 DISPLAY=:99) as a base64 PNG with OCR-"
    "detected text elements + window list. Each element has cx/cy (center "
    "pixel coords) — pass those to desktop_click. Use to orient before "
    "clicking or to read on-screen text."
)


def _desktop_screenshot_tool(exec_client: DockerExecClient) -> StructuredTool:
    def _run() -> str:
        cmd = f"{_DISPLAY_ENV} python3 {_DESKTOP_OBSERVE}"
        err, out, _ = _exec_desktop(exec_client, "screenshot", cmd)
        if err is not None:
            return err
        try:
            parsed = json.loads(out)
        except json.JSONDecodeError:
            return _result({"success": False, "reason": "malformed_response",
                            "error": "desktop_screenshot: non-JSON output",
                            "detail": _tail(out, 400)})
        # Faithful to Go: set ok=false when desktop_observe.py reported an
        # error key, ok=true otherwise.
        parsed["ok"] = False if parsed.get("error") else True
        return _result(parsed)

    return StructuredTool(
        name=PUX_PREFIX + "desktop_screenshot", description=_DESKTOP_SCREENSHOT_DESC,
        args_schema=_NoArgs, func=_run,
    )


_DESKTOP_CLICK_DESC = (
    "Click at pixel coordinates on the sandbox desktop. Pick coords from "
    "desktop_screenshot's element.cx/element.cy or the visible image. Optional "
    "button: 1=left (default), 2=middle, 3=right."
)


class _DesktopClickArgs(BaseModel):
    x: int = Field(..., description="X pixel coordinate (0 = left edge)")
    y: int = Field(..., description="Y pixel coordinate (0 = top edge)")
    button: int = Field(1, description="Mouse button: 1=left (default), 2=middle, 3=right")


def _desktop_click_tool(exec_client: DockerExecClient) -> StructuredTool:
    def _run(x: int, y: int, button: int = 1) -> str:
        if button < 1 or button > 3:
            return _result({"success": False, "error": "button must be 1, 2, or 3"})
        cmd = f"{_DISPLAY_ENV} xdotool mousemove --sync {x} {y} click {button}"
        err, _, _ = _exec_desktop(exec_client, "click", cmd)
        if err is not None:
            return err
        return _result({"ok": True, "x": x, "y": y, "button": button})

    return StructuredTool(
        name=PUX_PREFIX + "desktop_click", description=_DESKTOP_CLICK_DESC,
        args_schema=_DesktopClickArgs, func=_run,
    )


_DESKTOP_TYPE_DESC = (
    "Type text into the focused desktop window via xdotool. Optional clear "
    "(default true) Ctrl+A + Delete's existing field content first. Characters "
    "are sent as real X11 key events — works in any app."
)


class _DesktopTypeArgs(BaseModel):
    text: str = Field(..., description="Text to type")
    clear: bool = Field(True, description="Clear field first (default true)")


def _desktop_type_tool(exec_client: DockerExecClient) -> StructuredTool:
    def _run(text: str, clear: bool = True) -> str:
        if not text:
            return _result({"success": False, "error": "text is required"})
        parts = [_DISPLAY_ENV, "xdotool"]
        if clear:
            parts.append("key ctrl+a Delete")
        parts += ["type", "--clearmodifiers", shlex.quote(text)]
        cmd = " ".join(parts)
        err, _, _ = _exec_desktop(exec_client, "type", cmd)
        if err is not None:
            return err
        return _result({"ok": True, "text": text, "clear": clear})

    return StructuredTool(
        name=PUX_PREFIX + "desktop_type", description=_DESKTOP_TYPE_DESC,
        args_schema=_DesktopTypeArgs, func=_run,
    )


_DESKTOP_KEY_DESC = (
    "Press a key combo on the sandbox desktop via xdotool key. Examples: "
    "'Return', 'ctrl+c', 'alt+Tab', 'Escape', 'super'. For text input use "
    "desktop_type instead."
)


class _DesktopKeyArgs(BaseModel):
    keys: str = Field(..., description="xdotool key combo (e.g. 'Return', 'ctrl+c', 'alt+Tab')")


def _desktop_key_tool(exec_client: DockerExecClient) -> StructuredTool:
    def _run(keys: str) -> str:
        if not keys:
            return _result({"success": False, "error": "keys is required"})
        cmd = f"{_DISPLAY_ENV} xdotool key {shlex.quote(keys)}"
        err, _, _ = _exec_desktop(exec_client, "key", cmd)
        if err is not None:
            return err
        return _result({"ok": True, "keys": keys})

    return StructuredTool(
        name=PUX_PREFIX + "desktop_key", description=_DESKTOP_KEY_DESC,
        args_schema=_DesktopKeyArgs, func=_run,
    )


# --- registry ---------------------------------------------------------------

def build_native_specialists(
    exec_client: DockerExecClient, model: object | None = None,
    org: str | None = None,
) -> list[StructuredTool]:
    """Every native ``pux_sandbox_*`` specialist. ``exec_client`` is shared with
    the backend (one Docker client per process). Host-FS-only tools (skills)
    ignore it but take it for a uniform signature.

    ``model`` threads the driving LLM into ``describe_image`` so it can use the
    model's native multimodal vision as the PRIMARY path (ONNX fallback). The
    offline ``--check`` smoke passes ``model=None`` → ``describe_image`` is
    ONNX-only and spends no tokens.

    ``org`` scopes the skills tools: ``list_skills`` / ``load_skill`` search the
    active org's skills first, then ``_shared``, then other orgs' (org-local
    wins on collision). ``None`` (the ``--check`` path) searches all roots with
    no priority."""
    return [
        _python_tool(exec_client),
        _list_skills_tool(org),
        _load_skill_tool(org),
        _describe_image_tool(exec_client, model),
        _browser_navigate_tool(exec_client),
        _browser_click_tool(exec_client),
        _browser_type_tool(exec_client),
        _browser_screenshot_tool(exec_client),
        _browser_evaluate_tool(exec_client),
        _browser_search_tool(exec_client),
        _browser_scroll_tool(exec_client),
        _browser_go_back_tool(exec_client),
        _browser_wait_tool(exec_client),
        _browser_find_text_tool(exec_client),
        _browser_extract_tool(exec_client),
        _browser_extract_images_tool(exec_client),
        _browser_save_screenshot_tool(exec_client),
        _browser_download_tool(exec_client),
        _browser_upload_tool(exec_client),
        _browser_tabs_tool(exec_client),
        _browser_new_tab_tool(exec_client),
        _browser_switch_tab_tool(exec_client),
        _browser_close_tab_tool(exec_client),
        _browser_dropdown_options_tool(exec_client),
        _browser_select_dropdown_tool(exec_client),
        _browser_save_session_tool(exec_client),
        _browser_restore_session_tool(exec_client),
        _desktop_screenshot_tool(exec_client),
        _desktop_click_tool(exec_client),
        _desktop_type_tool(exec_client),
        _desktop_key_tool(exec_client),
    ]
