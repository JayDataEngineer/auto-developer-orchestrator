"""Repo-wide tripwires for the dcode workspace — the TEST half of the former
``pux_harness/agent/contract.py``, slimmed to what the folded workspace needs.

Two permanent gates (both ``verify-or-die``):

* **``kit-import-isolation``** — the PURE emission surface (``src/compiler``,
  ``src/protocol``, ``src/plugins`` + the pure profile loaders
  ``src/profiles/{_paths,loaders}.py``) must import NEITHER a heavy runtime
  module (``deepagents``/``openshell``/``langchain*``/server lane) NOR a
  sibling runtime package (``src/run.py``, ``src/tools``, ``src/sandbox``,
  ``src/middlewares``). The compiler is pure data→format projection; the
  runtime lives in the packages that are NOT scanned here. Eager OR lazy
  (``ast.walk`` descends into function bodies). Relative imports are resolved
  first, so a within-surface ``from .loaders import ...`` is not a false
  positive. (``src/profiles/subagents.py`` is runtime — imports
  ``tools.resolve`` / ``middlewares.rubric`` — and is deliberately NOT in the
  scanned set.)
* **``no-pux-harness-refs``** — the literal ``pux_harness`` may not appear
  anywhere under ``src/``. The harness is gone; the workspace IS the repo.

Each check returns ``list[Problem]`` (empty = green). They live here — in the
test suite — so they are optional and never deployed.
"""

from __future__ import annotations

import ast
import re
from pathlib import Path
from typing import NamedTuple

_SRC = Path(__file__).resolve().parents[2] / "src"

# The pure emission surface — the "kit" of the workspace. src/tools,
# src/sandbox, src/middlewares, src/run.py are the RUNTIME and are not scanned.
_CLEAN_PACKAGES = ("compiler", "protocol", "plugins")
_CLEAN_PROFILES_FILES = ("_paths.py", "loaders.py")


class Problem(NamedTuple):
    severity: str
    rule: str
    message: str


# --- kit import isolation ----------------------------------------------------

# Heavy runtime roots the pure surface must not import: the graph/sandbox
# runtime (deepagents/openshell/langchain) + the dead server lane, kept banned.
_HEAVY_MODULE_ROOTS: frozenset[str] = frozenset(
    {
        "docker",  # [sandbox]
        "selenium",
        "seleniumbase",  # [browser]
        "fastapi",
        "uvicorn",
        "starlette",  # [server] HTTP runtime (dead lane, still banned)
        "ag_ui_langgraph",  # [server] AG-UI transport
        "fastmcp",
        "langchain_mcp_adapters",  # [mcp] MCP servers/adapters
        "deepagents",  # [runtime] the graph — src/run.py's, not the compiler's
        "deepagents_code",  # [runtime] dcode TUI + config
        "openshell",  # [runtime] sandbox gateway
        "langchain",  # [runtime] tool/llm layer
        "langchain_core",
        "langgraph",
        "httpx",
    }
)

# The workspace's runtime packages — importing one from the pure surface is a
# reach into the runtime (the subagents module is the proof of what that looks
# like: profiles/subagents.py imports tools.resolve + middlewares.rubric).
_RUNTIME_SIBLINGS: frozenset[str] = frozenset({"run", "tools", "sandbox", "middlewares"})


def _resolve_relative_import(level: int, module: str | None, pkg_parts: list[str]) -> str:
    """Resolve a relative ``from . import ...`` to its absolute dotted name."""
    base = pkg_parts[: len(pkg_parts) - (level - 1)] if level >= 1 else list(pkg_parts)
    if module:
        base = base + module.split(".")
    return ".".join(base)


def _scan_for_heavy_imports(src: Path, pkg_parts: list[str]) -> list[Problem]:
    """AST-scan ONE pure-surface source file for imports it must not make:
    heavy runtime roots (deepagents/openshell/...) OR a sibling runtime
    package (``tools``/``sandbox``/``middlewares``/``run``). Relative imports
    are resolved first."""
    v: list[Problem] = []
    try:
        tree = ast.parse(src.read_text())
    except SyntaxError as exc:  # pragma: no cover
        v.append(Problem("error", "kit-import-isolation", f"{src}: does not parse: {exc}"))
        return v

    def _check(name: str) -> None:
        root = name.split(".")[0]
        if root in _HEAVY_MODULE_ROOTS:
            v.append(
                Problem(
                    "error",
                    "kit-import-isolation",
                    f"{src}: imports heavy module {name!r} — the pure emission "
                    f"surface must not depend on it (the RUNTIME's home is "
                    f"src/run.py, tools/, sandbox/, middlewares/)",
                )
            )
        elif root in _RUNTIME_SIBLINGS:
            v.append(
                Problem(
                    "error",
                    "kit-import-isolation",
                    f"{src}: imports runtime sibling {name!r} — the pure "
                    f"emission surface must not reach into the runtime",
                )
            )

    for node in ast.walk(tree):
        if isinstance(node, ast.Import):
            for alias in node.names:
                _check(alias.name)
        elif isinstance(node, ast.ImportFrom):
            if node.level and node.level > 0:
                resolved = _resolve_relative_import(node.level, node.module, pkg_parts)
            else:
                resolved = node.module or ""
            if resolved and resolved != "__future__":
                _check(resolved)
    return v


def _kit_import_isolation() -> list[Problem]:
    """The pure emission surface — ``src/{compiler,protocol,plugins}/**`` +
    ``src/profiles/{_paths,loaders}.py`` — must import NEITHER a heavy runtime
    module NOR a sibling runtime package."""
    targets: list[tuple[Path, list[str]]] = []
    for pkg in _CLEAN_PACKAGES:
        d = _SRC / pkg
        for py in sorted(d.glob("*.py")):
            targets.append((py, [pkg]))
    for name in _CLEAN_PROFILES_FILES:
        targets.append((_SRC / "profiles" / name, ["profiles"]))
    v: list[Problem] = []
    for src, pkg_parts in targets:
        if src.is_file():
            v.extend(_scan_for_heavy_imports(src, pkg_parts))
    return v


# --- no pux_harness refs -----------------------------------------------------

_PUX_RE = re.compile(r"pux_harness")


def _no_pux_harness_refs() -> list[Problem]:
    """The literal ``pux_harness`` may not appear anywhere under ``src/`` —
    the harness died with the dual track; the workspace IS the repo."""
    v: list[Problem] = []
    for src in sorted(_SRC.rglob("*.py")):
        for i, line in enumerate(src.read_text().splitlines(), start=1):
            if _PUX_RE.search(line):
                v.append(
                    Problem(
                        "error", "no-pux-harness-refs",
                        f"{src}:{i}: {line.strip()}",
                    )
                )
    return v
