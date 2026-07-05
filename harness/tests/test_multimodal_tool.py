"""Dispatch tests for the two multimodal tools.

``multimodal`` (the DEFAULT) — media + prompt → multimodal model, end of story.
Returns the model's reasoning or an HONEST error; NEVER silently falls back,
because the value is the model's PROMPT-CONDITIONED judgment and a silent
downgrade would return a worse answer the caller can't distinguish.

``multimodal_mega`` (the opt-in resilient sibling) — model first, then a per-type
WATERFALL on failure (image→ONNX, audio→honest-unavailable, video→keyframes).

Both are locked here without touching Docker or spending tokens: a fake
``exec_client`` stands in for the sandbox and a fake model stands in for the
multimodal LLM. The fake model's ``rejects_video`` flag models the real failure
mode that justifies the video waterfall — the model rejects a raw ``video_url``
block but accepts per-frame ``image_url`` blocks.
"""
from __future__ import annotations

import json

import pytest

from pux_harness.sandbox.tools import _multimodal_tool, _multimodal_mega_tool


class _Resp:
    def __init__(self, content):
        self.content = content


class _FakeModel:
    """``raises`` short-circuits every invoke; ``content`` can be a str, a list
    of content blocks, or empty to simulate null output. ``rejects_video`` makes
    invoke raise ONLY when the message carries a ``video_url`` block (so a clip
    fails Tier 1 but per-frame image calls succeed — the realistic keyframe
    trigger). Records call count."""

    def __init__(self, content="a red square", raises=None, name="mimo-v2.5",
                 rejects_video=False):
        self._content = content
        self._raises = raises
        self.model_name = name
        self._rejects_video = rejects_video
        self.calls = 0

    def invoke(self, msgs):
        self.calls += 1
        if self._rejects_video and self._has_video_block(msgs):
            raise RuntimeError("model rejects video_url")
        if self._raises:
            raise self._raises
        return _Resp(self._content)

    @staticmethod
    def _has_video_block(msgs):
        for m in msgs:
            content = getattr(m, "content", None)
            if isinstance(content, list):
                for b in content:
                    if isinstance(b, dict) and b.get("type") == "video_url":
                        return True
        return False


def _onnx_json(desc="onnx-desc"):
    return json.dumps({"description": desc, "model": "Qwen3.5-2B-ONNX-OPT"})


class _FakeExec:
    """Routes on the command prefix: ``ffprobe`` (duration) / ``ffmpeg`` (frame
    extraction) / ``ls -1`` (frame glob) / ``base64``|``curl`` (byte fetch) /
    anything else (the ONNX describe_image.py script). ``ffmpeg_missing`` makes
    ffprobe exit 127."""

    def __init__(self, *, b64="aGVsbG8=", onnx_out=None, onnx_exit=0,
                 duration="10.0", ffmpeg_missing=False):
        self._b64 = b64
        self._onnx_out = onnx_out if onnx_out is not None else _onnx_json()
        self._onnx_exit = onnx_exit
        self._duration = duration
        self._ffmpeg_missing = ffmpeg_missing
        self.cmds: list[str] = []

    def exec(self, cmd, timeout=None):
        self.cmds.append(cmd)
        if cmd.startswith("ffprobe"):
            return ("command not found", 127) if self._ffmpeg_missing else (self._duration, 0)
        if cmd.startswith("ffmpeg"):
            return ("command not found", 127) if self._ffmpeg_missing else ("", 0)
        if cmd.startswith("ls -1") and "pux_multimodal_kf" in cmd:
            return ("\n".join(f"/tmp/pux_multimodal_kf/kf_{i:03d}.png"
                              for i in range(3)), 0)
        if cmd.startswith("base64") or cmd.startswith("curl"):
            return self._b64, 0
        return self._onnx_out, self._onnx_exit


def _invoke(tool, kwargs):
    return json.loads(tool.func(**kwargs))


# ============================================================================
# multimodal (the DEFAULT) — model answer or honest error, NO fallback
# ============================================================================

def test_simple_image_primary_success():
    exec_ = _FakeExec()
    tool = _multimodal_tool(exec_, _FakeModel(content="a diagram of a triangle"))
    res = _invoke(tool, {"media_path": "/sandbox/workspace/qc_01.png"})

    assert res["success"] is True
    assert res["source"] == "primary"
    assert res["media_type"] == "image"
    assert res["description"] == "a diagram of a triangle"
    assert res["model"] == "mimo-v2.5"
    # The simple tool never touches the ONNX path.
    assert [c for c in exec_.cmds if "describe_image.py" in c] == []


def test_simple_image_model_raises_is_honest_error_no_fallback():
    # The whole point of the simple tool: model failure is HONEST, not a silent
    # ONNX downgrade. The agent gets a clear error and decides what to do.
    exec_ = _FakeExec()
    tool = _multimodal_tool(exec_, _FakeModel(raises=RuntimeError("model 429")))
    res = _invoke(tool, {"media_path": "/sandbox/workspace/x.png"})

    assert res["success"] is False
    assert res["media_type"] == "image"
    assert res["reason"] == "model_failed"
    assert "model 429" in res["primary_error"]
    # CRITICAL: no ONNX fallback ran. The judgment wasn't silently swapped for
    # a generic description.
    assert [c for c in exec_.cmds if "describe_image.py" in c] == []


def test_simple_audio_model_raises_is_model_failed_not_unavailable():
    # The simple tool does NOT distinguish audio from image on failure — both are
    # "model_failed". (Only multimodal_mega has the audio_unavailable_offline
    # terminal tier, because only mega tries an offline fallback.)
    exec_ = _FakeExec()
    tool = _multimodal_tool(exec_, _FakeModel(raises=RuntimeError("audio 429")))
    res = _invoke(tool, {"media_path": "/sandbox/workspace/clip.mp3"})

    assert res["success"] is False
    assert res["media_type"] == "audio"
    assert res["reason"] == "model_failed"
    assert "audio 429" in res["primary_error"]


def test_simple_no_model_returns_no_model_error():
    # Offline --check path: vision_model=None. The simple tool can't do anything
    # (it has no fallback), so it says so honestly + points at the alternatives.
    exec_ = _FakeExec()
    tool = _multimodal_tool(exec_, vision_model=None)
    res = _invoke(tool, {"media_path": "/sandbox/workspace/x.png"})

    assert res["success"] is False
    assert res["reason"] == "no_model"
    assert res["media_type"] == "image"
    assert "multimodal_mega" in res["explanation"]


def test_simple_unknown_extension():
    tool = _multimodal_tool(_FakeExec(), _FakeModel())
    res = _invoke(tool, {"media_path": "/sandbox/workspace/data.xyz"})
    assert res["success"] is False
    assert "unsupported media type" in res["error"]


def test_simple_validation():
    tool = _multimodal_tool(_FakeExec(), _FakeModel())
    assert _invoke(tool, {})["success"] is False
    assert _invoke(tool, {"media_path": "/a.png",
                          "media_url": "https://x/a.png"})["success"] is False


# ============================================================================
# multimodal_mega (opt-in) — model first, then per-type waterfall
# ============================================================================

def test_mega_image_primary_success_skips_onnx():
    exec_ = _FakeExec()
    tool = _multimodal_mega_tool(exec_, _FakeModel(content="a red square"))
    res = _invoke(tool, {"media_path": "/sandbox/workspace/x.png"})

    assert res["success"] is True
    assert res["source"] == "primary"
    assert res["media_type"] == "image"
    assert [c for c in exec_.cmds if "describe_image.py" in c] == []


def test_mega_image_model_raises_falls_back_to_onnx_normalized_source():
    exec_ = _FakeExec()
    tool = _multimodal_mega_tool(exec_, _FakeModel(raises=RuntimeError("model 429")))
    res = _invoke(tool, {"media_path": "/sandbox/workspace/x.png"})

    assert res["success"] is True
    # mega normalizes to fallback:onnx (NOT describe_image's plain "fallback")
    assert res["source"] == "fallback:onnx"
    assert res["media_type"] == "image"
    assert res["description"] == "onnx-desc"
    assert "model 429" in res["primary_error"]
    assert any("describe_image.py" in c for c in exec_.cmds)


def test_mega_image_no_model_is_onnx_only():
    exec_ = _FakeExec()
    tool = _multimodal_mega_tool(exec_, vision_model=None)
    res = _invoke(tool, {"media_path": "/sandbox/workspace/x.png"})

    assert res["success"] is True
    assert res["source"] == "fallback:onnx"
    assert res["media_type"] == "image"
    assert "primary_error" not in res


def test_mega_audio_model_raises_is_honest_terminal_tier():
    # mega TRIES the model, then on failure reports the honest "no offline
    # audio fallback exists" — it never fabricates a whisper transcript.
    exec_ = _FakeExec()
    tool = _multimodal_mega_tool(exec_, _FakeModel(raises=RuntimeError("audio 429")))
    res = _invoke(tool, {"media_path": "/sandbox/workspace/clip.mp3"})

    assert res["success"] is False
    assert res["media_type"] == "audio"
    assert res["reason"] == "audio_unavailable_offline"
    assert "audio 429" in res["primary_error"]


def test_mega_video_primary_success_skips_keyframes():
    exec_ = _FakeExec()
    tool = _multimodal_mega_tool(exec_, _FakeModel(content="a car drives by"))
    res = _invoke(tool, {"media_path": "/sandbox/workspace/clip.mp4"})

    assert res["success"] is True
    assert res["source"] == "primary"
    assert res["media_type"] == "video"
    assert res["description"] == "a car drives by"
    assert [c for c in exec_.cmds if c.startswith("ffprobe")] == []


def test_mega_video_model_rejects_raw_video_falls_back_to_keyframes():
    # rejects_video: the whole-clip video_url call fails → keyframe extraction →
    # each frame's image_url call succeeds. This is the realistic trigger.
    exec_ = _FakeExec()
    tool = _multimodal_mega_tool(exec_, _FakeModel(content="frame action",
                                                   rejects_video=True))
    res = _invoke(tool, {"media_path": "/sandbox/workspace/clip.mp4"})

    assert res["success"] is True
    assert res["source"] == "fallback:keyframes"
    assert res["media_type"] == "video"
    assert res["frame_count"] == 3
    assert res["description"].count("[frame") == 3
    # Every frame went through the model (image_url), not ONNX.
    assert all(f["source"] == "primary" for f in res["frames"])
    assert [c for c in exec_.cmds if "describe_image.py" in c] == []
    assert any(c.startswith("ffprobe") for c in exec_.cmds)
    assert any(c.startswith("ffmpeg") for c in exec_.cmds)


def test_mega_video_no_model_falls_back_to_keyframes_onnx_per_frame():
    exec_ = _FakeExec()
    tool = _multimodal_mega_tool(exec_, vision_model=None)
    res = _invoke(tool, {"media_path": "/sandbox/workspace/clip.mp4"})

    assert res["success"] is True
    assert res["source"] == "fallback:keyframes"
    assert res["frame_count"] == 3
    assert all(f.get("source") in ("fallback", "onnx") for f in res["frames"])


def test_mega_video_ffmpeg_absent_returns_ffmpeg_missing():
    exec_ = _FakeExec(ffmpeg_missing=True)
    tool = _multimodal_mega_tool(exec_, _FakeModel(rejects_video=True))
    res = _invoke(tool, {"media_path": "/sandbox/workspace/clip.mp4"})

    assert res["success"] is False
    assert res["media_type"] == "video"
    assert res["reason"] == "ffmpeg_missing"


def test_mega_validation_matches_simple_tool():
    tool = _multimodal_mega_tool(_FakeExec(), _FakeModel())
    assert _invoke(tool, {})["success"] is False
    assert _invoke(tool, {"media_path": "/a.png",
                          "media_url": "https://x/a.png"})["success"] is False


# --- shared helper ----------------------------------------------------------

def test_media_kind_detection_by_extension():
    from pux_harness.sandbox.tools import _media_kind
    cases = {
        "x.png": "image", "x.JPG": "image", "x.wav": "audio",
        "x.MP3": "audio", "x.mp4": "video", "x.WEBM": "video",
    }
    for name, expected in cases.items():
        assert _media_kind(name) == expected, name
    assert _media_kind("data.xyz") == "unknown"
