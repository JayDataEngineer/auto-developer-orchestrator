"""Tests for untested tool factories in pux_harness.sandbox.tools.

Covers: _python_tool, _parse_skill, _skills_dirs, _list_skills_tool,
_load_skill_tool, _tail, _result, _media_kind, _guess_mime, _model_name.
"""
from __future__ import annotations

import json
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import MagicMock, patch

import pytest

from pux_harness.sandbox.tools._media import _guess_mime, _media_kind, _model_name
from pux_harness.sandbox.tools.skills import _list_skills_tool, _load_skill_tool, _parse_skill
from pux_harness.sandbox.tools.python import _python_tool
from pux_harness.sandbox.tools._shared import _result, _skills_dirs, _tail


# --- _tail --------------------------------------------------------------------


def test_tail_short_text():
    assert _tail("hello", 10) == "hello"


def test_tail_long_text():
    result = _tail("a" * 200, 50)
    assert result.startswith("...")
    assert len(result) == 53  # "..." + 50


# --- _result ------------------------------------------------------------------


def test_result_sorts_keys():
    out = _result({"z": 1, "a": 2})
    parsed = json.loads(out)
    assert list(parsed.keys()) == ["a", "z"]


def test_result_indent():
    out = _result({"key": "val"})
    assert "  " in out  # 2-space indent


# --- _parse_skill -------------------------------------------------------------


def test_parse_skill_with_frontmatter():
    raw = '---\nname: my-skill\ndescription: A test skill\n---\nBody here'
    name, desc = _parse_skill(raw)
    assert name == "my-skill"
    assert desc == "A test skill"


def test_parse_skill_no_frontmatter():
    name, desc = _parse_skill("Just a body")
    assert name == ""
    assert desc == ""


def test_parse_skill_empty():
    name, desc = _parse_skill("")
    assert name == ""
    assert desc == ""


# --- _skills_dirs -------------------------------------------------------------


def test_skills_dirs_org_first():
    dirs = _skills_dirs("general")
    assert len(dirs) > 0
    # First should be the org's own skills dir (if it exists), else _shared
    # The important thing is _shared comes early
    assert any("_shared" in str(d) or "general" in str(d) for d in dirs[:2])


def test_skills_dirs_no_org():
    dirs = _skills_dirs(None)
    assert len(dirs) > 0
    # Should include _shared
    assert any("_shared" in str(d) for d in dirs)


# --- _python_tool -------------------------------------------------------------


def test_python_tool_empty_code():
    exec_client = MagicMock()
    tool = _python_tool(exec_client)
    result = tool.invoke({"code": ""})
    parsed = json.loads(result)
    assert parsed["success"] is False
    assert "no code" in parsed["error"]


def test_python_tool_success():
    exec_client = MagicMock()
    exec_client.exec.return_value = ("42\n", 0)
    tool = _python_tool(exec_client)
    result = tool.invoke({"code": "print(42)"})
    parsed = json.loads(result)
    assert parsed["success"] is True
    assert "42" in parsed["output"]


def test_python_tool_nonzero_exit():
    exec_client = MagicMock()
    exec_client.exec.return_value = ("Traceback...\n", 1)
    tool = _python_tool(exec_client)
    result = tool.invoke({"code": "bad code"})
    parsed = json.loads(result)
    assert parsed["success"] is False
    assert "python exited 1" in parsed["error"]


# --- _list_skills_tool --------------------------------------------------------


def test_list_skills_tool_empty(tmp_path):
    tool = _list_skills_tool()
    # The tool searches real org dirs; just verify it returns valid JSON
    result = tool.invoke({})
    parsed = json.loads(result)
    assert "skills" in parsed
    assert "count" in parsed
    assert isinstance(parsed["skills"], list)


def test_list_skills_tool_with_fake_skill(tmp_path, monkeypatch):
    skill_dir = tmp_path / "skills" / "test-skill"
    skill_dir.mkdir(parents=True)
    (skill_dir / "SKILL.md").write_text("---\nname: test\ndescription: desc\n---\nBody")

    monkeypatch.setattr(
        "pux_harness.sandbox.tools.skills._skills_dirs",
        lambda org=None: [tmp_path / "skills"],
    )
    tool = _list_skills_tool()
    result = tool.invoke({})
    parsed = json.loads(result)
    assert parsed["count"] == 1
    assert parsed["skills"][0]["name"] == "test"


# --- _load_skill_tool ---------------------------------------------------------


def test_load_skill_tool_missing_name():
    tool = _load_skill_tool()
    result = tool.invoke({"name": ""})
    parsed = json.loads(result)
    assert parsed["success"] is False


def test_load_skill_tool_not_found():
    tool = _load_skill_tool()
    result = tool.invoke({"name": "nonexistent"})
    parsed = json.loads(result)
    assert parsed["success"] is False
    assert "not found" in parsed["error"]


def test_load_skill_tool_success(tmp_path, monkeypatch):
    skill_dir = tmp_path / "skills" / "my-skill"
    skill_dir.mkdir(parents=True)
    (skill_dir / "SKILL.md").write_text("---\nname: my-skill\ndescription: A skill\n---\nDo this.")

    monkeypatch.setattr(
        "pux_harness.sandbox.tools.skills._skills_dirs",
        lambda org=None: [tmp_path / "skills"],
    )
    tool = _load_skill_tool()
    result = tool.invoke({"name": "my-skill"})
    parsed = json.loads(result)
    assert parsed["name"] == "my-skill"
    assert parsed["description"] == "A skill"
    assert "Do this." in parsed["content"]


# --- _media_kind --------------------------------------------------------------


def test_media_kind_image():
    assert _media_kind("photo.png") == "image"
    assert _media_kind("photo.JPG") == "image"
    assert _media_kind("pic.webp") == "image"


def test_media_kind_audio():
    assert _media_kind("song.mp3") == "audio"
    assert _media_kind("voice.wav") == "audio"
    assert _media_kind("track.flac") == "audio"


def test_media_kind_video():
    assert _media_kind("clip.mp4") == "video"
    assert _media_kind("movie.webm") == "video"


def test_media_kind_unknown():
    assert _media_kind("file.xyz") == "unknown"
    assert _media_kind("noext") == "unknown"


# --- _guess_mime --------------------------------------------------------------


def test_guess_mime_png():
    assert _guess_mime("test.png") == "image/png"


def test_guess_mime_unknown():
    # .xyz maps to chemical/x-xyz in Python's mimetypes; the point is it's not None
    mime = _guess_mime("test.xyz")
    assert mime is not None
    assert "/" in mime


# --- _model_name --------------------------------------------------------------


def test_model_name_with_model_name():
    m = SimpleNamespace(model_name="gpt-4")
    assert _model_name(m) == "gpt-4"


def test_model_name_with_model():
    m = SimpleNamespace(model="claude-3")
    assert _model_name(m) == "claude-3"


def test_model_name_fallback():
    m = object()
    assert _model_name(m) == "model"
