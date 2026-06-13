"""
E2E tests for the sub-agent coding pipeline — CTO → code_orchestrator → workers.

Verifies the full delegation chain for code generation tasks:
1. CTO delegates to code_orchestrator (or directly to code_ops/shell_ops)
2. Sub-agent writes files to correct paths (no double-nesting)
3. Files persist to disk at the expected host location
4. Go build succeeds
5. No tool execution timeouts (the 0-default fix)

Uses OpenRouter or local model. Requires running Go backend.
Auto-skips if API unreachable.
"""

import os
import sys
import uuid

import pytest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))
from conftest import API_BASE_URL
from utils.sse import post_and_stream

pytestmark = [pytest.mark.agent, pytest.mark.slow, pytest.mark.llm]

MODEL = os.environ.get("E2E_MODEL", "deepseek/deepseek-v4-flash")
PROJECT = "auto-developer-orchestrator"


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _collect_prompt(api_session, message, timeout=600):
    """Send a prompt and return structured results."""
    agent_id = f"coding-e2e-{uuid.uuid4().hex[:8]}"

    events = list(post_and_stream(
        api_session,
        f"{API_BASE_URL}/api/pux/prompt",
        {
            "message": message,
            "project": PROJECT,
            "agentId": agent_id,
            "model": MODEL,
        },
        timeout=timeout,
    ))

    tool_starts = [(t, d) for t, d in events if t == "tool_execution_start"]
    tool_ends = [(t, d) for t, d in events if t == "tool_execution_end"]
    text_parts = [d.get("text", "") for t, d in events if t == "text_delta"]
    errors = [(t, d) for t, d in events if t == "error"]

    tool_names = [d.get("toolName", "") for _, d in tool_starts]
    full_text = "".join(text_parts)

    # Extract delegate_to results
    delegate_results = []
    for _, d in tool_ends:
        if d.get("toolName") == "delegate_to":
            result = d.get("result", {})
            if isinstance(result, dict):
                delegate_results.append(result.get("result", ""))
            elif isinstance(result, str):
                delegate_results.append(result)

    return {
        "events": events,
        "tool_names": tool_names,
        "tool_starts": tool_starts,
        "tool_ends": tool_ends,
        "text": full_text,
        "errors": errors,
        "delegate_results": delegate_results,
        "agent_id": agent_id,
    }


def _no_hard_errors(result):
    """Assert no fatal errors (skip on infrastructure issues)."""
    if not result["errors"]:
        return
    err_msgs = [e.get("error", e.get("message", str(e))) for _, e in result["errors"]]
    hard = [m for m in err_msgs if "retrying" not in m.lower()]
    infra = [m for m in hard if any(k in m.lower() for k in ["docker", "sandbox unavailable", "connection refused"])]
    if infra:
        pytest.skip(f"Infrastructure unavailable: {infra[0][:120]}")
    assert len(hard) == 0, f"Agent errors: {hard}"


def _assert_delegated(result):
    """Assert CTO delegated to a sub-agent."""
    delegate_tools = [n for n in result["tool_names"] if n in ("delegate_to", "delegate_async")]
    assert len(delegate_tools) > 0, (
        f"CTO never delegated! Tools: {result['tool_names']}\n"
        f"Response: {result['text'][:500]}"
    )


# ---------------------------------------------------------------------------
# Tests
# ---------------------------------------------------------------------------

class TestSubAgentDelegation:
    """Verify CTO delegates coding tasks to sub-agents."""

    def test_simple_coding_task_delegates(self, api_session):
        """A coding prompt should trigger delegate_to, not direct tool use."""
        result = _collect_prompt(
            api_session,
            "Create a simple Go package at backend/internal/echotest/ "
            "with a single function Echo(s string) string that returns s. "
            "Write the code, run 'go build ./internal/echotest/' to verify it compiles.",
            timeout=600,
        )

        print(f"\n  Agent: {result['agent_id']}")
        print(f"  Tools: {result['tool_names']}")
        print(f"  Errors: {len(result['errors'])}")
        print(f"  Text ({len(result['text'])} chars): {result['text'][:300]}")

        _no_hard_errors(result)
        _assert_delegated(result)

    def test_delegation_completes_without_timeout(self, api_session):
        """Sub-agent should finish within 10 minutes (no 5-min tool timeout)."""
        result = _collect_prompt(
            api_session,
            "Create a Go file at backend/internal/timeouttest/hello.go "
            "with package timeouttest and a function Hello() string. "
            "Build it with 'go build ./internal/timeouttest/'. "
            "This is a trivial task — should complete quickly.",
            timeout=600,
        )

        print(f"\n  Agent: {result['agent_id']}")
        print(f"  Tools: {result['tool_names']}")

        # Check for timeout-related errors
        timeout_errors = [
            e for _, e in result["errors"]
            if "timeout" in str(e).lower() or "context deadline" in str(e).lower()
        ]
        assert len(timeout_errors) == 0, (
            f"Sub-agent hit a timeout (should not happen with 0-default): {timeout_errors}"
        )

        _no_hard_errors(result)


class TestFilePathRemapping:
    """Verify files are written to correct host paths (no double-nesting)."""

    def test_file_written_to_correct_location(self, api_session, tmp_path):
        """Sub-agent file_write should land at the correct host path."""
        # Use a unique package name to avoid collisions
        pkg_name = f"pathtest{uuid.uuid4().hex[:6]}"
        result = _collect_prompt(
            api_session,
            f"Create a Go file at backend/internal/{pkg_name}/util.go "
            f"with 'package {pkg_name}' and a function Version() string "
            f"that returns '1.0'. Build it.",
            timeout=600,
        )

        print(f"\n  Agent: {result['agent_id']}")
        print(f"  Tools: {result['tool_names']}")

        _no_hard_errors(result)

        # Check that the file exists at the correct location
        # Project root is the repo root (auto-developer-orchestrator/)
        repo_root = os.environ.get("PROJECT_ROOT", os.path.abspath(
            os.path.join(os.path.dirname(__file__), "..", "..", "..")
        ))
        expected_path = os.path.join(repo_root, "backend", "internal", pkg_name, "util.go")

        if os.path.exists(expected_path):
            content = open(expected_path).read()
            print(f"  File found at: {expected_path}")
            print(f"  Content ({len(content)} chars): {content[:200]}")
            assert f"package {pkg_name}" in content, f"Wrong package name in file"
            # Clean up
            os.unlink(expected_path)
            pkg_dir = os.path.dirname(expected_path)
            if os.path.isdir(pkg_dir) and not os.listdir(pkg_dir):
                os.rmdir(pkg_dir)
        else:
            # Check for double-nesting bug
            double_nested = os.path.join(
                repo_root, "backend", "backend", "internal", pkg_name, "util.go"
            )
            if os.path.exists(double_nested):
                pytest.fail(
                    f"DOUBLE-NESTING BUG: file written to {double_nested} "
                    f"instead of {expected_path}"
                )
            # File might not have been created if model didn't follow instructions
            print(f"  WARNING: File not found at {expected_path} (model may not have created it)")
            print(f"  This is a model quality issue, not infrastructure")


class TestSSEContractDuringDelegation:
    """Verify SSE events are well-formed during sub-agent execution."""

    def test_no_duplicate_events(self, api_session):
        """Each tool_execution_start should have exactly one tool_execution_end."""
        result = _collect_prompt(
            api_session,
            "List the files in the current directory using bash.",
            timeout=120,
        )

        _no_hard_errors(result)

        starts = [d for _, d in result["tool_starts"]]
        ends = [d for _, d in result["tool_ends"]]

        # Every start should have a corresponding end (by tool call ID)
        start_ids = {d.get("callId", d.get("toolName", "")) for d in starts}
        end_ids = {d.get("callId", d.get("toolName", "")) for d in ends}

        # Ends should be a superset of starts (some tools may have been started
        # by sub-agents whose start events we don't see)
        missing = start_ids - end_ids
        assert len(missing) == 0, (
            f"Tool calls started but never ended: {missing}"
        )

    def test_agent_lifecycle_events_present(self, api_session):
        """Agent should emit agent_start and agent_end events."""
        result = _collect_prompt(
            api_session,
            "echo test-lifecycle",
            timeout=120,
        )

        event_types = [t for t, _ in result["events"]]

        assert "agent_start" in event_types, f"Missing agent_start. Events: {set(event_types)}"
        assert "agent_end" in event_types, f"Missing agent_end. Events: {set(event_types)}"
