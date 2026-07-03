"""Direct Docker exec — the Python-native sandbox execution path (Phase 8a).

Replaces the ``execute() → MCP bash → Go → moby Exec`` chain with a single
``docker exec`` over the Docker SDK. The harness now reaches the pux-sandbox
container the same way the Go binary did (moby ``ExecCreate``/``ExecAttach``),
just from Python — no JSON-RPC hop, no Go middleman.

The container is discovered by its ``openshell.project-path`` label, decoupled
from the Go binary's ``orchestrator-sandbox-<id>`` naming convention. The Go
MCP bridge still owns the container *lifecycle* (create/start/stop) until
Phase 8g ports it here; this module only *execs into* an already-running
container, so today the Go ``task start`` boots it and the harness execs it.

Why ``tty=False``: the Go binary reads the Docker attach stream raw
(``io.Copy``) so it needs ``TTY=true`` to dodge Docker's multiplexed 8-byte
frame headers. The Python SDK's ``exec_run`` parses that framing for us, so
``tty=False`` gives clean combined stdout+stderr bytes with no terminal ``\\r``
translation — behaviorally equivalent for our text + base64 payloads, and
cleaner output for the inherited ``_build_*_cmd`` scripts. Proven against the
live container (8a probe): ``exec_run(['bash','-c',...], tty=False)`` returns
``ExecResult(exit_code=int, output=bytes)``.

Timeout: ``docker.from_env(timeout=300)`` sets the SDK's HTTP read timeout —
the same 300s ceiling the bridge's ``_TIMEOUT`` enforced. Per-command timeouts
are not yet wired (the Go ``bash`` tool had none either); the ``execute``
signature keeps ``timeout`` for API compatibility.
"""
from __future__ import annotations

import os
from pathlib import Path

import docker
from docker.errors import APIError, NotFound

PROJECT_ROOT = Path(__file__).resolve().parents[2]
PROJECT_LABEL = "openshell.project-path"
_DEFAULT_TIMEOUT = 300  # matches bridge.py _TIMEOUT


def _resolve_project() -> str:
    """Absolute project path whose sandbox label we filter on.

    ``PUX_PROJECT_PATH`` overrides (the Go binary honors the same env var);
    otherwise the repo root. Absolute because Docker bind labels store the
    absolute host path (verified: the live container is labeled with the full
    ``/home/ubuntu/.../auto-developer-orchestrator`` path).
    """
    return os.path.abspath(os.environ.get("PUX_PROJECT_PATH") or str(PROJECT_ROOT))


def _discover(client: docker.DockerClient, project_path: str) -> str:
    """Find the running sandbox container for ``project_path`` by label.

    The Go binary labels every sandbox ``openshell.project-path=<abs>``. There
    must be exactly one running match (single-tenant per project); if several
    exist we take the newest and raise loudly so the operator notices the
    anomaly rather than silently exec-ing the wrong container.
    """
    try:
        containers = client.containers.list(
            filters={"label": f"{PROJECT_LABEL}={project_path}", "status": "running"},
        )
    except APIError as exc:  # docker daemon unreachable / permission
        raise RuntimeError(f"docker list failed: {exc}") from exc
    if not containers:
        raise RuntimeError(
            f"no running pux-sandbox container found for project {project_path!r} "
            f"(label {PROJECT_LABEL}={project_path}). Boot one with `task start`."
        )
    if len(containers) > 1:
        names = sorted(c.name for c in containers)
        raise RuntimeError(
            f"single-tenant invariant violated: {len(containers)} running containers "
            f"match project {project_path!r}: {names}"
        )
    return containers[0].name


class DockerExecClient:
    """Exec-only client over the Docker SDK.

    Caches the discovered container name so the label-filter lookup runs once
    per process — the hot path is ``exec()``, not discovery. Stateless apart
    from that cache + the (long-lived) SDK client.
    """

    def __init__(self, container: str | None = None, *, timeout: int = _DEFAULT_TIMEOUT):
        self._client = docker.from_env(timeout=timeout)
        self._container = container

    @property
    def container(self) -> str:
        if self._container is None:
            self._container = _discover(self._client, _resolve_project())
        return self._container

    def exec(self, command: str, *, timeout: int | None = None) -> tuple[str, int]:
        """Run ``bash -c <command>`` in the sandbox; return (output, exit_code).

        Output is the combined stdout+stderr, utf-8 decoded (errors replaced)
        so binary-ish payloads (base64 from upload/download helpers) survive.
        ``exit_code`` is 0 on success, non-zero on container-side failure — the
        caller decides whether non-zero is an error (the inherited fs scripts
        append ``|| true`` so they report 0; a raw command failing surfaces its
        real exit code).
        """
        try:
            result = self._client.containers.get(self.container).exec_run(
                ["bash", "-c", command],
                tty=False,
                demux=False,  # combined stream; SDK parses Docker framing
                stdin=False,
            )
        except NotFound as exc:
            raise RuntimeError(
                f"sandbox container {self.container!r} vanished mid-run: {exc}"
            ) from exc
        out = result.output
        if isinstance(out, (bytes, bytearray)):
            out = out.decode("utf-8", "replace")
        return out, int(result.exit_code) if result.exit_code is not None else 0


_client: DockerExecClient | None = None


def get_exec_client(container: str | None = None) -> DockerExecClient:
    """Build a fresh exec client (container auto-discovered if not given)."""
    return DockerExecClient(container=container)


def shared_exec() -> DockerExecClient:
    """One exec client for the process — created lazily so importing this
    module never touches Docker (keeps tests + ``--help`` offline-cheap)."""
    global _client
    if _client is None:
        _client = get_exec_client()
    return _client


if __name__ == "__main__":  # pragma: no cover - operator smoke probe
    ec = get_exec_client()
    out, code = ec.exec("cat /etc/hostname && echo --- && ls /sandbox/workspace | head -3")
    print(f"[container={ec.container} exit={code}]")
    print(out)
