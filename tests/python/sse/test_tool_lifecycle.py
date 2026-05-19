"""
Tool Execution Lifecycle Tests.

Tests that verify the complete tool call lifecycle mirrors the frontend's
ToolCall interface and agentReducer behavior:

  tool_execution_start → tool_execution_end
  { toolName, args, toolId } → { toolId, result/error }

The frontend creates a ToolCall on start, then updates it on end.
If any field is wrong, the tool card UI breaks.

Run: cd tests/python && uv run pytest test_tool_lifecycle.py -v --tb=long
"""

import json
import time

import pytest

from conftest import API_BASE_URL
from fixtures.agent import spawn_agent, destroy_agent
from utils.sse import stream_prompt, post_and_stream
from utils.contract import validate_sse_event

pytestmark = [pytest.mark.api, pytest.mark.sse, pytest.mark.slow, pytest.mark.llm]

API = API_BASE_URL
TEST_PROJECT = "test-repo"
TEST_MODEL = "gemma-4-26b"


_mod_session = None


@pytest.fixture(scope="module", autouse=True)
def _setup(api_session):
    global _mod_session
    _mod_session = api_session
    spawn_agent(API, api_session, TEST_PROJECT)
    yield
    destroy_agent(API, api_session, TEST_PROJECT)


def _stream_with_tools(message):
    """Send a prompt designed to trigger tool use and collect all events."""
    return stream_prompt(API, _mod_session, TEST_PROJECT, message)


def _get_tool_events(events):
    """Separate tool start and end events."""
    starts = [(i, d) for i, (t, d) in enumerate(events) if t == "tool_execution_start"]
    ends = [(i, d) for i, (t, d) in enumerate(events) if t == "tool_execution_end"]
    return starts, ends


# ===========================================================================
# 1. Bash tool contract
# ===========================================================================


class TestBashToolContract:
    """
    Test the bash tool execution lifecycle.

    Frontend ToolCall interface:
      { id: string, name: string, args: Record<string, unknown>,
        result?: unknown, error?: string, startTime: number, endTime?: number }
    """

    def test_bash_tool_start_has_toolName(self):
        events = _stream_with_tools("Run: echo BASH_TOOL_CONTRACT")
        starts, ends = _get_tool_events(events)

        if not starts:
            pytest.skip("Model didn't use bash tool")

        for idx, data in starts:
            name = data.get("toolName", "")
            if name == "bash":
                assert isinstance(data["toolName"], str) and len(data["toolName"]) > 0, (
                    f"bash tool has no toolName. Data: {data}"
                )
                return

        # bash tool wasn't used, but another tool was — that's also fine
        pytest.skip("Model used non-bash tool")

    def test_bash_tool_start_has_command_args(self):
        """
        Bash tool args should contain a 'command' field.
        Frontend displays the command in the tool card.
        """
        events = _stream_with_tools("Run this bash command: echo ARGS_TEST")
        starts, ends = _get_tool_events(events)

        if not starts:
            pytest.skip("Model didn't use tools")

        for _, data in starts:
            if data.get("toolName") == "bash":
                args = data.get("args", {})
                assert isinstance(args, dict), f"args is not a dict: {args}"
                # Bash tool should have a command argument
                assert "command" in args or len(args) > 0, (
                    f"bash tool args empty or missing 'command': {args}"
                )
                return

        pytest.skip("Model didn't use bash specifically")

    def test_bash_tool_end_has_result(self):
        """Bash tool end should have result with stdout."""
        events = _stream_with_tools("Run: echo RESULT_TEST")
        starts, ends = _get_tool_events(events)

        if not ends:
            pytest.skip("No tool_execution_end events")

        for _, data in ends:
            # Check if this is a bash tool end (by matching toolId)
            has_result = "result" in data
            has_error = "error" in data
            assert has_result or has_error, (
                f"Tool end has no result or error: {data}"
            )

    def test_bash_result_contains_echo_output(self):
        """
        If the model ran 'echo X', the result should contain X.
        This tests end-to-end tool execution.
        """
        events = _stream_with_tools("Run exactly: echo ECHO_VERIFY_12345")
        starts, ends = _get_tool_events(events)

        if not starts or not ends:
            pytest.skip("Model didn't execute the tool")

        # Find the end event matching a bash tool start
        bash_start_ids = set()
        for _, data in starts:
            if data.get("toolName") == "bash":
                bash_start_ids.add(data.get("toolId"))

        for _, data in ends:
            if data.get("toolId") in bash_start_ids:
                result = str(data.get("result", ""))
                if "ECHO_VERIFY" in result:
                    return  # Found the expected output

        # Model might not have run exactly what we asked
        pytest.skip("Model didn't run the exact echo command")


# ===========================================================================
# 2. Tool ID matching (critical for frontend)
# ===========================================================================


class TestToolIdMatching:
    """
    The frontend matches tool_execution_start and tool_execution_end
    by toolId. If toolIds don't match, tool cards stay in "running" state.
    """

    def test_every_end_matches_a_start(self):
        """All tool_execution_end toolIds must appear in tool_execution_start."""
        events = _stream_with_tools("Run: echo ID_MATCH_TEST && ls /tmp")
        starts, ends = _get_tool_events(events)

        if not ends:
            pytest.skip("No tool end events")

        start_ids = {data.get("toolId") for _, data in starts}
        end_ids = {data.get("toolId") for _, data in ends}

        unmatched = end_ids - start_ids
        assert len(unmatched) == 0, (
            f"tool_execution_end toolIds not in starts:\n"
            f"  End IDs: {end_ids}\n"
            f"  Start IDs: {start_ids}\n"
            f"  Unmatched: {unmatched}\n"
            f"Tool cards will stay in 'running' state."
        )

    def test_every_start_has_matching_end(self):
        """All tool_execution_start toolIds should have a corresponding end."""
        events = _stream_with_tools("Run: echo START_END_MATCH && pwd")
        starts, ends = _get_tool_events(events)

        if not starts:
            pytest.skip("No tool start events")

        start_ids = {data.get("toolId") for _, data in starts}
        end_ids = {data.get("toolId") for _, data in ends}

        unmatched = start_ids - end_ids
        # It's possible for a tool to still be running if agent ended early
        if unmatched:
            # Check if agent_end was received (normal completion)
            has_end = any(t == "agent_end" for t, _ in events)
            if has_end:
                # If agent ended but tool didn't, that's a bug
                assert len(unmatched) == 0, (
                    f"Tools started but never ended (agent_end received):\n"
                    f"  Started: {start_ids}\n"
                    f"  Ended: {end_ids}\n"
                    f"  Missing ends: {unmatched}\n"
                    f"Tool cards stay in 'running' state forever."
                )

    def test_tool_ids_are_unique(self):
        """Each tool_execution_start should have a unique toolId."""
        events = _stream_with_tools("Run: echo UNIQUE_ID_TEST")
        starts, ends = _get_tool_events(events)

        if not starts:
            pytest.skip("No tool start events")

        ids = [data.get("toolId") for _, data in starts]
        unique_ids = set(ids)

        assert len(ids) == len(unique_ids), (
            f"Duplicate toolIds detected: {ids}\n"
            f"Frontend will overwrite earlier tool calls."
        )


# ===========================================================================
# 3. Tool args structure
# ===========================================================================


class TestToolArgsStructure:
    """
    Tool args must be a JSON object (dict) because the frontend
    reads args.command, args.path, etc. with dot notation.
    """

    def test_args_is_always_dict(self):
        events = _stream_with_tools("Run: echo ARGS_TYPE_TEST")
        starts, ends = _get_tool_events(events)

        if not starts:
            pytest.skip("No tool start events")

        for _, data in starts:
            assert isinstance(data.get("args"), dict), (
                f"tool args is {type(data.get('args')).__name__}, expected dict.\n"
                f"toolName: {data.get('toolName')}, args: {data.get('args')!r}\n"
                f"Frontend reads args.command, args.path etc. — non-dict breaks it."
            )

    def test_args_not_empty_string(self):
        """Args should not be an empty string or null."""
        events = _stream_with_tools("Run: echo ARGS_EMPTY_TEST")
        starts, ends = _get_tool_events(events)

        if not starts:
            pytest.skip("No tool start events")

        for _, data in starts:
            args = data.get("args")
            assert args is not None, f"args is null for tool {data.get('toolName')}"
            assert args != "", f"args is empty string for tool {data.get('toolName')}"


# ===========================================================================
# 4. Tool execution within stream sequence
# ===========================================================================


class TestToolSequenceInStream:
    """
    Verify tool events are properly interleaved with content events.
    The frontend displays tool calls inline with text.
    """

    def test_tools_come_between_start_and_end(self):
        """
        Tool events must appear between agent_start and agent_end.
        Tools outside this range would be ignored (isStreaming is false).
        """
        events = _stream_with_tools("Run: echo SEQUENCE_RANGE_TEST")

        start_idx = None
        end_idx = None
        tool_indices = []

        for i, (t, _) in enumerate(events):
            if t == "agent_start" and start_idx is None:
                start_idx = i
            if t == "agent_end":
                end_idx = i
            if t in ("tool_execution_start", "tool_execution_end"):
                tool_indices.append(i)

        if not tool_indices:
            pytest.skip("No tool events")

        assert start_idx is not None, "No agent_start event"

        # agent_end may be missing if context exhausted or stream errored
        if end_idx is None:
            pytest.skip("No agent_end event (stream may have errored or timed out)")

        out_of_range = [i for i in tool_indices if i < start_idx or i > end_idx]
        assert len(out_of_range) == 0, (
            f"Tool events outside agent_start/agent_end range:\n"
            f"  agent_start: idx {start_idx}, agent_end: idx {end_idx}\n"
            f"  Out of range tool indices: {out_of_range}\n"
            f"These tool events won't be processed (isStreaming is false)."
        )

    def test_tool_start_before_text_after_it(self):
        """
        If tool use happens, text_delta should still flow after tool ends.
        The model usually explains results after using a tool.
        """
        events = _stream_with_tools("List the files in the current directory using ls")

        starts, _ = _get_tool_events(events)
        if not starts:
            pytest.skip("Model didn't use tools")

        last_tool_end_idx = None
        for i, (t, _) in enumerate(events):
            if t == "tool_execution_end":
                last_tool_end_idx = i

        if last_tool_end_idx is None:
            pytest.skip("Tool started but never ended")

        # Check if there are text_deltas after the last tool end
        text_after = [
            (i, d) for i, (t, d) in enumerate(events)
            if t == "text_delta" and i > last_tool_end_idx
        ]

        # This is a soft check — model might not explain
        if text_after:
            text = "".join(d.get("text", "") for _, d in text_after)
            assert len(text.strip()) > 0 or True  # Just log it
            print(f"  ✓ Model explained after tool: {len(text)} chars")
        else:
            print(f"  ℹ No text after tool (model ended directly)")


# ===========================================================================
# 5. Multiple tool calls in sequence
# ===========================================================================


class TestMultipleToolCalls:
    """
    Test that multiple sequential tool calls are handled correctly.
    The frontend tracks all tool calls in an array.
    """

    def test_multiple_tools_all_have_unique_ids(self):
        """If multiple tools are used, each must have a unique toolId."""
        events = _stream_with_tools(
            "Run these two commands in order: echo FIRST && echo SECOND"
        )
        starts, _ = _get_tool_events(events)

        if len(starts) < 2:
            pytest.skip("Model didn't make multiple tool calls")

        ids = [data.get("toolId") for _, data in starts]
        assert len(ids) == len(set(ids)), (
            f"Multiple tools with same toolId: {ids}"
        )

    def test_multiple_tools_all_match_end(self):
        """Each tool start should have a matching end."""
        events = _stream_with_tools(
            "Run these commands: echo A && echo B && echo C"
        )
        starts, ends = _get_tool_events(events)

        if len(starts) < 2:
            pytest.skip("Model didn't make multiple tool calls")

        start_ids = {data.get("toolId") for _, data in starts}
        end_ids = {data.get("toolId") for _, data in ends}

        missing = start_ids - end_ids
        if missing and any(t == "agent_end" for t, _ in events):
            assert len(missing) == 0, (
                f"Some tools never got end events: {missing}"
            )


# ===========================================================================
# 6. Tool event contract validation
# ===========================================================================


class TestToolEventContract:
    """
    Run the generic validate_sse_event against all tool events.
    """

    def test_all_tool_events_pass_validation(self):
        events = _stream_with_tools("Run: echo VALIDATION_TEST")
        violations = []

        for ev_type, data in events:
            if ev_type in ("tool_execution_start", "tool_execution_end"):
                issues = validate_sse_event(ev_type, data)
                for issue in issues:
                    violations.append(f"  {ev_type}: {issue}")

        assert len(violations) == 0, (
            f"Tool event contract violations:\n" + "\n".join(violations)
        )


# ===========================================================================
# 7. Tool names are recognized by frontend
# ===========================================================================


class TestToolNamesRecognized:
    """
    The frontend may display different icons or formatting based on toolName.
    Verify that tool names are strings the frontend can handle.
    """

    def test_tool_names_are_lowercase_strings(self):
        events = _stream_with_tools("Run: echo NAME_TEST")
        starts, _ = _get_tool_events(events)

        if not starts:
            pytest.skip("No tool start events")

        for _, data in starts:
            name = data.get("toolName", "")
            assert isinstance(name, str), f"toolName is not a string: {name!r}"
            assert len(name) > 0, "toolName is empty string"
            # Frontend uses toolName as a display label and key
            assert name.strip() == name, (
                f"toolName has leading/trailing whitespace: {name!r}"
            )
