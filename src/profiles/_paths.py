"""Location-independent project-root resolver — the ONE shared seam.

Every subsystem calls :func:`project_root` instead of ``Path(__file__).parents[N]``
(which shatters when the harness is installed elsewhere). Resolution:
``$PUX_PROJECT_ROOT`` override, else CWD. Lives in the slim kit so the heavy
layer depends on it without a cycle.
"""
from __future__ import annotations

import os
from pathlib import Path

_PROJECT_ROOT_ENV = "PUX_PROJECT_ROOT"


def project_root() -> Path:
    """The app root: ``$PUX_PROJECT_ROOT`` if set, else the resolved CWD."""
    return Path(os.environ.get(_PROJECT_ROOT_ENV, ".")).resolve()


# --- cross-project org library -----------------------------------------------
#
# Two mechanisms let a consumer app REUSE profiles/shipped bases without vendoring:
#
# * ``$PUX_ORG_PATHS`` — colon-separated extra ``profiles/``-shaped roots. Org
#   resolution searches the project's own ``profiles/`` first, then each entry
#   (top-level, then its ``specialists/``). Local wins over extra roots.
# * the ``pux:`` namespace — a name prefixed ``pux:<base>`` resolves ONLY
#   against the shipped library bases (``src/profiles/bases/<base>/``), the
#   escape-hatch to a base even when a local namesake exists. Used in
#   ``extends: pux:`` (org inheritance) and roster / agent
#   ``extends: pux:<slug>`` (a library agent). The contract rule
#   ``pux-namespace-resolvable`` makes every ``pux:`` reference resolve or fail
#   loud.

ORG_NAMESPACE_PREFIX = "pux:"
_PUX_ORG_PATHS_ENV = "PUX_ORG_PATHS"


def library_bases_dir() -> Path:
    """The shipped library org bases — ``src/profiles/bases/``. A ``pux:``
    namespace target resolves here. Lives in the wheel so any consumer app
    extends a base without vendoring it."""
    return Path(__file__).resolve().parent / "bases"


def is_pux_namespace(name: object) -> bool:
    """True iff ``name`` is a ``pux:``-namespaced reference."""
    return isinstance(name, str) and name.startswith(ORG_NAMESPACE_PREFIX)


def strip_namespace(name: str) -> str:
    """Drop the ``pux:`` prefix if present (idempotent on a bare name)."""
    return name[len(ORG_NAMESPACE_PREFIX):] if is_pux_namespace(name) else name


def extra_org_roots() -> list[Path]:
    """Parsed ``$PUX_ORG_PATHS`` roots (each a ``profiles/``-shaped dir: it holds
    org dirs + an optional ``specialists/`` subdir). Non-existent entries are
    silently dropped; order is preserved (search order = listing order)."""
    out: list[Path] = []
    for part in os.environ.get(_PUX_ORG_PATHS_ENV, "").split(os.pathsep):
        if not part:
            continue
        root = Path(part).expanduser()
        if root.is_dir():
            out.append(root)
    return out


def library_base_dirs() -> list[Path]:
    """Existing library base ORG dirs (those shipping ``AGENTS.md``), sorted by
    name — the resolved targets of ``pux:<base>`` org references."""
    base = library_bases_dir()
    if not base.is_dir():
        return []
    return sorted(
        d for d in base.iterdir()
        if d.is_dir() and (d / "AGENTS.md").is_file()
    )


def library_base_agent_dirs() -> list[Path]:
    """The ``agents/`` dir of each library base (existing only, base-sorted) —
    the search path for a ``pux:``-namespaced agent slug."""
    return [d / "agents" for d in library_base_dirs() if (d / "agents").is_dir()]


def resolve_library_agent(slug: str) -> Path | None:
    """The ``<slug>.md`` path for a ``pux:``-namespaced agent slug, searched
    across every library base's ``agents/`` dir (first hit wins, base-sorted).
    ``None`` if no base ships it. ``slug`` may be passed namespaced or bare."""
    bare = strip_namespace(slug)
    for d in library_base_agent_dirs():
        candidate = d / f"{bare}.md"
        if candidate.is_file():
            return candidate
    return None


def search_org_dir(name: str, project_root: Path) -> Path:
    """Resolve an org directory across all roots; ``FileNotFoundError`` if none.
    ``pux:<base>`` resolves only against library bases. A bare name: project
    ``profiles/`` first (top-level then ``specialists/``), then ``$PUX_ORG_PATHS``.
    The ONE org-directory resolver (``loaders._org_path`` delegates here).
    """
    if is_pux_namespace(name):
        base = library_bases_dir() / strip_namespace(name)
        if base.is_dir():
            return base
        raise FileNotFoundError(
            f"pux: base {strip_namespace(name)!r} not found in library bases "
            f"({library_bases_dir()})"
        )
    for root in [project_root / "profiles", *extra_org_roots()]:
        top = root / name
        if top.is_dir():
            return top
        spec = root / "specialists" / name
        if spec.is_dir():
            return spec
    raise FileNotFoundError(
        f"org {name!r} not found under profiles/, profiles/specialists/, or $PUX_ORG_PATHS"
    )

