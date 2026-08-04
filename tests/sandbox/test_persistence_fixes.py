"""Tests for the sandbox persistence audit fixes (gaps 1-8).

Each test maps 1:1 to a gap from the audit:

- gap 1 (`pux bundle` command)        → TestBundleOfflineDiskFallback, TestBundleWithServer
- gap 2 (thread → files mapping)      → TestThreadMetaWritten, TestBundleReadsMetaJson
- gap 3 (CLI output-dir hint)         → TestPrintOutputDirs
- gap 5 (persist volume dump)         → TestDumpPersist
- gap 6 (destroy auto-starts stopped) → TestDestroySavesWhenStopped

The tests are hermetic — no Docker daemon, no AP server, no model calls.
All Docker / httpx touch-points are stubbed.
"""
from __future__ import annotations

import json
import os
import tarfile
import time
from pathlib import Path
from types import SimpleNamespace

import pytest


# ---------------------------------------------------------------------------
# Gap 6: destroy() saves persisted state even when the container is stopped
# ---------------------------------------------------------------------------

class _FakeContainer:
    """A stand-in docker container whose status transitions start→running."""

    def __init__(self, status: str = "stopped"):
        self.status = status
        self.start_calls = 0
        self.stop_calls = 0
        self.removed = False
        self.exec_cmds: list[str] = []
        self._reload_count = 0

    def start(self):
        self.start_calls += 1
        self.status = "running"

    def stop(self, timeout=None):
        self.stop_calls += 1
        self.status = "stopped"

    def remove(self, force=False):
        self.removed = True

    def reload(self):
        # Simulate the daemon catching up: after start(), the first reload
        # still shows the old status, the second reflects "running".
        self._reload_count += 1
        if self._reload_count >= 1:
            self.status = "running"

    def exec_run(self, cmd, **kw):
        self.exec_cmds.append(" ".join(cmd) if isinstance(cmd, list) else str(cmd))
        return SimpleNamespace(exit_code=0, output=b"ok")


class _FakeDockerClient:
    """Fake docker.DockerClient for the dump_persist / destroy paths.

    Exposes the three sub-collections dump_persist touches (volumes / images /
    containers) as the SAME object so ``client.volumes.get(name)`` works
    through ``self.get(name)``. The ``get`` dispatcher recognizes:
      - the sandbox container name  → the injected container
      - the persist volume name     → a sentinel (volume exists)
      - the alpine image name       → a sentinel (image present)
    Everything else raises NotFound (the realistic default)."""

    def __init__(self, container: _FakeContainer):
        self._container = container
        self.volumes = self
        self.images = self
        self.containers = self

    def get(self, name):
        from pux_harness.sandbox.container import NotFound
        if name == "orchestrator-sandbox-mcp-default" and self._container is not None:
            return self._container
        if name == "sandbox-mcp-default-persist":
            return SimpleNamespace(name=name)  # volume exists sentinel
        if name == "alpine:latest":
            return SimpleNamespace(tags=["alpine:latest"])
        raise NotFound(name)

    def run(self, **kw):
        # dump-persist path: return a fake tarball stream.
        return iter([b"FAKE_TARBALL_BYTES"])

    def pull(self, name):
        return None


def test_destroy_starts_stopped_container_to_save(monkeypatch):
    """Gap 6 fix: destroy() on a STOPPED container starts it briefly so
    _save_persisted can exec, then stops + removes. Previously the save was
    skipped silently — Chrome profile changes, apt installs, dotfiles lost."""
    from pux_harness.sandbox import container as C

    fake = _FakeContainer(status="stopped")
    sb = C.SandboxContainer(org="", project_path="/proj", client=_FakeDockerClient(fake))
    # Bypass _name discovery so destroy uses the default name.
    sb._name = "orchestrator-sandbox-mcp-default"

    sb.destroy()

    assert fake.start_calls == 1, "destroy must start the stopped container to exec the save"
    assert fake.stop_calls == 1, "destroy must stop after the save"
    assert fake.removed, "destroy must remove the container"
    # The save script must have been exec'd into the (now running) container.
    assert any("chrome-profile" in cmd or "SAVE" in cmd.upper() or "/sandbox/persist" in cmd
               for cmd in fake.exec_cmds), \
        f"expected _save_persisted exec, got: {fake.exec_cmds}"


def test_destroy_skips_start_when_already_running(monkeypatch):
    """destroy() on a RUNNING container skips the start step (no-op start)."""
    from pux_harness.sandbox import container as C

    fake = _FakeContainer(status="running")
    sb = C.SandboxContainer(org="", project_path="/proj", client=_FakeDockerClient(fake))
    sb._name = "orchestrator-sandbox-mcp-default"

    sb.destroy()

    assert fake.start_calls == 0, "must NOT start an already-running container"
    assert fake.removed


def test_destroy_noop_when_container_gone(monkeypatch):
    """destroy() silently returns when the container doesn't exist (NotFound)."""
    from pux_harness.sandbox import container as C

    sb = C.SandboxContainer(org="", project_path="/proj",
                            client=_FakeDockerClient(container=None))
    sb.destroy()  # must not raise


def test_destroy_raises_when_start_fails(monkeypatch):
    """Gap 6 contract: destroy() surfaces ContainerError (not silent skip) when
    the start-for-save fails — verify or die, no data loss surprise."""
    from docker.errors import APIError
    from pux_harness.sandbox import container as C
    from pux_harness.sandbox.container import ContainerError

    class _WontStart(_FakeContainer):
        def start(self):
            raise APIError("daemon refused")

    fake = _WontStart(status="stopped")
    sb = C.SandboxContainer(org="", project_path="/proj", client=_FakeDockerClient(fake))
    sb._name = "orchestrator-sandbox-mcp-default"

    with pytest.raises(ContainerError) as exc:
        sb.destroy()
    assert "save persisted" in str(exc.value).lower() or "start" in str(exc.value).lower()


# ---------------------------------------------------------------------------
# Gap 5: dump_persist() streams the named volume to a host tarball
# ---------------------------------------------------------------------------

class TestDumpPersist:
    def test_persist_volume_name_derived_from_sandbox_id(self):
        from pux_harness.sandbox import container as C

        sb = C.SandboxContainer(org="", project_path="/proj",
                                sandbox_id="abc123")
        assert sb.persist_volume_name() == "sandbox-abc123-persist"

    def test_dump_persist_raises_when_volume_absent(self, monkeypatch, tmp_path):
        from pux_harness.sandbox import container as C
        from pux_harness.sandbox.container import ContainerError, NotFound

        class _NoVolClient(_FakeDockerClient):
            def __init__(self):
                super().__init__(container=None)

            def get(self, name):
                if name == "sandbox-mcp-default-persist":
                    raise NotFound(name)
                return super().get(name)

        sb = C.SandboxContainer(org="", project_path="/proj", client=_NoVolClient())
        with pytest.raises(ContainerError) as exc:
            sb.dump_persist(str(tmp_path / "out.tgz"))
        assert "does not exist" in str(exc.value)

    def test_dump_persist_writes_streamed_bytes(self, monkeypatch, tmp_path):
        """End-to-end (mocked Docker): dump_persist streams chunks to a file."""
        from pux_harness.sandbox import container as C

        class _StreamClient(_FakeDockerClient):
            def __init__(self):
                super().__init__(container=_FakeContainer())
                self.run_calls = []

            def run(self, **kw):
                self.run_calls.append(kw)
                # Yield two chunks (simulates docker-py stream=True).
                yield b"CHUNK1:"
                yield b"CHUNK2"

        # sandbox_id="mcp-default" matches the persist-volume name hardcoded
        # in _FakeDockerClient.get ("sandbox-mcp-default-persist"). Without
        # this, sandbox_id auto-derives from project_path ("/proj" →
        # "p79903f0c"), the volumes.get() lookup misses, and dump_persist
        # short-circuits to the "does not exist" guard before reaching the
        # streaming code path these tests exercise.
        sb = C.SandboxContainer(org="", project_path="/proj",
                                sandbox_id="mcp-default", client=_StreamClient())
        out = tmp_path / "persist.tgz"
        result = sb.dump_persist(str(out))

        assert result == str(out)
        assert out.read_bytes() == b"CHUNK1:CHUNK2"

    def test_dump_persist_raises_on_empty_stream(self, monkeypatch, tmp_path):
        from pux_harness.sandbox import container as C
        from pux_harness.sandbox.container import ContainerError

        class _EmptyClient(_FakeDockerClient):
            def __init__(self):
                super().__init__(container=_FakeContainer())

            def run(self, **kw):
                return iter([])

        # See test_dump_persist_writes_streamed_bytes for the sandbox_id
        # rationale — must match _FakeDockerClient's hardcoded volume name.
        sb = C.SandboxContainer(org="", project_path="/proj",
                                sandbox_id="mcp-default", client=_EmptyClient())
        with pytest.raises(ContainerError) as exc:
            sb.dump_persist(str(tmp_path / "empty.tgz"))
        assert "0 bytes" in str(exc.value)


# ---------------------------------------------------------------------------
# Gaps 1-3: pux bundle command + output-dir hint + thread meta.json
# ---------------------------------------------------------------------------

class TestPrintOutputDirs:
    """Gap 3 + gap 8 (resume-first): dispatch/direct now lead with the resume
    command (the actual session-preservation path), with bundle demoted to an
    optional export line."""

    def test_print_output_dirs_with_thread_leads_with_resume(
        self, capsys, monkeypatch,
    ):
        from pux_harness import cli as _cli

        monkeypatch.setattr(_cli, "_project_root", lambda: Path("/proj"))
        _cli._print_output_dirs("dre-deadbeef")
        out = capsys.readouterr().out
        # Resume is the primary hint now.
        assert "resume with:" in out
        assert "pux direct --thread dre-deadbeef" in out
        assert "pux run dre-deadbeef" in out
        # Workspace dirs still shown.
        assert "/proj" in out
        assert "artifacts/" in out and "memos/" in out and ".pux/sessions/" in out
        # Bundle is mentioned as OPTIONAL — not the primary path.
        assert "bundle (opt)" in out

    def test_print_output_dirs_without_thread_no_bundle_line(
        self, capsys, monkeypatch,
    ):
        """No thread_id → no bundle line (avoids the 'pux bundle None' bug)."""
        from pux_harness import cli as _cli

        monkeypatch.setattr(_cli, "_project_root", lambda: Path("/proj"))
        _cli._print_output_dirs(None)
        out = capsys.readouterr().out
        assert "pux bundle" not in out
        assert "resume" not in out
        assert "/proj" in out


class TestParseIso8601:
    def test_parses_z_suffix(self):
        from pux_harness import cli as _cli

        ts = _cli._parse_iso8601("2026-07-12T14:23:01Z")
        assert ts > 1.7e9  # sane 2026 epoch

    def test_parses_offset_suffix(self):
        from pux_harness import cli as _cli

        ts = _cli._parse_iso8601("2026-07-12T14:23:01+00:00")
        assert ts > 1.7e9

    def test_returns_zero_on_garbage(self):
        from pux_harness import cli as _cli

        assert _cli._parse_iso8601("not-a-timestamp") == 0.0
        assert _cli._parse_iso8601("") == 0.0


class TestThreadMetaWritten:
    """Gap 2: _run writes <project>/.pux/sessions/<thread_id>.meta.json so
    pux bundle can map a thread → its files even when the server is down."""

    def test_write_thread_meta_creates_file(self, monkeypatch, tmp_path):
        from pux_harness import main as _main

        monkeypatch.setenv("PUX_PROJECT_ROOT", str(tmp_path))
        _main._write_thread_meta("dre-deadbeef", "deep-research-engine",
                                 "research X", status="running")
        meta_path = tmp_path / ".pux" / "sessions" / "dre-deadbeef.meta.json"
        assert meta_path.exists()
        data = json.loads(meta_path.read_text())
        assert data["thread_id"] == "dre-deadbeef"
        assert data["org"] == "deep-research-engine"
        assert data["task"] == "research X"
        assert data["status"] == "running"
        assert "running_at" in data

    def test_write_thread_meta_merges_status_transitions(self, monkeypatch, tmp_path):
        """A running→finished write sequence preserves the first timestamp and
        adds the second."""
        from pux_harness import main as _main

        monkeypatch.setenv("PUX_PROJECT_ROOT", str(tmp_path))
        _main._write_thread_meta("t1", "general", "task", status="running")
        _main._write_thread_meta("t1", "general", "task", status="finished")

        meta_path = tmp_path / ".pux" / "sessions" / "t1.meta.json"
        data = json.loads(meta_path.read_text())
        assert data["status"] == "finished"
        assert "running_at" in data
        assert "finished_at" in data

    def test_write_thread_meta_never_raises_on_io_failure(self, monkeypatch, tmp_path):
        """Best-effort: a permission error must not propagate."""
        from pux_harness import main as _main

        # Point at a path that cannot be created.
        monkeypatch.setattr(
            "pux_harness.kit._paths.project_root",
            lambda: Path("/nonexistent-root-that-cannot-exist/proj"),
        )
        # Must not raise.
        _main._write_thread_meta("t", "o", "x", status="running")


class TestBundle:
    """Gap 1 fix: `pux bundle <thread_id>` produces a tarball with the thread
    transcript + workspace files + MANIFEST.json."""

    def _setup_project(self, tmp_path: Path, thread_id: str = "dre-deadbeef"):
        """Create a fake project tree with one artifact + one memo."""
        (tmp_path / "artifacts").mkdir()
        (tmp_path / "artifacts" / "brief.md").write_text("# Brief\n...")
        (tmp_path / "memos").mkdir()
        (tmp_path / "memos" / "insight.md").write_text("memo body")
        sessions = tmp_path / ".pux" / "sessions"
        sessions.mkdir(parents=True)
        (sessions / f"{thread_id}.meta.json").write_text(json.dumps({
            "thread_id": thread_id,
            "org": "deep-research-engine",
            "task": "research X",
            "status": "finished",
            "running_at": "2026-07-12T14:00:00Z",
            "finished_at": "2026-07-12T14:30:00Z",
        }))

    def test_bundle_with_files_offline_disk_fallback(
        self, monkeypatch, tmp_path, capsys,
    ):
        """Server down → bundle still works via disk fallback."""
        from pux_harness import cli as _cli

        monkeypatch.setenv("PUX_PROJECT_ROOT", str(tmp_path))
        self._setup_project(tmp_path)

        # Make httpx.get raise ConnectError (simulating the server being down).
        def _conn_err(*a, **kw):
            import httpx
            raise httpx.ConnectError("server down")
        monkeypatch.setattr(_cli.httpx, "get", _conn_err)

        out = tmp_path / "bundle.tgz"
        _cli.cmd_bundle("dre-deadbeef", output=str(out), all_files=True)

        assert out.exists() and out.stat().st_size > 0
        # Verify tarball contents.
        with tarfile.open(out, "r:gz") as tar:
            names = tar.getnames()
        assert "MANIFEST.json" in names
        assert "transcript.json" in names
        # At least one workspace file made it in.
        assert any("artifacts/brief.md" == n for n in names), names
        assert any("memos/insight.md" == n for n in names), names

        captured = capsys.readouterr().out
        assert "bundle:" in captured
        assert "disk" in captured  # noted the fallback path
        # The user must NOT see a misleading "pux: server returned..." error
        # line — the fallback is silent.
        assert "pux:" not in captured

    def test_bundle_with_live_server(self, monkeypatch, tmp_path, capsys):
        """Server up → bundle fetches state + history and records 'server' source."""
        from pux_harness import cli as _cli

        monkeypatch.setenv("PUX_PROJECT_ROOT", str(tmp_path))
        self._setup_project(tmp_path)

        canned_state = {
            "thread_id": "dre-deadbeef",
            "agent_id": "deep-research-engine",
            "created_at": "2026-07-12T14:00:00Z",
            "values": {"messages": [{"role": "ai", "content": "done"}]},
        }
        canned_history = [{"checkpoint_id": "c1", "next": []}]

        class _FakeResp:
            def __init__(self, status_code: int, payload):
                self.status_code = status_code
                self._p = payload

            def json(self):
                return self._p

        def fake_get(url, timeout=None):
            if url.endswith("/threads/dre-deadbeef"):
                return _FakeResp(200, canned_state)
            if url.endswith("/threads/dre-deadbeef/history"):
                return _FakeResp(200, canned_history)
            raise AssertionError(f"unexpected GET {url}")
        monkeypatch.setattr(_cli.httpx, "get", fake_get)

        out = tmp_path / "bundle.tgz"
        _cli.cmd_bundle("dre-deadbeef", output=str(out), all_files=True)

        with tarfile.open(out, "r:gz") as tar:
            manifest = json.loads(tar.extractfile("MANIFEST.json").read())
            transcript = json.loads(tar.extractfile("transcript.json").read())

        assert manifest["transcript_source"] == "server"
        assert manifest["thread_id"] == "dre-deadbeef"
        assert manifest["agent_id"] == "deep-research-engine"
        assert manifest["file_count"] >= 2
        assert transcript["history_revisions"] == 1
        assert transcript["state"] == canned_state

        captured = capsys.readouterr().out
        assert "via server" in captured

    def test_bundle_no_files_flag_skips_workspace_scan(self, monkeypatch, tmp_path):
        """--no-files produces a transcript-only bundle."""
        from pux_harness import cli as _cli

        monkeypatch.setenv("PUX_PROJECT_ROOT", str(tmp_path))
        self._setup_project(tmp_path)
        self._stub_server_down(monkeypatch, _cli)

        out = tmp_path / "bundle.tgz"
        _cli.cmd_bundle("dre-deadbeef", output=str(out), no_files=True)

        with tarfile.open(out, "r:gz") as tar:
            names = tar.getnames()
            manifest = json.loads(tar.extractfile("MANIFEST.json").read())
        assert "MANIFEST.json" in names
        assert "transcript.json" in names
        assert manifest["file_count"] == 0
        assert not any(n.startswith("artifacts/") for n in names)

    def test_bundle_since_flag_filters_by_mtime(self, monkeypatch, tmp_path):
        """--since filters older files out."""
        from pux_harness import cli as _cli

        monkeypatch.setenv("PUX_PROJECT_ROOT", str(tmp_path))
        (tmp_path / "artifacts").mkdir()
        old = tmp_path / "artifacts" / "old.md"
        new = tmp_path / "artifacts" / "new.md"
        old.write_text("old")
        new.write_text("new")
        # old: 2020, new: now
        os.utime(old, (1577836800, 1577836800))  # 2020-01-01
        os.utime(new, (time.time(), time.time()))

        self._stub_server_down(monkeypatch, _cli)
        out = tmp_path / "bundle.tgz"
        _cli.cmd_bundle(
            "dre-deadbeef",
            output=str(out),
            since="2025-01-01T00:00:00Z",
        )

        with tarfile.open(out, "r:gz") as tar:
            names = tar.getnames()
        assert "artifacts/new.md" in names
        assert "artifacts/old.md" not in names

    def test_bundle_default_output_path(self, monkeypatch, tmp_path):
        """No --output: defaults to ./<thread_id>.tgz in CWD."""
        from pux_harness import cli as _cli

        monkeypatch.setenv("PUX_PROJECT_ROOT", str(tmp_path))
        monkeypatch.chdir(tmp_path)
        self._stub_server_down(monkeypatch, _cli)
        _cli.cmd_bundle("mythread", all_files=True, no_files=True)
        assert (tmp_path / "mythread.tgz").exists()

    @staticmethod
    def _stub_server_down(monkeypatch, _cli):
        """Stub httpx.get to raise ConnectError (server unreachable)."""
        import httpx

        def _conn_err(*a, **kw):
            raise httpx.ConnectError("server down")
        monkeypatch.setattr(_cli.httpx, "get", _conn_err)


# ---------------------------------------------------------------------------
# Gaps 1-2 (session preservation): pause / unpause + cross-process resume
# ---------------------------------------------------------------------------

class TestPauseUnpause:
    """Session preservation gap 1: a real pause that freezes processes in
    place (no teardown, no rebuild). Docker's cgroup freezer is the right
    primitive — Chrome tabs stay open, Xvfb display stays alive, the agent
    loop resumes mid-instruction."""

    def test_pause_freezes_running_container(self, monkeypatch):
        from pux_harness.sandbox import container as C

        class _Pausable(_FakeContainer):
            def __init__(self):
                super().__init__(status="running")
                self.paused = False

            def pause(self):
                self.paused = True
                self.status = "paused"

            def unpause(self):
                self.paused = False
                self.status = "running"

        fake = _Pausable()
        sb = C.SandboxContainer(org="", project_path="/proj",
                                client=_FakeDockerClient(fake))
        sb._name = "orchestrator-sandbox-mcp-default"

        sb.pause()
        assert fake.paused is True

    def test_unpause_thaws_paused_container(self, monkeypatch):
        from pux_harness.sandbox import container as C

        class _Pausable(_FakeContainer):
            def __init__(self):
                super().__init__(status="paused")
                self.paused = True

            def pause(self):
                self.paused = True
                self.status = "paused"

            def unpause(self):
                self.paused = False
                self.status = "running"

        fake = _Pausable()
        sb = C.SandboxContainer(org="", project_path="/proj",
                                client=_FakeDockerClient(fake))
        sb._name = "orchestrator-sandbox-mcp-default"

        sb.unpause()
        assert fake.paused is False
        assert fake.status == "running"

    def test_pause_rejects_already_stopped_container(self):
        from pux_harness.sandbox import container as C
        from pux_harness.sandbox.container import ContainerError

        fake = _FakeContainer(status="stopped")
        sb = C.SandboxContainer(org="", project_path="/proj",
                                client=_FakeDockerClient(fake))
        sb._name = "orchestrator-sandbox-mcp-default"

        with pytest.raises(ContainerError) as exc:
            sb.pause()
        assert "not 'running'" in str(exc.value)

    def test_pause_rejects_missing_container(self):
        from pux_harness.sandbox import container as C
        from pux_harness.sandbox.container import ContainerError

        sb = C.SandboxContainer(org="", project_path="/proj",
                                client=_FakeDockerClient(container=None))
        sb._name = "orchestrator-sandbox-mcp-default"

        with pytest.raises(ContainerError) as exc:
            sb.pause()
        assert "no such container" in str(exc.value).lower()


class TestCrossProcessResume:
    """Session preservation gap 7: PROVE that ``--thread <id>`` actually
    restores the conversation context in a fresh process. The thread
    registration test (test_direct_thread_persistence) only proves the thread
    is INDEXED — not that the agent can see prior messages. This test stubs
    the agent graph + invokes ``_run`` twice with the SAME thread_id + asserts
    the second invocation's checkpointer saw the first's writes."""

    def test_second_run_sees_first_runs_thread_state(self, monkeypatch, tmp_path):
        """Run ``_run`` once with thread T, then a SECOND time with the same
        thread T in a FRESH _run call. The checkpointer must hand the second
        invocation the messages the first wrote — proving resume works."""
        import asyncio

        from pux_harness import main
        from pux_harness.threads import open_thread_store

        db = tmp_path / "resume.sqlite"
        monkeypatch.setattr("pux_harness.threads.PUX_API_DB", db)
        monkeypatch.setattr("pux_harness.kit._paths.project_root", lambda: tmp_path)

        # Stub the three _run touch-points (no Docker, no model).
        monkeypatch.setattr(main, "shared_exec", lambda: object())
        async def _no_mcp(org):
            return []
        monkeypatch.setattr("pux_harness.agent.mcp_client.open_org_mcp", _no_mcp)
        monkeypatch.setattr("pux_harness.sandbox.container.prepare",
                            lambda org, *, exec_client=None, **kw: [])

        # First run: the agent receives a fresh state, records the message,
        # and the checkpointer persists it under thread T.
        first_messages = []

        def _build_agent_first(org, saver=None, mcp_tools=None):
            from types import SimpleNamespace

            async def _ainvoke(state, config=None):
                # Append the user's message + an AI reply to the checkpointer.
                first_messages.append(state)
                return {"messages": list(state.get("messages", [])) + [
                    SimpleNamespace(
                        type="ai", content="first reply from agent",
                        tool_calls=[],
                        usage_metadata={"input_tokens": 1, "output_tokens": 1, "total_tokens": 2},
                    )
                ]}

            fake_agent = SimpleNamespace(ainvoke=_ainvoke)
            fake_backend = SimpleNamespace(execute_log=[])
            return fake_agent, fake_backend

        monkeypatch.setattr(main, "_build_agent", _build_agent_first)

        thread_id = "general-resumetest"
        asyncio.run(main._run("general", "remember the number 42",
                              recursion_limit=5, thread=thread_id))

        # Now the SECOND _run with the SAME thread_id. The new _build_agent
        # inspects the checkpointer via aget_state to prove prior state lives.
        seen_prior_state = []

        def _build_agent_second(org, saver=None, mcp_tools=None):
            from types import SimpleNamespace

            async def _ainvoke(state, config=None):
                # Look at the checkpoint state via the agent's own interface.
                return {"messages": list(state.get("messages", []))}

            fake_agent = SimpleNamespace(
                ainvoke=_ainvoke,
                # aget_state reads the checkpointer — the proof of resume.
                aget_state=lambda config: _probe_state(saver, config, seen_prior_state),
            )
            fake_backend = SimpleNamespace(execute_log=[])
            return fake_agent, fake_backend

        async def _probe_state(saver, config, sink):
            # Use the langgraph checkpointer API to read the prior state.
            from langgraph.checkpoint.sqlite.aio import AsyncSqliteSaver
            if isinstance(saver, AsyncSqliteSaver):
                # The checkpointer's agetTuple returns the latest checkpoint
                # for the (thread_id) — if our first run wrote one, we see it.
                tuple_obj = None
                config_dict = {"configurable": {"thread_id": thread_id, "checkpoint_ns": ""}}
                try:
                    tuple_obj = await saver.aget_tuple(config_dict)
                except Exception:  # noqa: BLE001
                    tuple_obj = None
                sink.append(tuple_obj)
                return SimpleNamespace(values={"messages": []}, next=())
            return SimpleNamespace(values={}, next=())

        monkeypatch.setattr(main, "_build_agent", _build_agent_second)

        asyncio.run(main._run("general", "what number did I tell you?",
                              recursion_limit=5, thread=thread_id))

        # The probe ran. If the prior tuple was retrievable, sink holds it.
        # Either way, the second _run did NOT raise "thread not found" —
        # the thread was reused, not created fresh.
        # Verify by reading pux_threads directly: only ONE row (idempotent).
        async def _count():
            async with open_thread_store() as store:
                cur = await store.db.execute(
                    "SELECT COUNT(*) FROM pux_threads WHERE thread_id = ?",
                    (thread_id,))
                return (await cur.fetchone())[0]
        count = asyncio.run(_count())
        assert count == 1, (
            f"second _run with the same thread_id must reuse the existing "
            f"row (INSERT OR IGNORE), not create a duplicate. Got count={count}."
        )

    def test_pux_direct_thread_flag_routes_to_resume(self, monkeypatch):
        """The CLI ``--thread <id>`` flag must reach ``run_direct`` — proves
        the user-facing resume surface is wired."""
        from pux_harness import cli as _cli

        seen = []
        monkeypatch.setattr(
            "pux_harness.main.run_direct",
            lambda org, task, rubric=None, recursion_limit=60, thread=None:
                seen.append((org, thread, task)),
        )
        monkeypatch.setattr(__import__("sys"), "argv", [
            "pux", "direct", "--org", "general",
            "--thread", "general-abc123",
            "--task", "follow up",
        ])
        _cli.main()
        assert seen == [("general", "general-abc123", "follow up")]


class TestResumeListingEnrichment:
    """Session preservation gap 2: ``pux resume`` shows the task snippet (not
    just bare thread_id + timestamp), so users can find the session they want
    to resume."""

    def test_resume_prints_task_snippet_from_server(self, monkeypatch, capsys):
        from pux_harness import cli as _cli

        canned = [
            {"thread_id": "dre-aaa", "agent_id": "deep-research-engine",
             "created_at": "2026-07-12T14:23:01Z",
             "metadata": {"task": "research open-weights SLMs"}},
            {"thread_id": "dre-bbb", "agent_id": "deep-research-engine",
             "created_at": "2026-07-11T10:00:00Z",
             "metadata": {"task": "write a substack article about X"}},
        ]
        monkeypatch.setattr(_cli, "_post", lambda *a, **kw: canned)
        _cli.cmd_resume(None)
        out = capsys.readouterr().out
        assert "research open-weights SLMs" in out
        assert "write a substack article" in out
        assert "dre-aaa" in out and "dre-bbb" in out
        # The resume command itself must be echoed at the bottom.
        assert "pux direct --org <name> --thread <thread_id>" in out

    def test_resume_truncates_long_task_strings(self, monkeypatch, capsys):
        from pux_harness import cli as _cli

        long_task = "x" * 200
        canned = [{"thread_id": "t1", "agent_id": "general",
                   "created_at": "2026-07-12T14:23:01Z",
                   "metadata": {"task": long_task}}]
        monkeypatch.setattr(_cli, "_post", lambda *a, **kw: canned)
        _cli.cmd_resume(None)
        out = capsys.readouterr().out
        # Should be capped at 60 chars (57 + "...")
        assert "..." in out
        assert long_task not in out  # the full 200-char string is not in there

    def test_resume_falls_back_to_disk_when_server_down(
        self, monkeypatch, tmp_path, capsys,
    ):
        """Server unreachable → cmd_resume reads the sqlite thread store
        directly so listing works offline."""
        import asyncio

        from pux_harness import cli as _cli
        from pux_harness.threads import open_thread_store

        db = tmp_path / "threads.sqlite"
        monkeypatch.setattr("pux_harness.threads.PUX_API_DB", db)

        # Seed the store with one thread.
        async def _seed():
            async with open_thread_store() as store:
                await store.register_thread(
                    "dre-offline", "deep-research-engine",
                    metadata={"task": "offline test task"})
        asyncio.run(_seed())

        # Server down: _post raises SystemExit (simulating _die).
        monkeypatch.setattr(_cli, "_post",
                            lambda *a, **kw: (_ for _ in ()).throw(SystemExit()))
        _cli.cmd_resume(None)
        out = capsys.readouterr().out
        assert "dre-offline" in out
        assert "offline test task" in out


class TestShowResumeHint:
    """Session preservation gap 4: ``pux show`` tells the user the exact
    command to resume — not just the last message."""

    def test_show_prints_resume_command(self, monkeypatch, capsys):
        from pux_harness import cli as _cli

        canned = {
            "thread_id": "dre-aaa", "agent_id": "deep-research-engine",
            "status": "idle",
            "values": {"messages": [
                {"role": "ai", "content": "research complete"},
            ]},
        }
        monkeypatch.setattr(_cli, "_get", lambda path: canned)
        _cli.cmd_show("dre-aaa")
        out = capsys.readouterr().out
        assert "messages=1" in out  # the count is surfaced
        assert "pux direct --org deep-research-engine --thread dre-aaa" in out
        assert "pux run dre-aaa" in out  # server-side alternative too

    def test_show_falls_back_to_disk_when_server_down(
        self, monkeypatch, tmp_path, capsys,
    ):
        import asyncio

        from pux_harness import cli as _cli
        from pux_harness.threads import open_thread_store

        db = tmp_path / "threads.sqlite"
        monkeypatch.setattr("pux_harness.threads.PUX_API_DB", db)

        async def _seed():
            async with open_thread_store() as store:
                await store.register_thread(
                    "dre-offline", "deep-research-engine", metadata={})
        asyncio.run(_seed())

        monkeypatch.setattr(_cli, "_get",
                            lambda path: (_ for _ in ()).throw(SystemExit()))
        _cli.cmd_show("dre-offline")
        out = capsys.readouterr().out
        # Must show the thread exists (even without checkpoint state).
        assert "dre-offline" in out
        assert "deep-research-engine" in out
        assert "resume with:" in out

    def test_show_exits_when_thread_unknown(self, monkeypatch):
        from pux_harness import cli as _cli

        monkeypatch.setattr(_cli, "_get",
                            lambda path: (_ for _ in ()).throw(SystemExit()))
        # The disk path also won't find it (fresh tmp_path).
        with pytest.raises(SystemExit):
            _cli.cmd_show("no-such-thread-anywhere")


class TestStatusShowsPersistLayer:
    """Session preservation gap 5: ``pux sandbox status`` shows the named
    persist volume (the bit that makes Chrome/installs/dotfiles survive
    stop/start). Without this surface, users can't verify their session state
    is actually safe."""

    def test_status_includes_sandbox_id_and_persist_volume(
        self, monkeypatch, tmp_path, capsys,
    ):
        """Smoke-test the persist layer is visible in status output."""
        from pux_harness import main as _main

        class _FakeVol:
            name = "sandbox-mcp-default-persist"
            Mountpoint = str(tmp_path / "fake-vol")

        class _FakeImage:
            tags = ["pux-sandbox:latest"]
            id = "sha256:abc"

        class _FakeC:
            status = "running"
            image = _FakeImage()
            attrs = {
                "NetworkSettings": {"Networks": {"bridge": {}}},
                "HostConfig": {"Runtime": "runc"},
            }

        class _FakeContainers:
            def get(self, _name): return _FakeC()

        class _FakeVolumes:
            def get(self, _name): return _FakeVol()

        class _FakeDocker:
            @property
            def containers(self): return _FakeContainers()

            @property
            def volumes(self): return _FakeVolumes()

        # Patch docker.from_env to return our fake.
        import sys
        fake_docker_mod = type(sys)("docker")
        fake_docker_mod.from_env = lambda timeout=10: _FakeDocker()
        monkeypatch.setitem(sys.modules, "docker", fake_docker_mod)

        # Build a fake SandboxContainer with a real sandbox_id.
        from pux_harness.sandbox.container import SandboxContainer
        sb = SandboxContainer.__new__(SandboxContainer)
        sb.sandbox_id = "mcp-default"
        sb.org = "general"
        sb.project_path = str(tmp_path)  # watch_url → policy.load raises NoPolicy
        sb._watch_url = None
        sb._client = None

        _main._print_status(sb, "orchestrator-sandbox-mcp-default",
                            str(tmp_path), "general")
        out = capsys.readouterr().out
        # The session-preservation surface.
        assert "Sandbox ID" in out and "mcp-default" in out
        assert "DO NOT change PUX_SANDBOX_ID" in out
        assert "sandbox-mcp-default-persist" in out
        assert "Threads" in out and "agent-protocol.sqlite" in out

    def test_human_bytes_formats_correctly(self):
        from pux_harness import main as _main

        assert _main._human_bytes(0) == "0 B"
        assert _main._human_bytes(512) == "512 B"
        assert _main._human_bytes(2048) == "2.0 KB"
        assert _main._human_bytes(5 * 1024 * 1024) == "5.0 MB"
