"""Tests for sandbox/scripts/scripts.py — the agent's script manager.

All tests use tmp_path and monkeypatch SCRIPTS_DIR; no Docker needed.
"""
from __future__ import annotations

import importlib.util
import json
import sys
from pathlib import Path

import pytest

# Import scripts.py from the sandbox directory
_SCRIPTS_PY = Path(__file__).resolve().parents[2] / "sandbox" / "scripts" / "scripts.py"


def _load_scripts(tmp_path: Path):
    """Import scripts.py with SCRIPTS_DIR pointing at tmp_path."""
    spec = importlib.util.spec_from_file_location("scripts", _SCRIPTS_PY)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    mod.SCRIPTS_DIR = tmp_path
    return mod


@pytest.fixture
def scripts(tmp_path):
    return _load_scripts(tmp_path)


# --- _script_path -------------------------------------------------------------


def test_script_path_valid(scripts):
    p = scripts._script_path("my_script")
    assert p.name == "my_script.py"


def test_script_path_invalid_name(scripts):
    with pytest.raises(ValueError, match="invalid script name"):
        scripts._script_path("123bad")


def test_script_path_hyphen_rejected(scripts):
    with pytest.raises(ValueError):
        scripts._script_path("my-script")


def test_script_path_underscore_ok(scripts):
    p = scripts._script_path("_private")
    assert p.name == "_private.py"


# --- _validate_python ---------------------------------------------------------


def test_validate_python_valid(scripts):
    assert scripts._validate_python("x = 1\n") is None


def test_validate_python_syntax_error(scripts):
    err = scripts._validate_python("def f(\n")
    assert err is not None
    assert "line" in err


def test_validate_python_multiline(scripts):
    code = """
def hello():
    return "world"
"""
    assert scripts._validate_python(code) is None


# --- _extract_description -----------------------------------------------------


def test_extract_description_with_docstring(scripts, tmp_path):
    f = tmp_path / "test.py"
    f.write_text('"""My description."""\nx = 1\n')
    assert scripts._extract_description(f) == "My description."


def test_extract_description_no_docstring(scripts, tmp_path):
    f = tmp_path / "test.py"
    f.write_text("x = 1\n")
    assert scripts._extract_description(f) == ""


# --- _extract_hints -----------------------------------------------------------


def test_extract_hints_with_section(scripts, tmp_path):
    f = tmp_path / "test.py"
    f.write_text('"""\nDesc\n\nhints:\n  - Use when X\n  - Returns Y\n"""\n')
    hints = scripts._extract_hints(f)
    assert "Use when X" in hints
    assert "Returns Y" in hints


def test_extract_hints_no_section(scripts, tmp_path):
    f = tmp_path / "test.py"
    f.write_text('"""Just a description."""\n')
    assert scripts._extract_hints(f) == ""


# --- _format_hints_block ------------------------------------------------------


def test_format_hints_block(scripts):
    result = scripts._format_hints_block("hint one\nhint two")
    assert "hints:" in result
    assert "- hint one" in result
    assert "- hint two" in result


def test_format_hints_block_empty(scripts):
    assert scripts._format_hints_block("") == ""
    assert scripts._format_hints_block("  ") == ""


# --- _format_hints_one_line ---------------------------------------------------


def test_format_hints_one_line(scripts):
    result = scripts._format_hints_one_line("- hint one\n- hint two")
    assert "hint one | hint two" in result


def test_format_hints_one_line_truncated(scripts):
    long = "- " + "x" * 300
    result = scripts._format_hints_one_line(long)
    assert len(result) <= 200


# --- _build_docstring ---------------------------------------------------------


def test_build_docstring_full(scripts):
    result = scripts._build_docstring("My script", "hint one", "Footer.")
    assert '"""' in result
    assert "My script" in result
    assert "hints:" in result
    assert "Footer." in result


def test_build_docstring_no_description(scripts):
    assert scripts._build_docstring("", "hint", "footer") == ""


# --- make_script --------------------------------------------------------------


def test_make_script_creates_file(scripts):
    result = scripts.make_script("test_fn", "A test", "print('hi')")
    assert "created" in result
    assert result["name"] == "test_fn"
    assert (scripts.SCRIPTS_DIR / "test_fn.py").exists()


def test_make_script_no_overwrite(scripts):
    scripts.make_script("test_fn", "A test", "print('hi')")
    result = scripts.make_script("test_fn", "B test", "print('bye')")
    assert "error" in result
    assert "already exists" in result["error"]


def test_make_script_overwrite_ok(scripts):
    scripts.make_script("test_fn", "A", "print(1)")
    result = scripts.make_script("test_fn", "B", "print(2)", overwrite=True)
    assert "created" in result


def test_make_script_syntax_error(scripts):
    result = scripts.make_script("bad", "Bad", "def f(")
    assert "error" in result
    assert "syntax" in result["error"].lower()


# --- run_script ---------------------------------------------------------------


def test_run_script_success(scripts):
    scripts.make_script("adder", "Add two numbers", "print(2 + 3)")
    result = scripts.run_script("adder")
    assert result["success"] is True
    assert result["exit_code"] == 0
    assert "5" in result["stdout"]


def test_run_script_not_found(scripts):
    result = scripts.run_script("nonexistent")
    assert "error" in result
    assert "not found" in result["error"]


def test_run_script_nonzero_exit(scripts):
    scripts.make_script("fail", "Fail", "import sys; sys.exit(1)")
    result = scripts.run_script("fail")
    assert result["success"] is False
    assert result["exit_code"] == 1


def test_run_script_with_args(scripts):
    scripts.make_script("echo_args", "Echo args", "import sys; print(sys.argv[1:])")
    result = scripts.run_script("echo_args", args=["foo", "bar"])
    assert "foo" in result["stdout"]


# --- list_scripts -------------------------------------------------------------


def test_list_scripts_empty(scripts):
    result = scripts.list_scripts()
    assert result["count"] == 0
    assert result["scripts"] == []


def test_list_scripts_with_files(scripts):
    scripts.make_script("alpha", "First script", "print(1)")
    scripts.make_script("beta", "Second script", "print(2)")
    result = scripts.list_scripts()
    assert result["count"] == 2
    names = {s["name"] for s in result["scripts"]}
    assert names == {"alpha", "beta"}


# --- edit_script --------------------------------------------------------------


def test_edit_script_updates_code(scripts):
    scripts.make_script("ed", "Original", "print(1)")
    result = scripts.edit_script("ed", "print(99)")
    assert "updated" in result
    assert "99" in (scripts.SCRIPTS_DIR / "ed.py").read_text()


def test_edit_script_not_found(scripts):
    result = scripts.edit_script("nope", "print(1)")
    assert "error" in result


def test_edit_script_preserves_description(scripts):
    scripts.make_script("ed", "My desc", "print(1)")
    scripts.edit_script("ed", "print(2)")
    desc = scripts._extract_description(scripts.SCRIPTS_DIR / "ed.py")
    assert desc == "My desc"


def test_edit_script_syntax_error(scripts):
    scripts.make_script("ed", "Desc", "print(1)")
    result = scripts.edit_script("ed", "def f(")
    assert "error" in result


# --- show_script --------------------------------------------------------------


def test_show_script(scripts):
    scripts.make_script("show_me", "Show this", "print(42)")
    result = scripts.show_script("show_me")
    assert result["name"] == "show_me"
    assert "print(42)" in result["code"]


def test_show_script_not_found(scripts):
    result = scripts.show_script("nope")
    assert "error" in result


# --- remove_script ------------------------------------------------------------


def test_remove_script(scripts):
    scripts.make_script("to_del", "Delete me", "print(1)")
    result = scripts.remove_script("to_del")
    assert "removed" in result
    assert not (scripts.SCRIPTS_DIR / "to_del.py").exists()


def test_remove_script_not_found(scripts):
    result = scripts.remove_script("nope")
    assert "error" in result
