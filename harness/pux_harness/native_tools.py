"""Native specialist tools — the Python replacements for the Go MCP bridge's
``pux_sandbox_*`` specialists (Phase 8b–8f).

Each tool is a LangChain StructuredTool named ``pux_sandbox_<name>`` — the SAME
names the bridge used, so agent frontmatter and the contract resolver are
untouched. ``graph.py`` requests the ported specialists from here and asks the
Go bridge for the REST (``SPECIALIST_TOOLS - PORTED_SPECIALISTS``). When every
specialist is ported, the bridge carries nothing and is retired (Phase 8i).

Result contract fidelity: each tool returns the SAME JSON the Go MCP server
marshaled into its tool-call text blocks (verified against the live bridge —
e.g. ``list_skills`` returns ``{"skills": [...], "count": N}`` indented 2),
so the agent-visible output is byte-equivalent pre/post port.

Batch 1 (8b/8c):
  - ``python``        — ``python3 -c <code>`` via docker exec (was Go's
                        SandboxPythonTool; 96 LOC).
  - ``list_skills``   — host FS walk of ``<project>/.pi/skills/``.
  - ``load_skill``    — host FS read of one ``SKILL.md`` body.

Batch 2 (8d):
  - ``describe_image`` — ``/usr/local/bin/describe_image.py`` via docker exec
                         (was Go's DescribeImageTool; exit-code dispatch +
                         120s timeout + the model-missing ``success:false``
                         contract — NOT an error).

Batch 3 (8e):
  - ``browser_navigate`` / ``_click`` / ``_type`` / ``_screenshot`` /
    ``_evaluate`` — POST to the in-sandbox ``sb_server.py`` (127.0.0.1:9876)
    via ``curl`` run through docker exec (was Go's BrowserTool spec family).

**Bug fixed by the port:** the Go skills package read ``<root>/skills/`` which
does not exist in this repo (skills live at ``.pi/skills/`` per the pi-mono
layout); the live ``list_skills`` returned ``count: 0``. Probed before porting
(2026-07-03); the Python port reads the correct path.

Timeout note: the Go ``describe_image`` tool enforced a 120s deadline. The
shared ``DockerExecClient.exec(timeout=120)`` now enforces it (Phase 8d) and
raises ``ExecTimeout``, which this tool maps to ``reason:"timeout"``. The Go
``python`` tool's 60s deadline is not yet wired (fast calls dominate).
"""
from __future__ import annotations

import json
import shlex
from pathlib import Path

from langchain_core.tools import StructuredTool
from pydantic import BaseModel, Field

from pux_harness.docker_exec import DockerExecClient, ExecTimeout

PUX_PREFIX = "pux_sandbox_"
PROJECT_ROOT = Path(__file__).resolve().parents[2]
SKILLS_DIR = PROJECT_ROOT / ".pi" / "skills"
SKILL_FILE = "SKILL.md"

# Unprefixed specialist names implemented natively HERE. ``graph.py`` subtracts
# this from the full specialist set to decide what still comes from the bridge.
# Grows each batch; when it equals SPECIALIST_TOOLS, the bridge is retired.
PORTED_SPECIALISTS: frozenset[str] = frozenset({
    "python", "list_skills", "load_skill", "describe_image",
    "browser_navigate", "browser_click", "browser_type", "browser_screenshot", "browser_evaluate",
})


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


# --- skills (8c) — host FS at <project>/.pi/skills/ ------------------------

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
    "List SKILL.md files under the project's .pi/skills/ directory. Each skill "
    "is operator-authored markdown with model-facing instructions (debugging "
    "recipes, codebase conventions, domain knowledge). Call this when starting "
    "work on a project to see what specialized guidance is available; then call "
    "load_skill to read the ones that apply."
)


def _list_skills_tool() -> StructuredTool:
    def _run() -> str:
        items: list[dict] = []
        if SKILLS_DIR.is_dir():
            for child in sorted(SKILLS_DIR.iterdir()):
                if not child.is_dir():
                    continue
                md = child / SKILL_FILE
                if not md.is_file():
                    continue
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


def _load_skill_tool() -> StructuredTool:
    def _run(name: str) -> str:
        if not name:
            return _result({"success": False, "error": "missing required parameter 'name'"})
        md = SKILLS_DIR / name / SKILL_FILE
        if not md.is_file():
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


# --- describe_image (8d) — local ONNX vision via describe_image.py ----------

_DESCRIBE_IMAGE_SCRIPT = "/usr/local/bin/describe_image.py"
_DESCRIBE_IMAGE_TIMEOUT = 120  # seconds; matches the Go tool's default

_VISION_UNAVAILABLE = (
    "Vision model is not downloaded. Run scripts/bootstrap-vision.sh from the "
    "host to enable. Until then, image-aware reasoning falls back to whatever "
    "the driving LLM provides natively."
)
_VISION_DEPS_MISSING = (
    "Sandbox image is missing onnxruntime-genai. Rebuild with `task build` "
    "after pulling latest sandbox/Dockerfile."
)


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
    "Describe an image using local vision inference (Qwen3.5-2B-ONNX-OPT). "
    "Use when the driving LLM can't see the image directly, or when you want a "
    "fast local description without an external round-trip. Pass either an "
    "in-sandbox image path OR a URL (the script fetches it). Vision is OPTIONAL "
    "— if the model isn't downloaded, returns a friendly 'run "
    "scripts/bootstrap-vision.sh' message instead of an error."
)


def _describe_image_tool(exec_client: DockerExecClient) -> StructuredTool:
    def _run(
        image_path: str | None = None,
        image_url: str | None = None,
        prompt: str | None = None,
    ) -> str:
        if not image_path and not image_url:
            return _result({"success": False, "error": "one of image_path or image_url is required"})
        if image_path and image_url:
            return _result({"success": False, "error": "image_path and image_url are mutually exclusive"})
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
            })
        except Exception as exc:  # container vanished / docker API error
            return _result({"success": False, "reason": "exec_failed", "error": str(exc)})

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
                })
            return _result({
                "success": True,
                "description": parsed.get("description", ""),
                "model": parsed.get("model", ""),
            })
        if exit_code == 2:
            return _result({
                "success": False, "reason": "unavailable",
                "explanation": _VISION_UNAVAILABLE, "detail": _tail(out),
            })
        if exit_code == 3:
            return _result({
                "success": False, "reason": "deps_missing",
                "explanation": _VISION_DEPS_MISSING, "detail": _tail(out),
            })
        return _result({"success": False, "reason": "inference_failed", "error": _tail(out)})

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


# --- registry ---------------------------------------------------------------

def build_native_specialists(exec_client: DockerExecClient) -> list[StructuredTool]:
    """Every native ``pux_sandbox_*`` specialist. ``exec_client`` is shared with
    the backend (one Docker client per process). Host-FS-only tools (skills)
    ignore it but take it for a uniform signature."""
    return [
        _python_tool(exec_client),
        _list_skills_tool(),
        _load_skill_tool(),
        _describe_image_tool(exec_client),
        _browser_navigate_tool(exec_client),
        _browser_click_tool(exec_client),
        _browser_type_tool(exec_client),
        _browser_screenshot_tool(exec_client),
        _browser_evaluate_tool(exec_client),
    ]
