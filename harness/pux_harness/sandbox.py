"""PuxSandboxBackend — a deepagents ``BaseSandbox`` over the Go MCP sandbox.

Shape A (decided by probe, see ``PROVEN`` below): subclass
``deepagents.backends.sandbox.BaseSandbox`` and implement only its four
abstract primitives — ``execute``, ``id``, ``upload_files``, ``download_files``.
The inherited ``ls/read/write/edit/grep/glob`` (and all ``a*`` async variants)
run small ``python3``/``grep`` scripts *through our* ``execute()``, so they work
the moment ``execute()`` does. This is Phase-8-aligned: later, only
``execute()`` retargets to a direct ``docker exec``; the file ops keep working
as shell with no rewrite.

Why one ``execute()`` path for everything (incl. upload/download): the Go MCP
``bash`` tool already runs commands inside the pux-sandbox container (python3
3.10 + grep present, verified). Moving bytes as base64 through that same path
handles text **and** binary uniformly — no split between the text-only
``file_write`` and a notional binary path. ``upload_files``/``download_files``
are invoked by the skills/summarization/memory middleware (not just abstract
baggage), so they must be real.

PROVEN 2026-07-03 (task #17): ``backend.ls('/sandbox/workspace')`` and
``backend.read('AGENTS.md')`` return correct structured results against the live
container — the inherited ``_build_*_cmd`` scripts run cleanly in the image.
"""
from __future__ import annotations

import base64
import json
import shlex

from deepagents.backends.protocol import (
    ExecuteResponse,
    FileDownloadResponse,
    FileUploadResponse,
)
from deepagents.backends.sandbox import BaseSandbox

from pux_harness.bridge import PuxMCPClient


def _content_text(result: dict) -> str:
    """Join an MCP ``tools/call`` result's text content blocks into one string."""
    parts: list[str] = []
    for item in (result or {}).get("content", []) or []:
        if isinstance(item, dict) and item.get("type") == "text":
            parts.append(item.get("text", ""))
    return "\n".join(p for p in parts if p)


def _unwrap(text: str) -> str:
    """Peel the Go ``bash`` tool's success envelope.

    Success: the tool returns JSON ``{"output": "<stdout>"}`` (stdout newlines
    re-escaped). Failure (non-zero exit): ``isError: true`` with a combined
    message — handled by ``execute()`` before this is called. Anything that
    isn't an ``{"output": str}`` object (raw text, other JSON) is returned
    verbatim so we never corrupt legitimate output that happens to be JSON.
    """
    s = text.strip()
    if not (s.startswith("{") and s.endswith("}")):
        return text
    try:
        parsed = json.loads(s)
    except json.JSONDecodeError:
        return text
    if isinstance(parsed, dict) and isinstance(parsed.get("output"), str):
        return parsed["output"]
    return text


# python3 snippets used for byte-accurate upload/download via execute(). Base64
# carries the payload so text and binary share one path; the snippets are quoted
# as a single shell argv so embedded quotes/newlines in paths can't break out.
_UPLOAD_PY = (
    "import base64,sys,os;"
    "p=sys.argv[1];"
    "d=os.path.dirname(p);"
    "os.makedirs(d,exist_ok=True) if d else None;"
    "open(p,'wb').write(base64.b64decode(sys.argv[2]))"
)
_DOWNLOAD_PY = (
    "import base64,sys;"
    "sys.stdout.buffer.write(base64.b64encode(open(sys.argv[1],'rb').read()))"
)


class PuxSandboxBackend(BaseSandbox):
    """deepagents sandbox backed by the pux Go MCP server's ``bash`` tool."""

    def __init__(self, client: PuxMCPClient):
        self._client = client
        self._id: str | None = None
        # Every command run through native execute() — including the inherited
        # ls/read/glob/grep/write/edit (they all build a cmd + call execute()).
        # Observation-only: turns "did the subagent use native fs tools?" from
        # inference into direct evidence. pux_sandbox_bash is never bound, so
        # any entry here is, by construction, a NATIVE fs/shell call.
        self.execute_log: list[str] = []

    # --- the four abstract primitives --------------------------------------

    @property
    def id(self) -> str:
        # Not invoked on the framework hot path (no `.id` reads in
        # middleware/graph), but abstract-required. Lazily reflect the real
        # container hostname so it's meaningful if ever logged.
        if self._id is None:
            res = self._client.call_tool("bash", {"command": "cat /etc/hostname"})
            self._id = _unwrap(_content_text(res)).strip() or "pux-sandbox"
        return self._id

    def execute(self, command: str, *, timeout: int | None = None) -> ExecuteResponse:
        self.execute_log.append(command)
        res = self._client.call_tool("bash", {"command": command})
        text = _content_text(res)
        # Non-zero exit in the container: the inherited _build_*_cmd scripts
        # append `2>/dev/null` + `|| true`, so a non-zero exit here is a real
        # failure — surface it (exit_code=1) rather than masquerading as success.
        if res.get("isError"):
            return ExecuteResponse(output=text, exit_code=1, truncated=False)
        return ExecuteResponse(output=_unwrap(text), exit_code=0, truncated=False)

    def upload_files(self, files: list[tuple[str, bytes]]) -> list[FileUploadResponse]:
        out: list[FileUploadResponse] = []
        for path, data in files:
            b64 = base64.b64encode(data).decode("ascii")
            cmd = ("python3 -c " + shlex.quote(_UPLOAD_PY)
                   + f" {shlex.quote(path)} {shlex.quote(b64)}")
            res = self.execute(cmd)
            if res.exit_code != 0:
                out.append(FileUploadResponse(
                    path=path, error=f"upload failed: {res.output}"))
            else:
                out.append(FileUploadResponse(path=path, error=None))
        return out

    def download_files(self, paths: list[str]) -> list[FileDownloadResponse]:
        out: list[FileDownloadResponse] = []
        for path in paths:
            cmd = "python3 -c " + shlex.quote(_DOWNLOAD_PY) + f" {shlex.quote(path)}"
            res = self.execute(cmd)
            if res.exit_code != 0:
                out.append(FileDownloadResponse(
                    path=path, content=None, error=f"download failed: {res.output}"))
                continue
            # b64decode(validate=False) discards transport noise (\r\n); a
            # short payload means the script itself errored before printing.
            try:
                content = base64.b64decode(res.output.strip())
            except Exception as exc:  # noqa: BLE001 - surface, don't raise
                out.append(FileDownloadResponse(
                    path=path, content=None, error=f"b64 decode failed: {exc}"))
                continue
            out.append(FileDownloadResponse(path=path, content=content, error=None))
        return out

    # ls/read/write/edit/grep/glob + async variants: inherited from BaseSandbox.
