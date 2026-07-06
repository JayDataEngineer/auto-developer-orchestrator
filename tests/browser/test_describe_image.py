"""Dispatch tests for the ``describe_image`` tool: driving-model PRIMARY with
in-sandbox ONNX FALLBACK.

These lock the dispatch contract without touching Docker or spending tokens:
a fake ``backend`` stands in for the sandbox filesystem (returns base64 via
``backend.read()``), a fake ``exec_client`` handles curl/ONNX commands, and a
fake model stands in for the driving LLM. The four behaviors that matter:

  1. model returns text  → ``source: "primary"``, ONNX never runs
  2. model raises / empty → ONNX fallback runs, ``source: "fallback"`` +
     ``primary_error`` preserved (fallback is observable, never silent)
  3. ``model=None``        → ONNX-only (the offline ``--check`` path),
     ``source: "onnx"``, no primary attempt
  4. arg validation        → mutual-exclusion + required-one-of error envelopes
"""
from __future__ import annotations

import json

import pytest

from pux_harness.sandbox.tools.describe_image import _describe_image_tool


class _Resp:
    def __init__(self, content):
        self.content = content


class _FakeModel:
    """Stands in for a ChatOpenAI. ``raises`` short-circuits invoke; ``content``
    can be a str, a list of content blocks, or empty to simulate null output."""

    def __init__(self, content="a red square", raises=None, name="mimo-v2.5"):
        self._content = content
        self._raises = raises
        self.model_name = name

    def invoke(self, msgs):
        if self._raises:
            raise self._raises
        return _Resp(self._content)


class _FakeBackend:
    """Stands in for PuxSandboxBackend. Returns base64 content from read()."""

    def __init__(self, b64="aGVsbG8="):
        self._b64 = b64
        self.reads: list[str] = []

    def read(self, path, offset=0, limit=2000):
        from deepagents.backends.protocol import FileData, ReadResult
        self.reads.append(path)
        return ReadResult(file_data=FileData(
            content=self._b64, encoding="base64"))


class _FakeExec:
    """Stands in for DockerExecClient. Routes on the command prefix:
    ``curl`` → URL fetch; anything else → the ONNX script.
    Records every command so tests can assert the ONNX path did/didn't run."""

    def __init__(self, *, b64="aGVsbG8=", onnx_out=None, onnx_exit=0):
        self._b64 = b64
        self._onnx_out = onnx_out if onnx_out is not None else json.dumps(
            {"description": "onnx-desc", "model": "Qwen3.5-2B-ONNX-OPT"}
        )
        self._onnx_exit = onnx_exit
        self.cmds: list[str] = []

    def exec(self, cmd, timeout=None):
        self.cmds.append(cmd)
        if cmd.startswith("curl"):
            return self._b64, 0
        return self._onnx_out, self._onnx_exit


def _invoke(tool, kwargs):
    """StructuredTool.func is the raw ``_run`` — call it directly with the
    kwargs dict and parse the JSON envelope it returns."""
    return json.loads(tool.func(**kwargs))


# --- case 1: primary success ------------------------------------------------

def test_primary_success_returns_primary_source_and_skips_onnx():
    backend = _FakeBackend()
    exec_ = _FakeExec()
    tool = _describe_image_tool(backend, exec_, _FakeModel(content="a diagram of a triangle"))
    res = _invoke(tool, {"image_path": "/sandbox/workspace/qc_01.png"})

    assert res["success"] is True
    assert res["source"] == "primary"
    assert res["description"] == "a diagram of a triangle"
    assert res["model"] == "mimo-v2.5"
    # backend.read() was used for the sandbox path — no ONNX script.
    assert backend.reads == ["/sandbox/workspace/qc_01.png"]
    assert [c for c in exec_.cmds if "describe_image.py" in c] == []


def test_primary_supports_content_block_list():
    # Some providers return content as a list of blocks.
    backend = _FakeBackend()
    exec_ = _FakeExec()
    blocks = [{"type": "text", "text": "block-desc"}]
    tool = _describe_image_tool(backend, exec_, _FakeModel(content=blocks))
    res = _invoke(tool, {"image_path": "/sandbox/workspace/x.png"})

    assert res["source"] == "primary"
    assert res["description"] == "block-desc"


# --- case 2: model failure → ONNX fallback ----------------------------------

def test_model_raises_falls_back_to_onnx_with_primary_error():
    backend = _FakeBackend()
    exec_ = _FakeExec()
    tool = _describe_image_tool(backend, exec_, _FakeModel(raises=RuntimeError("model 429")))
    res = _invoke(tool, {"image_path": "/sandbox/workspace/x.png"})

    assert res["success"] is True
    assert res["source"] == "fallback"
    assert res["model"] == "Qwen3.5-2B-ONNX-OPT"
    assert res["description"] == "onnx-desc"
    # The fallback is observable: primary_error is preserved.
    assert "model 429" in res["primary_error"]
    # ONNX actually ran.
    assert any("describe_image.py" in c for c in exec_.cmds)


def test_model_empty_content_falls_back_to_onnx():
    backend = _FakeBackend()
    exec_ = _FakeExec()
    tool = _describe_image_tool(backend, exec_, _FakeModel(content="   "))
    res = _invoke(tool, {"image_url": "https://example.com/a.png"})

    assert res["source"] == "fallback"
    assert res["description"] == "onnx-desc"
    assert "primary_error" in res


def test_both_paths_fail_surfaces_unavailable_with_primary_error():
    # ONNX returns exit 2 (model not downloaded) AND primary failed.
    backend = _FakeBackend()
    exec_ = _FakeExec(onnx_out="model not found", onnx_exit=2)
    tool = _describe_image_tool(backend, exec_, _FakeModel(raises=RuntimeError("no vision")))
    res = _invoke(tool, {"image_path": "/sandbox/workspace/x.png"})

    assert res["success"] is False
    assert res["reason"] == "unavailable"
    assert "primary_error" in res  # both paths failed, both visible


# --- case 3: model=None → ONNX only (offline --check path) ------------------

def test_no_model_is_onnx_only_no_primary_attempt():
    backend = _FakeBackend()
    exec_ = _FakeExec()
    tool = _describe_image_tool(backend, exec_, vision_model=None)
    res = _invoke(tool, {"image_path": "/sandbox/workspace/x.png"})

    assert res["success"] is True
    assert res["source"] == "onnx"
    assert res["description"] == "onnx-desc"
    # No primary_error key when primary was never attempted.
    assert "primary_error" not in res


# --- case 4: arg validation --------------------------------------------------

def test_requires_one_of_path_or_url():
    tool = _describe_image_tool(_FakeBackend(), _FakeExec(), _FakeModel())
    res = _invoke(tool, {})
    assert res["success"] is False
    assert "required" in res["error"]


def test_path_and_url_are_mutually_exclusive():
    tool = _describe_image_tool(_FakeBackend(), _FakeExec(), _FakeModel())
    res = _invoke(tool, {"image_path": "/a.png", "image_url": "https://x/a.png"})
    assert res["success"] is False
    assert "mutually exclusive" in res["error"]


# --- prompt plumbing --------------------------------------------------------

def test_prompt_forwarded_to_onnx_fallback():
    backend = _FakeBackend()
    exec_ = _FakeExec()
    tool = _describe_image_tool(backend, exec_, _FakeModel(raises=RuntimeError("boom")))
    _invoke(tool, {"image_path": "/x.png", "prompt": "what text is on the sign?"})
    onnx_cmd = next(c for c in exec_.cmds if "describe_image.py" in c)
    assert "--prompt" in onnx_cmd
    assert "what text is on the sign?" in onnx_cmd
