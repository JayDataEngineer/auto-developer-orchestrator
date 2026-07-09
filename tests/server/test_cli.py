"""Tests for ``pux_harness.cli`` — the unified CLI.

Covers the HTTP helpers (``_post``, ``_get``, ``_die``, ``_print_block``),
the client subcommands (``cmd_*``), and the ``main()`` dispatcher's argument
parsing + routing.  Uses ``monkeypatch`` to stub ``httpx`` and the dispatch
targets; no server, no Docker, no tokens.
"""
from __future__ import annotations

import sys

import httpx
import pytest


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

@pytest.fixture
def cli():
    """Import the module once per test so monkeypatches are clean."""
    from pux_harness import cli as _cli
    return _cli


# ---------------------------------------------------------------------------
# _die
# ---------------------------------------------------------------------------

class TestDie:
    """``_die`` prints to stderr and raises ``SystemExit(1)``."""

    def test_exits_with_code_1(self, cli):
        with pytest.raises(SystemExit) as exc:
            cli._die("boom")
        assert exc.value.code == 1

    def test_prints_message_to_stderr(self, cli, capsys):
        with pytest.raises(SystemExit):
            cli._die("something went wrong")
        err = capsys.readouterr().err
        assert "pux: something went wrong" in err


# ---------------------------------------------------------------------------
# _print_block
# ---------------------------------------------------------------------------

class TestPrintBlock:
    """``_print_block`` prints a labelled block to stdout."""

    def test_prints_label_and_body(self, cli, capsys):
        cli._print_block("RESULTS", "line1\nline2\n")
        out = capsys.readouterr().out
        assert "=== RESULTS ===" in out
        assert "line1" in out
        assert "line2" in out

    def test_strips_trailing_newline(self, cli, capsys):
        cli._print_block("X", "hello\n")
        out = capsys.readouterr().out
        assert out.endswith("hello\n")  # stripped + our newline = exactly one


# ---------------------------------------------------------------------------
# _post / _get  (HTTP helpers)
# ---------------------------------------------------------------------------

class _FakeResponse:
    """Stand-in for ``httpx.Response``."""
    def __init__(self, status_code: int, json_data=None, text: str = ""):
        self.status_code = status_code
        self._json = json_data or {}
        self.text = text

    def json(self):
        return self._json


class TestPost:
    """``_post`` sends a JSON POST and returns parsed JSON."""

    def test_success_returns_json(self, cli, monkeypatch):
        monkeypatch.setattr(
            httpx, "post",
            lambda url, **kw: _FakeResponse(200, {"ok": True}),
        )
        assert cli._post("/test") == {"ok": True}

    def test_connect_error_dies(self, cli, monkeypatch, capsys):
        def _fail(*a, **kw):
            raise httpx.ConnectError("connection refused")
        monkeypatch.setattr(httpx, "post", _fail)
        with pytest.raises(SystemExit) as exc:
            cli._post("/test")
        assert exc.value.code == 1
        assert "can't reach" in capsys.readouterr().err

    def test_4xx_dies_with_detail(self, cli, monkeypatch, capsys):
        monkeypatch.setattr(
            httpx, "post",
            lambda url, **kw: _FakeResponse(404, json_data={"detail": "not found"}),
        )
        with pytest.raises(SystemExit) as exc:
            cli._post("/test")
        assert exc.value.code == 1
        assert "not found" in capsys.readouterr().err


class TestGet:
    """``_get`` sends a GET and returns parsed JSON."""

    def test_success_returns_json(self, cli, monkeypatch):
        monkeypatch.setattr(
            httpx, "get",
            lambda url, **kw: _FakeResponse(200, {"ok": True}),
        )
        assert cli._get("/test") == {"ok": True}

    def test_connect_error_dies(self, cli, monkeypatch, capsys):
        def _fail(*a, **kw):
            raise httpx.ConnectError("refused")
        monkeypatch.setattr(httpx, "get", _fail)
        with pytest.raises(SystemExit) as exc:
            cli._get("/test")
        assert exc.value.code == 1
        assert "can't reach" in capsys.readouterr().err

    def test_4xx_dies_with_text(self, cli, monkeypatch, capsys):
        monkeypatch.setattr(
            httpx, "get",
            lambda url, **kw: _FakeResponse(500, text="internal error"),
        )
        with pytest.raises(SystemExit) as exc:
            cli._get("/test")
        assert exc.value.code == 1
        assert "internal error" in capsys.readouterr().err


# ---------------------------------------------------------------------------
# Client subcommands (cmd_*)
# ---------------------------------------------------------------------------

class _PseudoCli:
    """Wraps the cli module with pre-stubbed ``_post`` / ``_get``.

    Each stub records calls and returns canned JSON so the ``cmd_*`` functions
    run without a real server.
    """
    def __init__(self, cli_mod):
        self._mod = cli_mod
        self.post_calls: list[tuple[str, dict]] = []
        self.get_calls: list[tuple[str]] = []
        self.post_results: dict[str, object] = {}
        self.get_results: dict[str, object] = {}

    def _post(self, path: str, **body):
        self.post_calls.append((path, body))
        return self.post_results.get(path, {})

    def _get(self, path: str):
        self.get_calls.append((path,))
        return self.get_results.get(path, {})

    def __getattr__(self, name):
        return getattr(self._mod, name)


@pytest.fixture
def pseudo(cli, monkeypatch):
    """Stub cli._post / cli._get with recording fakes."""
    p = _PseudoCli(cli)
    monkeypatch.setattr(cli, "_post", p._post)
    monkeypatch.setattr(cli, "_get", p._get)
    return p


class TestCmdAgents:
    """``cmd_agents`` lists agents from the server."""

    def test_lists_agents(self, pseudo, capsys):
        pseudo.post_results["/agents/search"] = [
            {"agent_id": "general", "description": "General purpose"},
            {"agent_id": "invest", "description": "Investment division"},
        ]
        pseudo._mod.cmd_agents()
        out = capsys.readouterr().out
        assert "general" in out
        assert "invest" in out
        assert pseudo.post_calls == [("/agents/search", {})]


class TestCmdDispatch:
    """``cmd_dispatch`` sends a task and prints the result."""

    def test_dispatches_task(self, pseudo, capsys):
        pseudo.post_results["/runs/wait"] = {
            "status": "success",
            "output": "task done",
            "thread_id": "th_123",
            "agent_id": "general",
        }
        pseudo._mod.cmd_dispatch("general", "do the thing", 60)
        out = capsys.readouterr().out
        assert "FINAL ANSWER" in out
        assert "task done" in out
        assert "th_123" in out
        # payload sent as string when no rubric
        path, body = pseudo.post_calls[0]
        assert path == "/runs/wait"
        assert body["agent_id"] == "general"

    def test_dispatches_with_rubric(self, pseudo):
        pseudo.post_results["/runs/wait"] = {
            "status": "success", "output": "ok", "thread_id": "t", "agent_id": "a",
        }
        pseudo._mod.cmd_dispatch("general", "task", 60, rubric="grade me")
        _, body = pseudo.post_calls[0]
        # rubric payload is a dict, not a bare string
        assert isinstance(body["input"], dict)
        assert body["input"]["rubric"] == "grade me"

    def test_error_status_exits(self, pseudo):
        pseudo.post_results["/runs/wait"] = {
            "status": "error", "error": "something broke", "thread_id": "t",
        }
        with pytest.raises(SystemExit):
            pseudo._mod.cmd_dispatch("general", "task", 60)


class TestCmdResume:
    """``cmd_resume`` lists recent threads."""

    def test_lists_threads(self, pseudo, capsys):
        pseudo.post_results["/threads/search"] = [
            {"thread_id": "th_1", "agent_id": "general", "created_at": "2025-01-01"},
        ]
        pseudo._mod.cmd_resume(org=None)
        out = capsys.readouterr().out
        assert "th_1" in out

    def test_empty(self, pseudo, capsys):
        pseudo.post_results["/threads/search"] = []
        pseudo._mod.cmd_resume(org=None)
        assert "(no threads)" in capsys.readouterr().out

    def test_filters_by_org(self, pseudo):
        pseudo.post_results["/threads/search"] = []
        pseudo._mod.cmd_resume(org="invest")
        assert pseudo.post_calls[0][1].get("agent_id") == "invest"


class TestCmdShow:
    """``cmd_show`` displays a thread's last message."""

    def test_shows_last_message(self, pseudo, capsys):
        pseudo.get_results["/threads/th_1"] = {
            "thread_id": "th_1",
            "agent_id": "general",
            "status": "running",
            "values": {
                "messages": [{"type": "ai", "content": "hello world"}],
            },
        }
        pseudo._mod.cmd_show("th_1")
        out = capsys.readouterr().out
        assert "th_1" in out
        assert "hello world" in out

    def test_empty_messages(self, pseudo, capsys):
        pseudo.get_results["/threads/th_1"] = {
            "thread_id": "th_1", "agent_id": "g", "status": "idle",
            "values": {},
        }
        pseudo._mod.cmd_show("th_1")
        assert "(no messages)" in capsys.readouterr().out


class TestCmdHistory:
    """``cmd_history`` shows checkpoint revisions."""

    def test_lists_history(self, pseudo, capsys):
        pseudo.get_results["/threads/th_1/history"] = [
            {"checkpoint_id": "cp_1", "next": ["cp_2"]},
            {"checkpoint_id": "cp_2", "next": []},
        ]
        pseudo._mod.cmd_history("th_1")
        out = capsys.readouterr().out
        assert "cp_1" in out
        assert "cp_2" in out


class TestCmdRun:
    """``cmd_run`` starts a background run."""

    def test_creates_run(self, pseudo, capsys):
        pseudo.post_results["/threads/th_1/runs"] = {
            "run_id": "run_42", "status": "queued",
        }
        pseudo._mod.cmd_run("th_1", "do stuff", 60)
        out = capsys.readouterr().out
        assert "run_42" in out
        assert "queued" in out

    def test_passes_recursion_limit(self, pseudo):
        pseudo.post_results["/threads/th_1/runs"] = {"run_id": "r"}
        pseudo._mod.cmd_run("th_1", "task", 30)
        _, body = pseudo.post_calls[0]
        assert body["recursion_limit"] == 30


class TestCmdWait:
    """``cmd_wait`` polls for a run's output."""

    def test_prints_output(self, pseudo, capsys):
        pseudo.get_results["/runs/r_1/wait"] = {
            "status": "completed", "output": "finished",
        }
        pseudo._mod.cmd_wait("r_1")
        out = capsys.readouterr().out
        assert "finished" in out

    def test_error_exits(self, pseudo):
        pseudo.get_results["/runs/r_1/wait"] = {
            "status": "error", "error": "failed",
        }
        with pytest.raises(SystemExit):
            pseudo._mod.cmd_wait("r_1")


class TestCmdJobsRun:
    """``cmd_jobs_run`` runs prep jobs."""

    def test_reports_ok_jobs(self, pseudo, capsys):
        pseudo.post_results["/jobs/invest/run"] = {
            "jobs": [
                {"name": "fetch", "status": "ok", "duration": 2.0},
            ],
        }
        pseudo._mod.cmd_jobs_run("invest", None)
        out = capsys.readouterr().out
        assert "fetch" in out
        assert "ok" in out

    def test_failed_jobs_exit_nonzero(self, pseudo):
        pseudo.post_results["/jobs/invest/run"] = {
            "jobs": [
                {"name": "bad", "status": "failed", "duration": 0.5, "error": "boom"},
            ],
        }
        with pytest.raises(SystemExit):
            pseudo._mod.cmd_jobs_run("invest", None)

    def test_filters_by_job_name(self, pseudo):
        pseudo.post_results["/jobs/invest/run"] = {"jobs": []}
        pseudo._mod.cmd_jobs_run("invest", "fetch")
        _, body = pseudo.post_calls[0]
        assert body["job"] == "fetch"


class TestCmdJobsStatus:
    """``cmd_jobs_status`` shows declared prep jobs."""

    def test_lists_jobs(self, pseudo, capsys):
        pseudo.get_results["/jobs/invest/status"] = {
            "jobs": [
                {"name": "fetch", "script": "fetch.py", "timeout": 60,
                 "description": "fetch market data"},
            ],
        }
        pseudo._mod.cmd_jobs_status("invest")
        out = capsys.readouterr().out
        assert "fetch" in out
        assert "fetch.py" in out

    def test_no_jobs_message(self, pseudo, capsys):
        pseudo.get_results["/jobs/invest/status"] = {"message": "no jobs declared"}
        pseudo._mod.cmd_jobs_status("invest")
        assert "no jobs declared" in capsys.readouterr().out


# ---------------------------------------------------------------------------
# main() — argument parsing + dispatcher
# ---------------------------------------------------------------------------

class TestMainDispatch:
    """``main()`` routes each subcommand to the right handler."""

    def test_acp(self, cli, monkeypatch):
        seen = []
        monkeypatch.setattr("pux_harness.acp.run_acp",
                            lambda org: seen.append(org))
        monkeypatch.setattr(sys, "argv", ["pux", "acp", "--org", "invest"])
        cli.main()
        assert seen == ["invest"]

    def test_mcp(self, cli, monkeypatch):
        # `pux mcp` is the documented FastMCP SSE server (README/AUDIT
        # + mcp_server.py docstring). It MUST be a wired subcommand — previously
        # the CLI rejected it ("invalid choice") while every doc referenced it.
        seen = []
        monkeypatch.setattr("pux_harness.mcp_server.main", lambda: seen.append("mcp"))
        monkeypatch.setattr(sys, "argv", ["pux", "mcp"])
        cli.main()
        assert seen == ["mcp"]

    def test_tui(self, cli, monkeypatch):
        seen = []
        monkeypatch.setattr("pux_harness.tui.run_tui",
                            lambda *a: seen.extend(a))
        monkeypatch.setattr(sys, "argv", ["pux", "tui", "--org", "invest"])
        cli.main()
        assert seen[0] == "invest"

    def test_tui_list_orgs(self, cli, monkeypatch):
        seen = []
        monkeypatch.setattr("pux_harness.tui.list_orgs",
                            lambda: seen.append("listed"))
        monkeypatch.setattr(sys, "argv", ["pux", "tui", "--list-orgs"])
        cli.main()
        assert seen == ["listed"]

    def test_direct(self, cli, monkeypatch):
        seen = []
        monkeypatch.setattr("pux_harness.main.run_direct",
                            lambda *a: seen.append(a))
        monkeypatch.setattr(sys, "argv", ["pux", "direct", "--org", "invest",
                                          "--task", "hello"])
        cli.main()
        args = seen[0]
        assert args[0] == "invest"
        assert args[1] == "hello"

    def test_sandbox(self, cli, monkeypatch):
        seen = []
        monkeypatch.setattr("pux_harness.main.run_sandbox",
                            lambda a: seen.append(a))
        monkeypatch.setattr(sys, "argv", ["pux", "sandbox", "start"])
        cli.main()
        assert seen == ["start"]

    def test_agents(self, cli, monkeypatch):
        seen = []
        monkeypatch.setattr(cli, "cmd_agents", lambda: seen.append("agents"))
        monkeypatch.setattr(sys, "argv", ["pux", "agents"])
        cli.main()
        assert seen == ["agents"]

    def test_dispatch(self, cli, monkeypatch):
        seen = []
        monkeypatch.setattr(cli, "cmd_dispatch",
                            lambda *a: seen.append(a))
        monkeypatch.setattr(sys, "argv",
                            ["pux", "dispatch", "--org", "general", "task text"])
        cli.main()
        args = seen[0]
        assert args[0] == "general"  # org
        assert args[1] == "task text"  # task

    def test_resume(self, cli, monkeypatch):
        seen = []
        monkeypatch.setattr(cli, "cmd_resume",
                            lambda org: seen.append(org))
        monkeypatch.setattr(sys, "argv", ["pux", "resume"])
        cli.main()
        assert seen == [None]

    def test_show(self, cli, monkeypatch):
        seen = []
        monkeypatch.setattr(cli, "cmd_show",
                            lambda tid: seen.append(tid))
        monkeypatch.setattr(sys, "argv", ["pux", "show", "th_1"])
        cli.main()
        assert seen == ["th_1"]

    def test_history(self, cli, monkeypatch):
        seen = []
        monkeypatch.setattr(cli, "cmd_history",
                            lambda tid: seen.append(tid))
        monkeypatch.setattr(sys, "argv", ["pux", "history", "th_1"])
        cli.main()
        assert seen == ["th_1"]

    def test_run(self, cli, monkeypatch):
        seen = []
        monkeypatch.setattr(cli, "cmd_run",
                            lambda *a: seen.append(a))
        monkeypatch.setattr(sys, "argv",
                            ["pux", "run", "--recursion-limit", "10", "th_1", "go"])
        cli.main()
        args = seen[0]
        assert args[0] == "th_1"
        assert args[1] == "go"
        assert args[2] == 10

    def test_wait(self, cli, monkeypatch):
        seen = []
        monkeypatch.setattr(cli, "cmd_wait",
                            lambda rid: seen.append(rid))
        monkeypatch.setattr(sys, "argv", ["pux", "wait", "run_1"])
        cli.main()
        assert seen == ["run_1"]

    def test_list(self, cli, monkeypatch):
        seen = []
        monkeypatch.setattr("pux_harness.main.run_list_orgs",
                            lambda: seen.append("listed"))
        monkeypatch.setattr(sys, "argv", ["pux", "list"])
        cli.main()
        assert seen == ["listed"]

    def test_check(self, cli, monkeypatch):
        seen = []
        monkeypatch.setattr("pux_harness.main.run_check_smoke",
                            lambda org: seen.append(org))
        monkeypatch.setattr(sys, "argv", ["pux", "check", "--org", "invest"])
        cli.main()
        assert seen == ["invest"]

    def test_check_contract(self, cli, monkeypatch):
        seen = []
        monkeypatch.setattr("pux_harness.main.run_check_contract",
                            lambda: seen.append("checked"))
        monkeypatch.setattr(sys, "argv", ["pux", "check-contract"])
        cli.main()
        assert seen == ["checked"]

    def test_check_policy(self, cli, monkeypatch):
        seen = []
        monkeypatch.setattr("pux_harness.main.run_check_policy",
                            lambda org: seen.append(org))
        monkeypatch.setattr(sys, "argv", ["pux", "check-policy", "--org", "invest"])
        cli.main()
        assert seen == ["invest"]

    def test_jobs_run(self, cli, monkeypatch):
        seen = []
        monkeypatch.setattr(cli, "cmd_jobs_run",
                            lambda *a: seen.append(a))
        monkeypatch.setattr(sys, "argv",
                            ["pux", "jobs", "run", "--org", "invest", "--job", "fetch"])
        cli.main()
        args = seen[0]
        assert args[0] == "invest"
        assert args[1] == "fetch"

    def test_jobs_status(self, cli, monkeypatch):
        seen = []
        monkeypatch.setattr(cli, "cmd_jobs_status",
                            lambda org: seen.append(org))
        monkeypatch.setattr(sys, "argv", ["pux", "jobs", "status", "--org", "invest"])
        cli.main()
        assert seen == ["invest"]

    def test_export_is_deprecated_hard_error(self, cli, monkeypatch, tmp_path, capsys):
        # `pux export` is GONE (P3 manifest rework) → a HARD ERROR, no alias. The
        # verb stays registered as a parser so the migration message is clear
        # (not an opaque argparse "invalid choice"). Decision 5: there is NO
        # silent fallback to `pack` — invoking export always fails deliberately,
        # forcing scripts/muscle memory off the un-validated path.
        monkeypatch.setattr(sys, "argv", ["pux", "export", "--org", "general",
                                          "-o", str(tmp_path / "general.tar.gz")])
        with pytest.raises(SystemExit) as exc:
            cli.main()
        assert exc.value.code != 0
        err = capsys.readouterr().err
        assert "pux pack" in err, f"deprecation must point at `pack`: {err}"

    def test_pack(self, cli, monkeypatch, tmp_path):
        output_path = tmp_path / "general.tar.gz"
        # Create a valid empty tar.gz so the open/read summary path in main() works.
        import tarfile
        with tarfile.open(output_path, "w:gz") as tar:
            pass
        monkeypatch.setattr("pux_harness.pack.pack_org",
                            lambda org, output=None, **kw: output or output_path)
        monkeypatch.setattr(sys, "argv", ["pux", "pack", "--org", "general",
                                          "-o", str(output_path)])
        cli.main()

    def test_unknown_command_exits(self, cli, monkeypatch):
        monkeypatch.setattr(sys, "argv", ["pux", "nonexistent"])
        with pytest.raises(SystemExit):
            cli.main()
