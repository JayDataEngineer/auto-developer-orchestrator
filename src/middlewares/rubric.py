"""``middleware:`` refs → deepagents' native ``AgentMiddleware``.

The org's ``middleware: [rubric]`` plus the agent's ``rubric:`` prose map onto
deepagents' own ``RubricMiddleware`` — the grader runs inside the graph the
same way dcode itself would run it, no re-implemented grading. Unknown refs
raise (no silent drops).
"""
from __future__ import annotations

from typing import Any

from deepagents.middleware import RubricMiddleware

_KNOWN = ("rubric",)


def agent_middlewares(spec: dict[str, Any], *, model: Any) -> list[RubricMiddleware]:
    """The agent spec's ``middleware:`` refs as native middleware instances."""
    out: list[RubricMiddleware] = []
    for ref in spec.get("middleware") or []:
        if ref == "rubric":
            prose = spec.get("rubric")
            out.append(RubricMiddleware(model=model, system_prompt=prose or None))
        else:
            raise ValueError(f"unknown middleware ref {ref!r} (known: {_KNOWN})")
    return out
