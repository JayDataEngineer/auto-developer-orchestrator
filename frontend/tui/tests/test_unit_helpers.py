#!/usr/bin/env python3
"""
Unit tests for TUI pure functions (helpers extracted from source).
These test the logic isolated from Ink/React — no visual server needed.

Run with: python3 -m pytest tests/test_unit_helpers.py -v
Or:       uv run --with pytest python3 -m pytest frontend/tui/tests/test_unit_helpers.py -v
"""

# ── cleanModelName (from status-bar.tsx) ──

def clean_model_name(raw: str) -> str:
    """Port of status-bar.tsx cleanModelName."""
    import re
    name = re.sub(r'^[a-z][-a-z0-9]*/', '', raw)
    name = re.sub(r'-it$', '', name)
    name = re.sub(r'-instruct$', '', name)
    name = re.sub(r'^gemma-4-26b.*$', 'Gemma 4 27B', name)
    name = re.sub(r'^qwen[^-]*-?(\d+)', r'Qwen \1', name)
    name = re.sub(r'^deepseek-v(\d+)(.*)', r'DeepSeek V\1\2', name)
    return name[0].upper() + name[1:] if name else name


class TestCleanModelName:
    def test_strips_provider_prefix(self):
        assert clean_model_name("deepseek/deepseek-v4-flash") == "DeepSeek V4-flash"

    def test_strips_it_suffix(self):
        assert clean_model_name("gemma-4-26b-it") == "Gemma 4 27B"

    def test_strips_instruct_suffix(self):
        # -instruct stripped, then qwen regex captures version number
        result = clean_model_name("qwen2.5-32b-instruct")
        assert "instruct" not in result.lower()

    def test_gemma_27b_mapping(self):
        assert clean_model_name("gemma-4-26b-foo") == "Gemma 4 27B"

    def test_qwen_mapping(self):
        result = clean_model_name("qwen3.6-27b-q5_k_s")
        assert result.startswith("Qwen")
        assert "27" in result

    def test_deepseek_mapping(self):
        result = clean_model_name("deepseek-v4-flash")
        assert result.startswith("DeepSeek V4")

    def test_no_provider_prefix(self):
        assert clean_model_name("gpt-4o") == "Gpt-4o"

    def test_empty_string(self):
        assert clean_model_name("") == ""

    def test_unknown_model(self):
        assert clean_model_name("claude-3-opus") == "Claude-3-opus"

    def test_camelcase_first_char(self):
        assert clean_model_name("llama-3.1-70b")[0].isupper()


# ── formatTokens (from status-bar.tsx) ──

def format_tokens(n: int) -> str:
    """Port of status-bar.tsx formatTokens."""
    if n >= 1_000_000:
        return f"{(n / 1_000_000):.1f}M"
    if n >= 1_000:
        return f"{(n / 1_000):.1f}K"
    return str(n)


class TestFormatTokens:
    def test_under_1k(self):
        assert format_tokens(584) == "584"

    def test_1k_range(self):
        assert format_tokens(584321) == "584.3K"

    def test_exact_1k(self):
        assert format_tokens(1000) == "1.0K"

    def test_1m_range(self):
        assert format_tokens(1_200_000) == "1.2M"

    def test_exact_1m(self):
        assert format_tokens(1_000_000) == "1.0M"

    def test_large_number(self):
        assert format_tokens(128_000_000) == "128.0M"

    def test_zero(self):
        assert format_tokens(0) == "0"

    def test_999(self):
        assert format_tokens(999) == "999"

    def test_rounding_at_9999(self):
        result = format_tokens(9999)
        assert "K" in result


# ── clip (from providers-overlay.tsx) ──

def clip(s: str, max_len: int) -> str:
    """Port of providers-overlay.tsx clip function."""
    if max_len <= 0:
        return ""
    if len(s) <= max_len:
        return s
    if max_len <= 1:
        return "…"
    return s[:max_len - 1] + "…"


class TestClip:
    def test_shorter_than_max(self):
        assert clip("hello", 10) == "hello"

    def test_exact_length(self):
        assert clip("hello", 5) == "hello"

    def test_longer_than_max(self):
        result = clip("hello world", 8)
        assert "hello" in result
        assert result.endswith("…")

    def test_max_zero(self):
        assert clip("hello", 0) == ""

    def test_max_one(self):
        assert clip("hello", 1) == "…"

    def test_negative_max(self):
        assert clip("hello", -1) == ""

    def test_empty_string(self):
        assert clip("", 10) == ""

    def test_truncation_adds_ellipsis(self):
        result = clip("this is a long description that needs truncation", 30)
        assert len(result) <= 30
        assert result.endswith("…")


# ── formatTokenCount (from providers-overlay.tsx) ──

def format_token_count(n: int) -> str:
    """Port of providers-overlay.tsx formatTokenCount."""
    if n >= 1_000_000:
        m = n / 1_000_000
        return f"{int(m)}M" if m == int(m) else f"{m:.1f}M"
    if n >= 1_000:
        k = n / 1_000
        if k == int(k):
            return f"{int(k)}K"
        if n >= 1024 and n % 1024 == 0:
            kb = n / 1024
            if kb >= 1024:
                mb = kb / 1024
                return f"{int(mb)}M" if mb == int(mb) else f"{mb:.1f}M"
            return f"{int(kb)}K"
        return f"{k:.1f}K"
    return str(n)


class TestFormatTokenCount:
    def test_under_1k(self):
        assert format_token_count(128) == "128"

    def test_exact_1k(self):
        assert format_token_count(1000) == "1K"

    def test_1k_rounding(self):
        assert format_token_count(1500) == "1.5K"

    def test_1024_boundary(self):
        assert format_token_count(1024) == "1K"

    def test_8192(self):
        assert format_token_count(8192) == "8K"

    def test_128k(self):
        result = format_token_count(128000)
        assert "K" in result
        assert "128" in result

    def test_exact_1m(self):
        assert format_token_count(1_000_000) == "1M"

    def test_1_5m(self):
        assert format_token_count(1_500_000) == "1.5M"

    def test_zero(self):
        assert format_token_count(0) == "0"


# ── parseCommand (from commands.ts) ──

def parse_command(input_str: str):
    """Port of commands.ts parseCommand."""
    trimmed = input_str.strip()
    if not trimmed.startswith("/"):
        return None
    space_idx = trimmed.find(" ")
    if space_idx == -1:
        return trimmed[1:].lower(), ""
    return trimmed[1:space_idx].lower(), trimmed[space_idx + 1:]


class TestParseCommand:
    def test_simple_command(self):
        cmd, args = parse_command("/help")
        assert cmd == "help"
        assert args == ""

    def test_command_with_args(self):
        cmd, args = parse_command("/compact project=foo")
        assert cmd == "compact"
        assert args == "project=foo"

    def test_no_slash(self):
        assert parse_command("hello") is None

    def test_empty_string(self):
        assert parse_command("") is None

    def test_just_slash(self):
        cmd, args = parse_command("/")
        assert cmd == ""

    def test_uppercase_command(self):
        cmd, args = parse_command("/HELP")
        assert cmd == "help"

    def test_mixed_case(self):
        cmd, args = parse_command("/Clear All")
        assert cmd == "clear"
        assert args == "All"

    def test_leading_whitespace(self):
        cmd, args = parse_command("  /status")
        assert cmd == "status"

    def test_trailing_whitespace(self):
        cmd, args = parse_command("/status  ")
        assert cmd == "status"

    def test_multiple_args(self):
        cmd, args = parse_command("/model gemma-4-26b")
        assert cmd == "model"
        assert args == "gemma-4-26b"


# ── trunc (from custom-tool-ui.tsx / assistant-message.tsx) ──

def trunc(s: str, max_len: int) -> str:
    """Port of trunc helper — never cuts mid-word."""
    if len(s) <= max_len:
        return s
    cut = s[:max_len - 1]
    last_space = cut.rfind(" ")
    if last_space < max_len * 0.5:
        return cut + "…"
    return cut[:last_space] + "…"


class TestTrunc:
    def test_shorter_than_max(self):
        assert trunc("hello", 10) == "hello"

    def test_exact_length(self):
        assert trunc("hello", 5) == "hello"

    def test_truncates_at_word_boundary(self):
        result = trunc("hello beautiful world", 20)
        assert result.endswith("…")
        # Full word "beautiful" should be preserved, not cut
        assert "beautiful" in result

    def test_short_max_uses_hard_cut(self):
        result = trunc("a b c d e f g h i j k", 6)
        assert result == "a b…"
        assert len(result) <= 6

    def test_no_space_near_end(self):
        result = trunc("abcdefghijklmnop", 10)
        assert result == "abcdefghi…"

    def test_empty_string(self):
        assert trunc("", 10) == ""


# ── relativeTime (from session-switcher.tsx) ──

def relative_time(iso: str) -> str:
    """Port of session-switcher.tsx relativeTime."""
    from datetime import datetime, timezone, timedelta
    # Parse ISO timestamp
    dt = datetime.fromisoformat(iso.replace("Z", "+00:00"))
    now = datetime.now(timezone.utc)
    diff = now - dt
    mins = int(diff.total_seconds() / 60)
    if mins < 1:
        return "just now"
    if mins < 60:
        return f"{mins}m ago"
    hrs = mins // 60
    if hrs < 24:
        return f"{hrs}h ago"
    days = hrs // 24
    if days < 30:
        return f"{days}d ago"
    return dt.strftime("%m/%d/%Y")


class TestRelativeTime:
    def test_just_now(self):
        now = __import__('datetime').datetime.now(__import__('datetime').timezone.utc).isoformat()
        result = relative_time(now)
        assert result == "just now"

    def test_minutes_ago(self):
        from datetime import datetime, timezone, timedelta
        dt = (datetime.now(timezone.utc) - timedelta(minutes=5)).isoformat()
        assert relative_time(dt) == "5m ago"

    def test_hours_ago(self):
        from datetime import datetime, timezone, timedelta
        dt = (datetime.now(timezone.utc) - timedelta(hours=3)).isoformat()
        assert relative_time(dt) == "3h ago"

    def test_days_ago(self):
        from datetime import datetime, timezone, timedelta
        dt = (datetime.now(timezone.utc) - timedelta(days=5)).isoformat()
        assert relative_time(dt) == "5d ago"

    def test_one_minute_ago(self):
        from datetime import datetime, timezone, timedelta
        dt = (datetime.now(timezone.utc) - timedelta(minutes=1)).isoformat()
        result = relative_time(dt)
        assert result == "1m ago"


# ── parseInline (from markdown-text.tsx) ──

def parse_inline(line: str):
    """Port of markdown-text.tsx parseInline."""
    import re
    segments = []
    pattern = r'(\*\*(.+?)\*\*|\*(.+?)\*|`([^`]+)`|\[([^\]]+)\]\(([^)]+)\))'
    last_idx = 0
    for match in re.finditer(pattern, line):
        if match.start() > last_idx:
            segments.append({"text": line[last_idx:match.start()]})
        full = match.group(1)
        if full.startswith("**"):
            segments.append({"text": match.group(2), "bold": True})
        elif full.startswith("*"):
            segments.append({"text": match.group(3), "italic": True})
        elif full.startswith("`"):
            segments.append({"text": match.group(4), "code": True})
        elif full.startswith("["):
            segments.append({"text": match.group(5), "color": "cyan"})
        last_idx = match.start() + len(full)
    if last_idx < len(line):
        segments.append({"text": line[last_idx:]})
    return segments if segments else [{"text": line}]


class TestParseInline:
    def test_plain_text(self):
        segs = parse_inline("hello world")
        assert len(segs) == 1
        assert segs[0]["text"] == "hello world"

    def test_bold(self):
        segs = parse_inline("hello **world**")
        assert len(segs) == 2
        assert segs[1]["bold"] is True
        assert segs[1]["text"] == "world"

    def test_italic(self):
        segs = parse_inline("*italic* text")
        assert len(segs) == 2
        assert segs[0]["italic"] is True

    def test_code(self):
        segs = parse_inline("use `code` here")
        assert len(segs) == 3
        assert segs[1]["code"] is True

    def test_link(self):
        segs = parse_inline("click [here](url.com)")
        assert len(segs) == 2
        assert segs[1]["color"] == "cyan"
        assert segs[1]["text"] == "here"

    def test_mixed_formatting(self):
        segs = parse_inline("**bold** and `code`")
        assert len(segs) >= 3

    def test_empty_string(self):
        segs = parse_inline("")
        assert len(segs) == 0 or segs[0]["text"] == ""


# ── findMatches (from search-overlay.tsx) ──

def find_matches(texts, query):
    """Port of search-overlay.tsx findMatches."""
    if not query:
        return []
    q = query.lower()
    results = []
    for mi, text in enumerate(texts):
        lower = text.lower()
        idx = 0
        while True:
            idx = lower.find(q, idx)
            if idx == -1:
                break
            start = max(0, idx - 40)
            end = min(len(text), idx + len(query) + 40)
            snippet = text[start:end]
            if start > 0:
                snippet = "..." + snippet
            if end < len(text):
                snippet = snippet + "..."
            results.append({
                "messageIdx": mi,
                "snippet": snippet,
                "spanStart": idx,
                "spanEnd": idx + len(query),
            })
            idx += 1
    return results


class TestFindMatches:
    def test_empty_query(self):
        assert find_matches(["hello"], "") == []

    def test_basic_match(self):
        texts = ["hello world"]
        results = find_matches(texts, "world")
        assert len(results) == 1
        assert results[0]["messageIdx"] == 0

    def test_no_match(self):
        results = find_matches(["hello world"], "xyz")
        assert len(results) == 0

    def test_multiple_matches(self):
        texts = ["foo bar foo"]
        results = find_matches(texts, "foo")
        assert len(results) == 2

    def test_case_insensitive(self):
        texts = ["Hello World"]
        results = find_matches(texts, "hello")
        assert len(results) == 1

    def test_multiple_texts(self):
        texts = ["first text", "second text"]
        results = find_matches(texts, "text")
        assert len(results) == 2

    def test_empty_texts(self):
        assert find_matches([], "test") == []

    def test_snippet_creation(self):
        texts = ["a" * 100 + "xyz" + "b" * 100]
        results = find_matches(texts, "xyz")
        assert len(results) == 1
        assert "..." in results[0]["snippet"]


# ── getToolArgPreview (port of shared) ──

def get_tool_arg_preview(tool_name: str, args: dict | None, max_len: int = 40) -> str:
    """Port of shared/src/tool-arg-preview.ts getToolArgPreview."""
    if not args:
        return ""
    if tool_name == "bash":
        return (args.get("command") or args.get("cmd") or "")[:max_len]
    if tool_name in ("delegate_to", "delegate_async"):
        return (args.get("role") or args.get("instructions") or "")[:max_len]
    if tool_name in ("write_file", "file_edit", "edit_file", "read_file"):
        return (args.get("path") or args.get("file_path") or "")[:max_len]
    if tool_name in ("screenshot", "desktop_screenshot", "browser_screenshot", "web_screenshot",
                     "computer_screenshot", "take_screenshot", "observe", "desktop_observe"):
        return ""
    if tool_name in ("todo",):
        todos = args.get("todos", [])
        return f"{len(todos)} items" if todos else ""
    return str(args)[:max_len]


class TestGetToolArgPreview:
    def test_bash_command(self):
        preview = get_tool_arg_preview("bash", {"command": "ls -la"})
        assert "ls -la" in preview

    def test_delegate_to(self):
        preview = get_tool_arg_preview("delegate_to", {"role": "sarah"})
        assert "sarah" in preview

    def test_write_file(self):
        preview = get_tool_arg_preview("write_file", {"path": "/tmp/test.txt"})
        assert "/tmp/test.txt" in preview

    def test_read_file(self):
        preview = get_tool_arg_preview("read_file", {"file_path": "/tmp/test.txt"})
        assert "/tmp/test.txt" in preview

    def test_screenshot(self):
        preview = get_tool_arg_preview("screenshot", {})
        assert preview == ""

    def test_todo_empty(self):
        preview = get_tool_arg_preview("todo", {})
        assert preview == ""

    def test_todo_with_items(self):
        preview = get_tool_arg_preview("todo", {"todos": [{"content": "test"}]})
        assert "1 items" in preview

    def test_none_args(self):
        assert get_tool_arg_preview("bash", None) == ""

    def test_truncation(self):
        long = "a" * 100
        preview = get_tool_arg_preview("bash", {"command": long}, max_len=20)
        assert len(preview) == 20

    def test_bash_cmd_alias(self):
        preview = get_tool_arg_preview("bash", {"cmd": "npm test"})
        assert "npm test" in preview

    def test_delegate_instructions(self):
        preview = get_tool_arg_preview("delegate_async", {"instructions": "research"})
        assert "research" in preview


# ── tryExtractImageDataURI (from custom-tool-ui.tsx) ──

import re
DATA_URI_RE = re.compile(r'^data:image/(png|jpeg|jpg|gif|webp);base64,')

def try_extract_image_data_uri(result):
    """Port of custom-tool-ui.tsx tryExtractImageDataURI."""
    if isinstance(result, str):
        trimmed = result.strip()
        match = DATA_URI_RE.match(trimmed)
        if match:
            end_idx = trimmed.find('"', match.end())
            return trimmed[:end_idx] if end_idx > 0 else trimmed
        any_match = DATA_URI_RE.search(trimmed)
        if any_match:
            start = any_match.start()
            end = trimmed.find(" ", start + len(any_match.group()))
            return trimmed[start:end] if end > 0 else trimmed[start:]
    if isinstance(result, dict):
        for val in result.values():
            uri = try_extract_image_data_uri(val)
            if uri:
                return uri
    return None


class TestTryExtractImageDataURI:
    def test_direct_data_uri(self):
        uri = "data:image/png;base64,iVBORw0KGgo="
        assert try_extract_image_data_uri(uri) == uri

    def test_data_uri_in_json(self):
        result = {"screenshot": "data:image/png;base64,iVBORw0KGgo="}
        assert try_extract_image_data_uri(result) is not None

    def test_nested_result(self):
        result = {"result": {"screenshot": "data:image/png;base64,iVBORw=="}}
        assert try_extract_image_data_uri(result) is not None

    def test_none(self):
        assert try_extract_image_data_uri(None) is None

    def test_invalid_data_uri(self):
        assert try_extract_image_data_uri("not a uri") is None

    def test_empty_string(self):
        assert try_extract_image_data_uri("") is None

    def test_embedded_data_uri(self):
        # Direct standalone data URI (match path)
        result = 'data:image/png;base64,abc123'
        extracted = try_extract_image_data_uri(result)
        assert extracted is not None
        assert extracted == 'data:image/png;base64,abc123'

    def test_embedded_data_uri_in_text(self):
        # Search path — regex has ^ anchor so embedded won't match
        result = 'Here is image: data:image/png;base64,abc123 done'
        extracted = try_extract_image_data_uri(result)
        # Note: original JS has ^ anchor, so embedded won't match
        assert extracted is None


# ── Runner ──

if __name__ == "__main__":
    import pytest
    import sys
    sys.exit(pytest.main([__file__, "-v"]))
