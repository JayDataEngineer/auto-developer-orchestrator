"""Tests for sandbox/scripts/desktop_observe.py — OCR parsing and word grouping.

Tests the pure functions (parse_tesseract_tsv, group_words, merge_group) with
synthetic data; no Docker, no X11, no tesseract needed.
"""
from __future__ import annotations

import importlib.util
from pathlib import Path

import pytest

_DESKTOP_PY = Path(__file__).resolve().parents[2] / "sandbox" / "scripts" / "desktop_observe.py"


def _load_module():
    spec = importlib.util.spec_from_file_location("desktop_observe", _DESKTOP_PY)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


mod = _load_module()


# --- parse_tesseract_tsv ------------------------------------------------------


def _make_tsv(rows: list[dict]) -> str:
    """Build a minimal tesseract TSV string from row dicts."""
    header = "level\tpage_num\tblock_num\tpar_num\tline_num\tword_num\tleft\ttop\twidth\theight\tconf\ttext"
    lines = [header]
    for r in rows:
        line = "\t".join(str(r.get(h, 0)) for h in [
            "level", "page_num", "block_num", "par_num", "line_num", "word_num",
            "left", "top", "width", "height", "conf", "text",
        ])
        lines.append(line)
    return "\n".join(lines)


def test_parse_tesseract_tsv_empty():
    assert mod.parse_tesseract_tsv("") == []


def test_parse_tesseract_tsv_header_only():
    tsv = "level\tpage_num\tblock_num\tpar_num\tline_num\tword_num\tleft\ttop\twidth\theight\tconf\ttext"
    assert mod.parse_tesseract_tsv(tsv) == []


def test_parse_tesseract_tsv_filters_low_conf():
    tsv = _make_tsv([
        {"level": 5, "left": 10, "top": 20, "width": 50, "height": 12, "conf": 10, "text": "low"},
        {"level": 5, "left": 70, "top": 20, "width": 50, "height": 12, "conf": 90, "text": "high"},
    ])
    result = mod.parse_tesseract_tsv(tsv)
    texts = [e["text"] for e in result]
    assert "high" in texts
    assert "low" not in texts


def test_parse_tesseract_tsv_filters_non_level5():
    tsv = _make_tsv([
        {"level": 4, "left": 10, "top": 20, "width": 50, "height": 12, "conf": 90, "text": "block"},
        {"level": 5, "left": 10, "top": 20, "width": 50, "height": 12, "conf": 90, "text": "word"},
    ])
    result = mod.parse_tesseract_tsv(tsv)
    assert len(result) == 1
    assert result[0]["text"] == "word"


def test_parse_tesseract_tsv_groups_adjacent_words():
    tsv = _make_tsv([
        {"level": 5, "left": 10, "top": 20, "width": 30, "height": 12, "conf": 90, "text": "Hello"},
        {"level": 5, "left": 45, "top": 20, "width": 30, "height": 12, "conf": 90, "text": "World"},
    ])
    result = mod.parse_tesseract_tsv(tsv)
    # Should be grouped into one element
    assert len(result) == 1
    assert result[0]["text"] == "Hello World"


# --- group_words --------------------------------------------------------------


def test_group_words_empty():
    assert mod.group_words([]) == []


def test_group_words_single():
    words = [{"text": "Hi", "left": 0, "top": 0, "width": 20, "height": 10, "right": 20, "bottom": 10}]
    result = mod.group_words(words)
    assert len(result) == 1
    assert result[0]["text"] == "Hi"
    assert result[0]["id"] == 1


def test_group_words_same_line_merge():
    words = [
        {"text": "A", "left": 0, "top": 0, "width": 10, "height": 10, "right": 10, "bottom": 10},
        {"text": "B", "left": 15, "top": 0, "width": 10, "height": 10, "right": 25, "bottom": 10},
    ]
    result = mod.group_words(words)
    assert len(result) == 1
    assert result[0]["text"] == "A B"


def test_group_words_different_lines():
    words = [
        {"text": "A", "left": 0, "top": 0, "width": 10, "height": 10, "right": 10, "bottom": 10},
        {"text": "B", "left": 0, "top": 50, "width": 10, "height": 10, "right": 10, "bottom": 60},
    ]
    result = mod.group_words(words)
    assert len(result) == 2


def test_group_words_gap_too_large():
    words = [
        {"text": "A", "left": 0, "top": 0, "width": 10, "height": 10, "right": 10, "bottom": 10},
        {"text": "B", "left": 200, "top": 0, "width": 10, "height": 10, "right": 210, "bottom": 10},
    ]
    result = mod.group_words(words)
    assert len(result) == 2


def test_group_words_assigns_ids():
    words = [
        {"text": "A", "left": 0, "top": 0, "width": 10, "height": 10, "right": 10, "bottom": 10},
        {"text": "B", "left": 0, "top": 50, "width": 10, "height": 10, "right": 10, "bottom": 60},
    ]
    result = mod.group_words(words)
    assert result[0]["id"] == 1
    assert result[1]["id"] == 2


# --- merge_group --------------------------------------------------------------


def test_merge_group_single_word():
    words = [{"text": "Hi", "left": 10, "top": 20, "width": 30, "height": 12, "right": 40, "bottom": 32}]
    result = mod.merge_group(words)
    assert result["text"] == "Hi"
    assert result["x"] == 10
    assert result["y"] == 20
    assert result["w"] == 30
    assert result["h"] == 12
    assert result["cx"] == 25
    assert result["cy"] == 26


def test_merge_group_multiple_words():
    words = [
        {"text": "Hello", "left": 10, "top": 20, "width": 50, "height": 12, "right": 60, "bottom": 32},
        {"text": "World", "left": 65, "top": 20, "width": 50, "height": 12, "right": 115, "bottom": 32},
    ]
    result = mod.merge_group(words)
    assert result["text"] == "Hello World"
    assert result["x"] == 10
    assert result["w"] == 105  # 115 - 10


def test_merge_group_text_truncated():
    long_text = "A" * 100
    words = [{"text": long_text, "left": 0, "top": 0, "width": 100, "height": 10, "right": 100, "bottom": 10}]
    result = mod.merge_group(words)
    assert len(result["text"]) <= 80
