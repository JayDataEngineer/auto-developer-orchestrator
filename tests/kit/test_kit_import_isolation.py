"""Import hygiene — the pure emission surface's import boundary is a PERMANENT
contract, not a passing today-property.

The tripwire ``kit-import-isolation`` (in ``tests/guards/tripwire_checks.py``)
fires the moment a heavy import (or a sibling-runtime import) is added to the
pure surface — ``src/compiler``, ``src/protocol``, ``src/plugins`` + the pure
profile loaders ``src/profiles/{_paths,loaders}.py``. Eager OR lazy
(``ast.walk`` descends into function bodies). Proves the compiler can't
silently re-couple to the runtime (``src/run.py``, ``src/tools``,
``src/sandbox``, ``src/middlewares``), which is the precondition for the
compiler staying a pure data→format projection of the profiles tree.

The tripwire resolves relative imports to absolute first, so a within-surface
``from .loaders import ...`` (resolves to ``profiles.loaders``) is NOT a false
positive.

The tripwire lives in ``tests/guards/tripwire_checks.py`` — a permanent,
repo-wide gate that is optional and never deployed. Being imported by this
suite IS its registration.
"""
from __future__ import annotations

import pytest

from tests.guards.tripwire_checks import (
    _HEAVY_MODULE_ROOTS,
    _kit_import_isolation,
    _scan_for_heavy_imports,
)

pytestmark = pytest.mark.guards

# Representative package identity for the provocation files: the compiler is
# the core of the pure surface.
KIT_PKG = ["compiler"]


# --- green on the real repo ------------------------------------------------


def test_tripwire_green_on_real_surface():
    """The real pure surface — src/{compiler,protocol,plugins}/** +
    src/profiles/{_paths,loaders}.py — imports nothing heavy and reaches no
    runtime sibling: the tripwire is clean on the shipped source."""
    assert _kit_import_isolation() == [], _kit_import_isolation()


# --- provocation: heavy module import trips --------------------------------


def test_trips_on_heavy_absolute_import(tmp_path):
    """``import docker`` in a pure-surface file is a hard failure."""
    src = tmp_path / "emit.py"
    src.write_text("import docker\n")
    vs = _scan_for_heavy_imports(src, KIT_PKG)
    assert len(vs) == 1, vs
    assert vs[0].rule == "kit-import-isolation"
    assert vs[0].severity == "error"
    assert "docker" in vs[0].message


def test_trips_on_heavy_from_import(tmp_path):
    """``from fastapi import FastAPI`` is just as much a leak."""
    src = tmp_path / "emit.py"
    src.write_text("from fastapi import FastAPI\n")
    vs = _scan_for_heavy_imports(src, KIT_PKG)
    assert len(vs) == 1, vs
    assert vs[0].rule == "kit-import-isolation"
    assert "fastapi" in vs[0].message


def test_trips_on_lazy_import_inside_function(tmp_path):
    """A deferred ``import deepagents`` inside a function body still trips —
    the pure surface must not reference the runtime AT ALL, not even lazily."""
    src = tmp_path / "emit.py"
    src.write_text(
        "def f():\n"
        "    import deepagents  # lazily imported, but still a pure->runtime coupling\n"
        "    return deepagents\n"
    )
    vs = _scan_for_heavy_imports(src, KIT_PKG)
    assert any(v.rule == "kit-import-isolation" and "deepagents" in v.message for v in vs), vs


def test_trips_on_every_declared_heavy_root(tmp_path):
    """Every root in ``_HEAVY_MODULE_ROOTS`` is rejected — none can sneak in."""
    for root in _HEAVY_MODULE_ROOTS:
        src = tmp_path / f"{root}_probe.py"
        src.write_text(f"import {root}\n")
        vs = _scan_for_heavy_imports(src, KIT_PKG)
        assert any(root in v.message for v in vs), f"{root} not flagged: {vs}"


# --- provocation: sibling runtime reach trips ------------------------------


def test_trips_on_sibling_runtime_absolute(tmp_path):
    """``from sandbox.local import X`` couples the pure surface to the runtime."""
    src = tmp_path / "emit.py"
    src.write_text("from sandbox.local import local_backend\n")
    vs = _scan_for_heavy_imports(src, KIT_PKG)
    assert len(vs) == 1, vs
    assert "sandbox.local" in vs[0].message


def test_trips_on_sibling_runtime_import_inside_function(tmp_path):
    """A deferred ``from tools.registry import REGISTRY`` is just as much a
    leak — profiles/subagents.py is the runtime's shape, not the compiler's."""
    src = tmp_path / "emit.py"
    src.write_text(
        "def f():\n"
        "    from tools.registry import REGISTRY\n"
        "    return REGISTRY\n"
    )
    vs = _scan_for_heavy_imports(src, KIT_PKG)
    assert any(v.rule == "kit-import-isolation" and "tools.registry" in v.message
               for v in vs), vs


def test_trips_on_top_level_init_importing_runtime(tmp_path):
    """The top-level ``src/__init__.py`` may import only the pure surface."""
    src = tmp_path / "__init__.py"
    src.write_text("from run import launch\n")
    vs = _scan_for_heavy_imports(src, ["src"])
    assert len(vs) == 1, vs
    assert "run" in vs[0].message


# --- no false positives: the allowed surface stays quiet --------------------


def test_clean_on_real_surface_imports(tmp_path):
    """A source mirroring the REAL pure-surface imports (stdlib + yaml +
    within-surface siblings) emits nothing."""
    src = tmp_path / "emit.py"
    src.write_text(
        "from __future__ import annotations\n"
        "import hashlib\n"
        "import json\n"
        "import shutil\n"
        "import tempfile\n"
        "from pathlib import Path\n"
        "from typing import Any\n"
        "import yaml\n"
        "from profiles._paths import project_root\n"
        "from compiler.capabilities import desugar_agent_capabilities\n"
        "from protocol.mcp import _org_mcp_servers\n"
    )
    assert _scan_for_heavy_imports(src, KIT_PKG) == []


def test_clean_on_within_surface_relative_from_init(tmp_path):
    """``profiles/loaders.py`` doing ``from .loaders import _load_agent_spec``
    resolves to ``profiles.loaders`` — NOT a runtime reach."""
    src = tmp_path / "__init__.py"
    src.write_text(
        "from ._paths import project_root\n"
        "from .loaders import discover_orgs\n"
    )
    assert _scan_for_heavy_imports(src, ["profiles"]) == []


def test_clean_on_top_level_init_reexporting_compiler(tmp_path):
    """The top-level re-export ``from compiler.emit import emit_union`` is a
    pure-surface import — it stays quiet."""
    src = tmp_path / "__init__.py"
    src.write_text(
        "from __future__ import annotations\n"
        "from compiler.emit import emit_union\n"
        "__all__ = ['emit_union']\n"
    )
    assert _scan_for_heavy_imports(src, ["src"]) == []
