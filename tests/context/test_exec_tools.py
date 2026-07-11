"""Tests for the exec-dependent context tools (gaps 1 + 2).

Covers:
- Pure command-builder functions (no Docker needed)
- Tool surface presence/absence based on exec_client
- Tool behavior with a mock exec_client (no Docker)
- TTL cache for ctx_fetch_and_index (EventStore.get_blob_by_tool)
- Routing deny exemption for the 4 exec tools

All tests are offline — no Docker, no network. The exec_client is a mock that
records calls and returns canned output.
"""
from __future__ import annotations

import time
from types import SimpleNamespace

import pytest
from langchain_core.messages import ToolMessage

from pux_harness.context.events import EventStore
from pux_harness.context.exec_tools import (
    _build_exec_command,
    _build_fetch_command,
    _build_file_command,
    _supported_languages,
    build_exec_tools,
)
from pux_harness.context.tools import build_context_tools


# -- mock exec_client --------------------------------------------------------

class MockExecClient:
    """Records exec calls and returns canned (output, exit_code) pairs.
    Default: empty output, exit 0. Set ``responses`` for specific commands."""

    def __init__(self, *, output: str = "", exit_code: int = 0,
                 responses: dict[str, tuple[str, int]] | None = None):
        self.calls: list[str] = []
        self._output = output
        self._exit_code = exit_code
        self._responses = responses or {}

    def exec(self, command: str, timeout: int | None = None) -> tuple[str, int]:
        self.calls.append(command)
        for needle, (out, code) in self._responses.items():
            if needle in command:
                return out, code
        return self._output, self._exit_code


# =============================================================================
# Pure command-builder tests (no Docker, no store)
# =============================================================================

class TestBuildExecCommand:
    """_build_exec_command generates the exact shell string for each language.
    Pure function — no side effects, no Docker."""

    def test_python_inline(self):
        cmd = _build_exec_command("python", "print(1+1)")
        assert "python3 -c" in cmd
        assert "print(1+1)" in cmd

    def test_python_alias_py(self):
        assert _build_exec_command("py", "x") == _build_exec_command("python", "x")

    def test_javascript_uses_node_e(self):
        cmd = _build_exec_command("javascript", "console.log(1)")
        assert "node -e" in cmd

    def test_typescript_alias_ts(self):
        cmd = _build_exec_command("ts", "const x: number = 1")
        assert "node -e" in cmd

    def test_shell_uses_bash_c(self):
        cmd = _build_exec_command("shell", "echo hi")
        assert "bash -c" in cmd
        assert "echo hi" in cmd

    def test_bash_alias(self):
        assert _build_exec_command("bash", "echo") == _build_exec_command("shell", "echo")

    def test_ruby_uses_ruby_e(self):
        cmd = _build_exec_command("ruby", "puts 'hi'")
        assert "ruby -e" in cmd

    def test_perl_uses_perl_e(self):
        cmd = _build_exec_command("perl", "print 'hi'")
        assert "perl -e" in cmd

    def test_php_uses_php_r(self):
        cmd = _build_exec_command("php", "echo 'hi';")
        assert "php -r" in cmd

    def test_elixir_uses_elixir_e(self):
        cmd = _build_exec_command("elixir", 'IO.puts("hi")')
        assert "elixir -e" in cmd

    def test_r_uses_rscript_e(self):
        cmd = _build_exec_command("r", "print(1)")
        assert "Rscript -e" in cmd

    def test_go_writes_temp_file_and_runs(self):
        cmd = _build_exec_command("go", "package main\nfunc main(){}")
        assert "cat > /tmp/ctx_exec_" in cmd
        assert ".go" in cmd
        assert "go run" in cmd
        assert "PUX_EOF" in cmd

    def test_rust_writes_temp_file_and_compiles(self):
        cmd = _build_exec_command("rust", "fn main(){}")
        assert "cat > /tmp/ctx_exec_" in cmd
        assert ".rs" in cmd
        assert "rustc" in cmd
        assert ".bin" in cmd

    def test_csharp_writes_temp_file_and_uses_dotnet_script(self):
        cmd = _build_exec_command("csharp", 'Console.WriteLine("hi");')
        assert "cat > /tmp/ctx_exec_" in cmd
        assert ".csx" in cmd
        assert "dotnet script" in cmd

    def test_unknown_language_raises_with_supported_list(self):
        with pytest.raises(ValueError, match="unsupported language"):
            _build_exec_command("brainfuck", "+>")

    def test_case_insensitive(self):
        assert _build_exec_command("PYTHON", "x") == _build_exec_command("python", "x")
        assert _build_exec_command("JavaScript", "x") == _build_exec_command("javascript", "x")

    def test_supported_languages_includes_common_set(self):
        langs = set(_supported_languages())
        for expected in ("python", "javascript", "typescript", "shell", "ruby",
                         "perl", "php", "go", "rust", "elixir", "r", "csharp"):
            assert expected in langs, f"{expected} missing from supported languages"


class TestBuildFileCommand:
    """_build_file_command injects a FILE_CONTENT reader before the user code."""

    def test_python_file_reader(self):
        cmd = _build_file_command("/tmp/data.txt", "python", "print(len(FILE_CONTENT))")
        assert "python3 -c" in cmd
        assert "pathlib" in cmd
        assert "FILE_CONTENT" in cmd
        assert "/tmp/data.txt" in cmd
        assert "print(len(FILE_CONTENT))" in cmd

    def test_shell_file_reader(self):
        cmd = _build_file_command("/tmp/log.txt", "bash", 'echo "$FILE_CONTENT"')
        assert "bash -c" in cmd
        assert "FILE_CONTENT=$(cat" in cmd
        assert "/tmp/log.txt" in cmd

    def test_javascript_file_reader(self):
        cmd = _build_file_command("/tmp/x.json", "javascript", "console.log(FILE_CONTENT)")
        assert "node -e" in cmd
        assert "readFileSync" in cmd
        assert "FILE_CONTENT" in cmd

    def test_ruby_file_reader(self):
        cmd = _build_file_command("/tmp/x", "ruby", "puts FILE_CONTENT")
        assert "ruby -e" in cmd
        assert "File.read" in cmd
        assert "FILE_CONTENT" in cmd

    def test_unsupported_language_raises(self):
        with pytest.raises(ValueError, match="ctx_execute_file does not support"):
            _build_file_command("/tmp/x", "go", "fmt.Println(FILE_CONTENT)")


class TestBuildFetchCommand:
    """_build_fetch_command generates curl piped through an HTML-to-text converter."""

    def test_curl_piped_to_python3(self):
        cmd = _build_fetch_command("https://example.com")
        assert "curl -sL" in cmd
        assert "https://example.com" in cmd
        assert "python3 -c" in cmd  # the HTML-to-text converter
        assert "|" in cmd  # piped

    def test_url_is_shell_quoted(self):
        cmd = _build_fetch_command("https://example.com/$(whoami)")
        # The URL must be shlex-quoted so $() is NOT expanded
        assert "$(whoami)" in cmd  # literally present, not expanded
        assert "'https://example.com/" in cmd or "\"https://example.com/" in cmd


# =============================================================================
# Tool surface: exec_client controls presence of exec tools
# =============================================================================

class TestToolSurface:
    """build_context_tools includes exec tools only when exec_client is provided."""

    def test_without_exec_client_returns_six_base_tools(self, tmp_path):
        store = EventStore(tmp_path / "e.db")
        tools = build_context_tools(store)
        names = {t.name for t in tools}
        assert names == {"ctx_recall", "ctx_search", "ctx_index",
                         "ctx_stats", "ctx_doctor", "ctx_purge"}

    def test_with_exec_client_adds_four_exec_tools(self, tmp_path):
        store = EventStore(tmp_path / "e.db")
        ec = MockExecClient()
        tools = build_context_tools(store, exec_client=ec)
        names = {t.name for t in tools}
        assert "ctx_execute" in names
        assert "ctx_execute_file" in names
        assert "ctx_batch_execute" in names
        assert "ctx_fetch_and_index" in names
        # base tools still present
        assert "ctx_recall" in names
        assert "ctx_search" in names

    def test_exec_tools_at_end_of_list(self, tmp_path):
        """Order: retrieval first, indexing, meta, exec LAST."""
        store = EventStore(tmp_path / "e.db")
        tools = build_context_tools(store, exec_client=MockExecClient())
        names = [t.name for t in tools]
        exec_start = names.index("ctx_execute")
        # All base tools come before exec tools
        for base in ("ctx_recall", "ctx_search", "ctx_index",
                      "ctx_stats", "ctx_doctor", "ctx_purge"):
            assert names.index(base) < exec_start


# =============================================================================
# ctx_execute behavior (mock exec_client)
# =============================================================================

class TestCtxExecute:
    """ctx_execute runs code in the sandbox and returns stdout."""

    def _tools(self, store, ec):
        return {t.name: t for t in build_exec_tools(store, ec)}

    def test_returns_stdout(self, tmp_path):
        store = EventStore(tmp_path / "e.db")
        ec = MockExecClient(output="42\n")
        tools = self._tools(store, ec)
        out = tools["ctx_execute"].invoke({"language": "python", "code": "print(6*7)"})
        assert out == "42\n"
        assert len(ec.calls) == 1
        assert "python3 -c" in ec.calls[0]

    def test_nonzero_exit_returns_error_envelope(self, tmp_path):
        store = EventStore(tmp_path / "e.db")
        ec = MockExecClient(output="NameError: name 'x' is not defined", exit_code=1)
        tools = self._tools(store, ec)
        out = tools["ctx_execute"].invoke({"language": "python", "code": "print(x)"})
        assert "[ctx_execute] exit 1" in out
        assert "NameError" in out

    def test_unsupported_language_returns_error_string(self, tmp_path):
        store = EventStore(tmp_path / "e.db")
        ec = MockExecClient()
        tools = self._tools(store, ec)
        out = tools["ctx_execute"].invoke({"language": "brainfuck", "code": "+"})
        assert "unsupported language" in out
        assert len(ec.calls) == 0  # never called Docker


# =============================================================================
# ctx_batch_execute behavior
# =============================================================================

class TestCtxBatchExecute:
    """ctx_batch_execute runs N commands, indexes all outputs, optionally queries."""

    def _tools(self, store, ec):
        return {t.name: t for t in build_exec_tools(store, ec)}

    def test_indexes_all_outputs_and_returns_handles(self, tmp_path):
        store = EventStore(tmp_path / "e.db")
        ec = MockExecClient(responses={
            "ls": ("file1.py\nfile2.py", 0),
            "pwd": ("/sandbox/workspace", 0),
        })
        tools = self._tools(store, ec)
        out = tools["ctx_batch_execute"].invoke({
            "commands": [
                {"command": "ls", "label": "list"},
                {"command": "pwd", "label": "cwd"},
            ],
        })
        assert "2 command(s)" in out
        assert "list" in out and "file1.py" not in out  # output indexed, not inline
        assert "cwd" in out
        assert "ctx:" in out  # handles present

    def test_outputs_are_searchable_after_batch(self, tmp_path):
        """The indexed outputs surface in ctx_search after batch_execute."""
        store = EventStore(tmp_path / "e.db")
        ec = MockExecClient(output="the alpha signal triggered at 3pm", exit_code=0)
        tools = self._tools(store, ec)
        tools["ctx_batch_execute"].invoke({
            "commands": [{"command": "cat signals.log", "label": "signals"}],
        })
        from pux_harness.context.tools import build_context_tools as bct
        search = {t.name: t for t in bct(store)}["ctx_search"]
        assert "alpha signal" in search.invoke({"query": "alpha signal"})

    def test_inline_queries_return_matches(self, tmp_path):
        store = EventStore(tmp_path / "e.db")
        ec = MockExecClient(output="error: authentication failed at step 3", exit_code=0)
        tools = self._tools(store, ec)
        out = tools["ctx_batch_execute"].invoke({
            "commands": [{"command": "grep ERROR log", "label": "errors"}],
            "queries": ["authentication"],
        })
        assert "Q: authentication" in out
        assert "authentication failed" in out  # match surfaced inline


# =============================================================================
# ctx_fetch_and_index + TTL cache
# =============================================================================

class TestCtxFetchAndIndex:
    """ctx_fetch_and_index fetches a URL, indexes it, with TTL cache."""

    def _tools(self, store, ec):
        return {t.name: t for t in build_exec_tools(store, ec)}

    def test_fetch_indexes_and_returns_handle(self, tmp_path):
        store = EventStore(tmp_path / "e.db")
        ec = MockExecClient(output="<html><body>Hello world</body></html>", exit_code=0)
        tools = self._tools(store, ec)
        out = tools["ctx_fetch_and_index"].invoke({"url": "https://example.com"})
        assert "ctx:" in out
        assert "example.com" in out
        assert len(ec.calls) == 1  # one curl exec

    def test_fetched_content_is_searchable(self, tmp_path):
        store = EventStore(tmp_path / "e.db")
        ec = MockExecClient(output="React hooks API reference useEffect cleanup", exit_code=0)
        tools = self._tools(store, ec)
        tools["ctx_fetch_and_index"].invoke({"url": "https://react.dev/hooks", "source": "react-docs"})
        from pux_harness.context.tools import build_context_tools as bct
        search = {t.name: t for t in bct(store)}["ctx_search"]
        assert "useEffect" in search.invoke({"query": "useEffect cleanup"})

    def test_ttl_cache_second_call_does_not_refetch(self, tmp_path):
        """Within the TTL window, a re-fetch returns the cached handle without
        calling exec_client again."""
        store = EventStore(tmp_path / "e.db")
        ec = MockExecClient(output="cached content here", exit_code=0)
        tools = self._tools(store, ec)
        tools["ctx_fetch_and_index"].invoke({"url": "https://example.com", "ttl": 3600})
        out2 = tools["ctx_fetch_and_index"].invoke({"url": "https://example.com", "ttl": 3600})
        assert "cached" in out2.lower()
        assert len(ec.calls) == 1  # NOT re-fetched

    def test_force_bypasses_cache(self, tmp_path):
        store = EventStore(tmp_path / "e.db")
        ec = MockExecClient(output="first fetch", exit_code=0)
        tools = self._tools(store, ec)
        tools["ctx_fetch_and_index"].invoke({"url": "https://example.com", "ttl": 3600})
        ec._output = "second fetch response body"
        out2 = tools["ctx_fetch_and_index"].invoke(
            {"url": "https://example.com", "ttl": 3600, "force": True})
        assert len(ec.calls) == 2  # re-fetched despite cache
        assert "Fetched" in out2 and "Indexed under" in out2  # full re-index path

    def test_fetch_failure_returns_error(self, tmp_path):
        store = EventStore(tmp_path / "e.db")
        ec = MockExecClient(output="curl: connection refused", exit_code=7)
        tools = self._tools(store, ec)
        out = tools["ctx_fetch_and_index"].invoke({"url": "https://down.example"})
        assert "failed" in out.lower() or "exit 7" in out


# =============================================================================
# EventStore.get_blob_by_tool (TTL cache primitive)
# =============================================================================

class TestGetBlobByTool:
    """The TTL-cache lookup primitive on EventStore."""

    def test_returns_most_recent_blob_for_tool(self, tmp_path):
        store = EventStore(tmp_path / "e.db")
        store.stash_blob("old content", tool="ctx_fetch:https://x.example")
        time.sleep(0.01)
        store.stash_blob("new content", tool="ctx_fetch:https://x.example")
        store.flush()
        result = store.get_blob_by_tool("ctx_fetch:https://x.example")
        assert result is not None
        assert result["chars"] == len("new content")

    def test_returns_none_if_no_match(self, tmp_path):
        store = EventStore(tmp_path / "e.db")
        assert store.get_blob_by_tool("nonexistent") is None

    def test_stale_blob_returns_none_with_max_age(self, tmp_path):
        store = EventStore(tmp_path / "e.db")
        store.stash_blob("stale", tool="ctx_fetch:https://x.example")
        store.flush()
        # max_age_s=0 → immediately stale
        assert store.get_blob_by_tool(
            "ctx_fetch:https://x.example", max_age_s=0) is None

    def test_fresh_blob_returns_with_max_age(self, tmp_path):
        store = EventStore(tmp_path / "e.db")
        store.stash_blob("fresh", tool="ctx_fetch:https://x.example")
        store.flush()
        result = store.get_blob_by_tool(
            "ctx_fetch:https://x.example", max_age_s=3600)
        assert result is not None
        assert "handle" in result


# =============================================================================
# Routing deny exemption for exec tools
# =============================================================================

class TestRoutingExemption:
    """The 4 exec tools are exempt from the routing deny — they already handle
    output context-safely (stash/index). Denying network inside them would
    defeat their purpose."""

    def _req(self, name, args):
        return SimpleNamespace(tool_call={"name": name, "args": args, "id": "c1"}, state={})

    def test_ctx_execute_with_curl_is_not_denied(self):
        # Bare curl (no -s) so the shell deny pattern DEFINITELY matches — the
        # only thing saving this call is the _DENY_EXEMPT_TOOLS membership.
        from pux_harness.context.sandbox_routing import RoutingMiddleware
        mw = RoutingMiddleware()
        ran = []
        out = mw.wrap_tool_call(
            self._req("ctx_execute", {"language": "shell", "code": "curl https://example.com"}),
            lambda r: ran.append(True) or ToolMessage(content="executed", tool_call_id="c1", name="ctx_execute"),
        )
        assert ran == [True], "ctx_execute must be exempt from deny"
        assert out.content == "executed"

    def test_ctx_execute_with_requests_get_is_not_denied(self):
        from pux_harness.context.sandbox_routing import RoutingMiddleware
        mw = RoutingMiddleware()
        ran = []
        out = mw.wrap_tool_call(
            self._req("ctx_execute", {"language": "python", "code": "requests.get('https://x')"}),
            lambda r: ran.append(True) or ToolMessage(content="ok", tool_call_id="c1", name="ctx_execute"),
        )
        assert ran == [True]

    def test_ctx_batch_execute_with_curl_is_not_denied(self):
        # Bare curl (no -s) so the deny pattern definitely matches — exemption saves it.
        from pux_harness.context.sandbox_routing import RoutingMiddleware
        mw = RoutingMiddleware()
        ran = []
        out = mw.wrap_tool_call(
            self._req("ctx_batch_execute",
                      {"commands": [{"command": "curl https://example.com", "label": "fetch"}]}),
            lambda r: ran.append(True) or ToolMessage(content="indexed", tool_call_id="c1", name="ctx_batch_execute"),
        )
        assert ran == [True], "ctx_batch_execute must be exempt — it indexes output"

    def test_ctx_fetch_and_index_is_not_denied(self):
        from pux_harness.context.sandbox_routing import RoutingMiddleware
        mw = RoutingMiddleware()
        ran = []
        out = mw.wrap_tool_call(
            self._req("ctx_fetch_and_index", {"url": "https://example.com"}),
            lambda r: ran.append(True) or ToolMessage(content="fetched", tool_call_id="c1", name="ctx_fetch_and_index"),
        )
        assert ran == [True]

    def test_non_exempt_tool_still_denied(self, tmp_path):
        """The exemption is NARROW — only the 4 exec tools. A random tool
        carrying curl is still denied (universal deny unaffected)."""
        from pux_harness.context.sandbox_routing import RoutingMiddleware, _DENY_MSG
        mw = RoutingMiddleware()
        ran = []
        out = mw.wrap_tool_call(
            self._req("my_custom_tool", {"cmd": "curl https://example.com"}),
            lambda r: ran.append(True) or ToolMessage(content="ran", tool_call_id="c1", name="x"),
        )
        assert ran == [], "non-exempt tool must still be denied"
        assert out.content == _DENY_MSG
