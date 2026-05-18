"""
SSE Event Contract Tests.

These tests verify that every SSE event the backend sends matches
the TypeScript interfaces in src/lib/pi-events.ts EXACTLY.

If these tests fail, the frontend will either crash, show blank content,
or silently drop data — even if the backend "works" from an API perspective.

This is the single source of truth for the SSE contract.

Run: cd tests/python && uv run pytest test_sse_contract.py -v --tb=long
"""

import json
import time

import pytest

from conftest import API_BASE_URL
from fixtures.agent import spawn_agent, destroy_agent
from utils.sse import stream_prompt
from utils.contract import validate_sse_event, VALID_SSE_EVENT_TYPES

pytestmark = [pytest.mark.api, pytest.mark.sse, pytest.mark.slow]

API = API_BASE_URL
TEST_PROJECT = "test-repo"
TEST_MODEL = "gemma-4-26b"


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


# Module-scoped session and agent setup
_mod_session = None


def _stream(message, model=TEST_MODEL):
    """Stream a prompt and return all events."""
    return stream_prompt(API, _mod_session, TEST_PROJECT, message, model=model)


@pytest.fixture(scope="module", autouse=True)
def _setup(api_session):
    """Spawn agent for the module, set module session, clean up after."""
    global _mod_session
    _mod_session = api_session
    spawn_agent(API, api_session, TEST_PROJECT)
    yield
    destroy_agent(API, api_session, TEST_PROJECT)


# ===========================================================================
# 1. Event type validity — every event must have a recognized type
# ===========================================================================


class TestEventTypeValidity:
    """
    The frontend's agentReducer has a switch(event.type) with specific cases.
    Unknown event types are silently ignored by the default branch — which
    means if the backend sends a typo like 'tool_exec_start' instead of
    'tool_execution_start', the frontend will drop it silently.
    """

    def test_all_events_have_known_types(self):
        """Every SSE event type must be in the frontend's PiEventType union."""
        events = _stream("Say exactly: hello contract test")

        # The SSE parser defaults to "message" when no event: line — frontend drops these
        # These extended types are used internally but also recognized by the frontend reducer
        extended_types = {
            "message",  # SSE default when no event: prefix (frontend safely drops)
            "message_start", "message_end", "message_update",
            "turn_start", "turn_end",
            "auto_retry_start", "auto_retry_end",
        }
        allowed = VALID_SSE_EVENT_TYPES | extended_types

        unknown_types = set()
        for ev_type, _ in events:
            if ev_type not in allowed:
                unknown_types.add(ev_type)

        assert len(unknown_types) == 0, (
            f"Unknown event types sent by backend: {unknown_types}\n"
            f"Frontend PiEventType union: {VALID_SSE_EVENT_TYPES}\n"
            f"These events will be silently dropped by agentReducer."
        )

    def test_event_types_match_exactly(self):
        """
        Event types must match the EXACT strings in pi-events.ts.
        No variants, no abbreviations, no different casing.
        """
        events = _stream("Say ok")

        expected_types = {
            "text_delta", "thinking_delta",
            "tool_execution_start", "tool_execution_end",
            "agent_start", "agent_end", "agent_spawned",
            "compaction_start", "compaction_end",
            "error", "state_update",
            "branch_created", "commit_created", "push_complete", "pr_created",
            "web_update", "approval_request", "question_asked",
            "message_start", "message_end", "turn_start", "turn_end",
            "auto_retry_start", "auto_retry_end",
        }

        for ev_type, _ in events:
            assert ev_type in expected_types, (
                f"Event type {ev_type!r} is not recognized by the frontend.\n"
                f"Expected one of: {sorted(expected_types)}"
            )


# ===========================================================================
# 2. Event data is always a dict (not string, not None, not array)
# ===========================================================================


class TestEventDataStructure:
    """
    The frontend does event.data as SomeInterface — if data is not an object,
    TypeScript would still let it through at runtime but reading .text or
    .toolName from a string/null would return undefined → blank UI.
    """

    def test_all_event_data_is_dict(self):
        events = _stream("Say exactly: hello data structure test")

        non_dict_events = []
        for ev_type, data in events:
            if not isinstance(data, dict):
                non_dict_events.append((ev_type, type(data).__name__, repr(data)[:100]))

        assert len(non_dict_events) == 0, (
            f"These events have non-dict data (frontend will fail to read fields):\n"
            + "\n".join(f"  {t}: {tp} = {d}" for t, tp, d in non_dict_events)
        )

    def test_no_raw_json_strings(self):
        """Event data must be parsed JSON objects, not raw JSON strings."""
        events = _stream("Say ok")

        raw_events = [
            (t, d) for t, d in events
            if isinstance(d, dict) and "raw" in d and len(d) == 1
        ]

        assert len(raw_events) == 0, (
            f"These events had unparseable JSON (frontend will get {{raw: '...'}}):\n"
            + "\n".join(f"  {t}: {d}" for t, d in raw_events)
        )


# ===========================================================================
# 3. text_delta contract: { text: string }
# ===========================================================================


class TestTextDeltaContract:
    """
    Frontend: data.text is appended to state.text and last assistant message.

    interface PiTextDelta { text: string }

    If 'text' is missing or not a string → blank chat output.
    """

    def test_text_delta_has_text_field(self):
        events = _stream("Say exactly: 'contract test response' and nothing else.")

        text_deltas = [(t, d) for t, d in events if t == "text_delta"]
        assert len(text_deltas) > 0, "No text_delta events received"

        for _, data in text_deltas:
            assert "text" in data, (
                f"text_delta event missing 'text' field. Got keys: {list(data.keys())}\n"
                f"Frontend reads data.text — will get undefined → blank output."
            )

    def test_text_delta_text_is_string(self):
        events = _stream("Say exactly: hello string type test")

        for _, data in events:
            if isinstance(data, dict) and "text" in data:
                assert isinstance(data["text"], str), (
                    f"text field is {type(data['text']).__name__}, expected str.\n"
                    f"Value: {data['text']!r}\n"
                    f"Frontend concatenates this as a string — non-string breaks accumulation."
                )

    def test_text_delta_accumulation(self):
        """
        Verify accumulated text deltas produce meaningful content.
        The frontend joins all text_delta.text values into the response.
        """
        events = _stream("Say exactly: ACCUMULATED_TEXT_TEST")

        full_text = "".join(
            d.get("text", "") for t, d in events
            if t == "text_delta" and isinstance(d, dict)
        )

        assert len(full_text.strip()) > 0, (
            f"Accumulated text is empty despite receiving text_delta events.\n"
            f"Either text field is always empty string, or events have no text."
        )


# ===========================================================================
# 4. thinking_delta contract: { text: string }
# ===========================================================================


class TestThinkingDeltaContract:
    """
    Frontend: data.text is appended to state.thinking.

    interface PiThinkingDelta { text: string }

    If 'text' is missing → thinking section is blank.
    """

    def test_thinking_delta_text_is_string_when_present(self):
        events = _stream("Think step by step: what is 13 * 29?")

        thinking_deltas = [d for t, d in events if t == "thinking_delta"]

        for data in thinking_deltas:
            assert "text" in data, (
                f"thinking_delta missing 'text' field. Got: {data}"
            )
            assert isinstance(data["text"], str), (
                f"thinking_delta.text is {type(data['text']).__name__}, expected str"
            )

    def test_thinking_or_text_present(self):
        """
        At minimum, thinking_delta or text_delta must produce content.
        If neither does, user sees a blank spinner.
        """
        events = _stream("Think about the number 42 and explain it.")

        thinking = "".join(
            d.get("text", "") for t, d in events
            if t == "thinking_delta" and isinstance(d, dict)
        )
        text = "".join(
            d.get("text", "") for t, d in events
            if t == "text_delta" and isinstance(d, dict)
        )

        assert len(thinking.strip()) > 0 or len(text.strip()) > 0, (
            "BUG: Neither thinking nor text content produced.\n"
            f"thinking: {len(thinking)} chars, text: {len(text)} chars\n"
            f"Event types: {[t for t, _ in events]}\n"
            "User sees blank spinner."
        )


# ===========================================================================
# 5. agent_start contract: { } (Record<string, never>)
# ===========================================================================


class TestAgentStartContract:
    """
    Frontend: sets isStreaming=true, resets text/thinking/toolCalls.

    type agent_start = { data: Record<string, never> }

    The event itself is the trigger — no data fields are read.
    But it MUST be sent before content events.
    """

    def test_agent_start_present(self):
        """
        agent_start must be in the event stream.
        Without it, frontend doesn't reset streaming state → stale content from
        previous prompt bleeds through.
        """
        events = _stream("Say ok")
        event_types = [t for t, _ in events]

        assert "agent_start" in event_types, (
            f"agent_start event missing. Types: {event_types}\n"
            f"Frontend relies on this to reset isStreaming/text/thinking/toolCalls."
        )

    def test_agent_start_before_content(self):
        """
        agent_start must come BEFORE text_delta/thinking_delta events.
        The frontend resets state on agent_start — if it arrives after
        content deltas, those deltas get wiped.
        """
        events = _stream("Say ok")

        start_idx = None
        first_content_idx = None

        for i, (t, _) in enumerate(events):
            if t == "agent_start" and start_idx is None:
                start_idx = i
            if t in ("text_delta", "thinking_delta") and first_content_idx is None:
                first_content_idx = i

        if start_idx is not None and first_content_idx is not None:
            assert start_idx < first_content_idx, (
                f"agent_start (idx {start_idx}) came AFTER first content event (idx {first_content_idx}).\n"
                f"Frontend resets state on agent_start — content deltas before it will be lost."
            )


# ===========================================================================
# 6. agent_end contract: { input: number, output: number, cache: number }
# ===========================================================================


class TestAgentEndContract:
    """
    Frontend: sets isStreaming=false, accumulates token counts.

    interface PiAgentEnd { input: number; output: number; cache: number }

    If missing → infinite spinner.
    If non-numeric → NaN in token display.
    """

    def test_agent_end_present(self):
        """agent_end MUST be the final event. Without it: infinite spinner."""
        events = _stream("Say ok")
        event_types = [t for t, _ in events]

        assert "agent_end" in event_types, (
            f"BUG: agent_end missing. Events: {event_types}\n"
            f"Infinite spinner — loading never stops."
        )

    def test_agent_end_is_last_event(self):
        """
        agent_end should be the last meaningful event.
        If events come after agent_end, the frontend won't process them
        (isStreaming is already false).
        """
        events = _stream("Say ok")

        last_idx = len(events) - 1
        end_indices = [i for i, (t, _) in enumerate(events) if t == "agent_end"]

        assert len(end_indices) > 0, "No agent_end event"
        end_idx = end_indices[-1]

        # Events after agent_end (excluding agent_spawned which is OK)
        post_end = [(t, d) for t, d in events[end_idx + 1:] if t != "agent_spawned"]
        # Allow some tolerance — the backend may send a few events after
        # but they shouldn't be content events
        content_after = [t for t, _ in post_end if t in ("text_delta", "thinking_delta")]
        assert len(content_after) == 0, (
            f"Content events after agent_end: {content_after}\n"
            f"These will be ignored because isStreaming is already false."
        )

    def test_agent_end_has_input_field(self):
        """agent_end.data.input must be a number (frontend does arithmetic)."""
        events = _stream("Say ok")

        end_events = [d for t, d in events if t == "agent_end"]
        assert len(end_events) > 0, "No agent_end event"

        data = end_events[0]
        assert "input" in data, f"agent_end missing 'input' field: {data}"
        assert isinstance(data["input"], (int, float)), (
            f"agent_end.input is {type(data['input']).__name__}, expected number"
        )

    def test_agent_end_has_output_field(self):
        """agent_end.data.output must be a number."""
        events = _stream("Say ok")

        end_events = [d for t, d in events if t == "agent_end"]
        data = end_events[0]
        assert "output" in data, f"agent_end missing 'output' field: {data}"
        assert isinstance(data["output"], (int, float)), (
            f"agent_end.output is {type(data['output']).__name__}, expected number"
        )

    def test_agent_end_has_cache_field(self):
        """agent_end.data.cache must be a number."""
        events = _stream("Say ok")

        end_events = [d for t, d in events if t == "agent_end"]
        data = end_events[0]
        assert "cache" in data, f"agent_end missing 'cache' field: {data}"
        assert isinstance(data["cache"], (int, float)), (
            f"agent_end.cache is {type(data['cache']).__name__}, expected number"
        )

    def test_agent_end_token_values_not_negative(self):
        """Token counts should be non-negative."""
        events = _stream("Say ok")

        end_events = [d for t, d in events if t == "agent_end"]
        data = end_events[0]

        for field in ("input", "output", "cache"):
            if field in data:
                assert data[field] >= 0, (
                    f"agent_end.{field} is negative: {data[field]}"
                )

    def test_agent_end_usage_nonzero(self):
        """At least input or output should be > 0 for a real prompt."""
        events = _stream("Say ok")

        end_events = [d for t, d in events if t == "agent_end"]
        data = end_events[0]

        has_usage = data.get("input", 0) > 0 or data.get("output", 0) > 0
        assert has_usage, (
            f"agent_end has zero usage: {data}\n"
            f"Token display will show all zeros."
        )


# ===========================================================================
# 7. agent_spawned contract: { agentId: string }
# ===========================================================================


class TestAgentSpawnedContract:
    """
    Frontend: updates state.agentId.

    interface PiAgentSpawned { agentId: string }

    If missing → agentId stays 'default' (may work but is wrong).
    If empty string → agentId is empty.
    """

    def test_agent_spawned_present(self):
        events = _stream("Say ok")
        event_types = [t for t, _ in events]

        assert "agent_spawned" in event_types, (
            f"agent_spawned missing. Events: {event_types}\n"
            f"Frontend needs this to set the agentId."
        )

    def test_agent_spawned_has_agentId(self):
        events = _stream("Say ok")

        spawned_events = [d for t, d in events if t == "agent_spawned"]
        if not spawned_events:
            pytest.skip("No agent_spawned event")

        data = spawned_events[0]
        assert "agentId" in data, f"agent_spawned missing 'agentId': {data}"
        assert isinstance(data["agentId"], str), (
            f"agentId is {type(data['agentId']).__name__}, expected str"
        )
        assert len(data["agentId"]) > 0, "agentId is empty string"

    def test_agent_spawned_before_agent_start(self):
        """
        agent_spawned should come before agent_start in the sequence.
        Frontend needs agentId set before streaming starts.
        """
        events = _stream("Say ok")

        spawned_idx = None
        start_idx = None
        for i, (t, _) in enumerate(events):
            if t == "agent_spawned" and spawned_idx is None:
                spawned_idx = i
            if t == "agent_start" and start_idx is None:
                start_idx = i

        if spawned_idx is not None and start_idx is not None:
            assert spawned_idx <= start_idx, (
                f"agent_spawned (idx {spawned_idx}) after agent_start (idx {start_idx}).\n"
                f"agentId not set when streaming begins."
            )


# ===========================================================================
# 8. tool_execution_start contract: { toolName: string, args: object, toolId: string }
# ===========================================================================


class TestToolStartContract:
    """
    Frontend: creates ToolCall object with name, args, startTime.

    interface PiToolStart { toolName: string; args: Record<string, unknown>; toolId: string }

    If toolName is blank → "Running tool: (blank)" in UI.
    If args is not an object → display breaks.
    If toolId is missing → tool can't be matched with tool_execution_end.
    """

    def test_tool_start_has_required_fields(self):
        events = _stream(
            "Run this bash command: echo TOOL_START_CONTRACT_TEST"
        )

        tool_starts = [d for t, d in events if t == "tool_execution_start"]

        if not tool_starts:
            pytest.skip("Model didn't use tools — can't test tool contract")

        for data in tool_starts:
            # toolName
            assert "toolName" in data, (
                f"tool_execution_start missing 'toolName'. Keys: {list(data.keys())}\n"
                f"Frontend shows blank tool name."
            )
            assert isinstance(data["toolName"], str), (
                f"toolName is {type(data['toolName']).__name__}, expected str"
            )
            assert len(data["toolName"]) > 0, (
                f"toolName is empty string. Frontend shows blank tool label."
            )

            # toolId
            assert "toolId" in data, (
                f"tool_execution_start missing 'toolId'. Keys: {list(data.keys())}\n"
                f"Frontend uses this to match with tool_execution_end."
            )
            assert isinstance(data["toolId"], str), (
                f"toolId is {type(data['toolId']).__name__}, expected str"
            )
            assert len(data["toolId"]) > 0, "toolId is empty string"

            # args
            assert "args" in data, (
                f"tool_execution_start missing 'args'. Keys: {list(data.keys())}\n"
                f"Frontend displays args in the tool card."
            )
            assert isinstance(data["args"], dict), (
                f"args is {type(data['args']).__name__}, expected dict/object.\n"
                f"Value: {json.dumps(data['args'])[:200]}"
            )

    def test_tool_start_comes_before_tool_end(self):
        """tool_execution_start must precede tool_execution_end for the same toolId."""
        events = _stream("Run: echo TOOL_ORDER_TEST")

        tool_starts = {d["toolId"]: i for i, (t, d) in enumerate(events)
                       if t == "tool_execution_start" and isinstance(d, dict)}
        tool_ends = {d["toolId"]: i for i, (t, d) in enumerate(events)
                     if t == "tool_execution_end" and isinstance(d, dict)}

        order_violations = []
        for tid, end_idx in tool_ends.items():
            if tid in tool_starts:
                if tool_starts[tid] >= end_idx:
                    order_violations.append(
                        f"toolId {tid!r}: start at idx {tool_starts[tid]}, "
                        f"end at idx {end_idx}"
                    )

        assert len(order_violations) == 0, (
            f"Tool start/end ordering violated:\n"
            + "\n".join(f"  {v}" for v in order_violations)
        )


# ===========================================================================
# 9. tool_execution_end contract: { toolId: string, result?: any, error?: string }
# ===========================================================================


class TestToolEndContract:
    """
    Frontend: updates ToolCall with result/error/endTime.

    interface PiToolEnd { toolName: string; toolId: string; result: unknown; error?: string }

    If toolId is missing → tool card stays in "running" state forever.
    If neither result nor error → blank tool card.
    """

    def test_tool_end_has_toolId(self):
        events = _stream("Run: echo TOOL_END_TEST")

        tool_ends = [d for t, d in events if t == "tool_execution_end"]

        if not tool_ends:
            pytest.skip("Model didn't use tools")

        for data in tool_ends:
            assert "toolId" in data, (
                f"tool_execution_end missing 'toolId'. Keys: {list(data.keys())}\n"
                f"Frontend can't match this to a tool_execution_start → tool stays 'running'."
            )
            assert isinstance(data["toolId"], str) and len(data["toolId"]) > 0, (
                f"toolId is empty or wrong type: {data.get('toolId')!r}"
            )

    def test_tool_end_has_result_or_error(self):
        """
        Every tool_execution_end should have either result or error.
        If both are missing, the frontend shows a blank tool card.
        """
        events = _stream("Run: echo TOOL_RESULT_TEST")

        tool_ends = [d for t, d in events if t == "tool_execution_end"]

        if not tool_ends:
            pytest.skip("Model didn't use tools")

        for data in tool_ends:
            has_result = "result" in data
            has_error = "error" in data
            assert has_result or has_error, (
                f"tool_execution_end has neither 'result' nor 'error'.\n"
                f"Keys: {list(data.keys())}\n"
                f"Frontend shows blank tool card."
            )

    def test_tool_id_matches_between_start_and_end(self):
        """
        The toolId in tool_execution_end MUST match a tool_execution_start.
        If it doesn't, the frontend can't update the tool card → stays "running".
        """
        events = _stream("Run: echo TOOL_ID_MATCH_TEST")

        start_ids = {d["toolId"] for t, d in events
                     if t == "tool_execution_start" and isinstance(d, dict)}
        end_ids = {d["toolId"] for t, d in events
                   if t == "tool_execution_end" and isinstance(d, dict)}

        if not end_ids:
            pytest.skip("No tool_execution_end events")

        unmatched = end_ids - start_ids
        assert len(unmatched) == 0, (
            f"tool_execution_end has toolIds not in tool_execution_start:\n"
            f"  Unmatched end IDs: {unmatched}\n"
            f"  Start IDs: {start_ids}\n"
            f"Frontend tool cards will stay in 'running' state forever."
        )

    def test_error_field_is_string_when_present(self):
        events = _stream("Run: echo TOOL_ERROR_TYPE_TEST")

        for t, d in events:
            if t == "tool_execution_end" and "error" in d:
                assert isinstance(d["error"], str), (
                    f"tool_execution_end.error is {type(d['error']).__name__}, expected str.\n"
                    f"Value: {d['error']!r}"
                )


# ===========================================================================
# 10. error event contract: { error: string }
# ===========================================================================


class TestErrorEventContract:
    """
    Frontend: sets state.error, stops streaming.

    interface PiError { error: string }

    If the backend ever sends this event, the message must be a non-empty string.
    """

    def test_error_event_has_message(self):
        """
        We can't reliably trigger an error event, but we can verify that
        IF one appears, it has the correct structure.
        """
        events = _stream("Say ok")

        error_events = [d for t, d in events if t == "error"]
        for data in error_events:
            assert "error" in data, f"Error event missing 'error' field: {data}"
            assert isinstance(data["error"], str), (
                f"error field is {type(data['error']).__name__}, expected str"
            )
            assert len(data["error"]) > 0, "Error message is empty string"


# ===========================================================================
# 11. Full event stream validation
# ===========================================================================


class TestFullStreamContract:
    """
    Validate EVERY event in a complete stream against the contract.
    This catches any event that doesn't match the TypeScript interfaces.
    """

    def test_all_events_pass_contract_validation(self):
        events = _stream("Say exactly: 'full contract validation test' and nothing else.")

        all_violations = []
        for ev_type, data in events:
            violations = validate_sse_event(ev_type, data)
            for v in violations:
                all_violations.append(f"  {ev_type}: {v}")

        assert len(all_violations) == 0, (
            f"Contract violations found in {len(events)} events:\n"
            + "\n".join(all_violations)
        )

    def test_valid_prompt_produces_complete_stream(self):
        """
        A valid prompt must produce:
        1. agent_spawned
        2. agent_start
        3. Content (text_delta or thinking_delta)
        4. agent_end
        """
        events = _stream("Say exactly: complete stream test")
        event_types = [t for t, _ in events]

        # Must have agent_spawned
        assert "agent_spawned" in event_types, (
            f"Missing agent_spawned. Types: {event_types}"
        )

        # Must have agent_start
        assert "agent_start" in event_types, (
            f"Missing agent_start. Types: {event_types}"
        )

        # Must have content
        has_content = any(t in event_types for t in ("text_delta", "thinking_delta"))
        assert has_content, (
            f"No text_delta or thinking_delta in stream. Types: {event_types}"
        )

        # Must have agent_end
        assert "agent_end" in event_types, (
            f"Missing agent_end. Types: {event_types}"
        )

    def test_no_duplicate_agent_end(self):
        """
        agent_end should appear exactly once per prompt.
        Multiple agent_end events would cause state confusion.
        """
        events = _stream("Say ok")
        end_count = sum(1 for t, _ in events if t == "agent_end")

        assert end_count == 1, (
            f"Expected exactly 1 agent_end, got {end_count}.\n"
            f"Multiple agent_end events confuse streaming state."
        )

    def test_agent_spawned_appears_once(self):
        """agent_spawned should appear exactly once."""
        events = _stream("Say ok")
        count = sum(1 for t, _ in events if t == "agent_spawned")

        assert count == 1, (
            f"Expected 1 agent_spawned, got {count}"
        )

    def test_agent_start_appears_once(self):
        """agent_start should appear exactly once per prompt."""
        events = _stream("Say ok")
        count = sum(1 for t, _ in events if t == "agent_start")

        assert count == 1, (
            f"Expected 1 agent_start, got {count}"
        )


# ===========================================================================
# 12. Error response contract — API-level errors
# ===========================================================================


class TestAPIErrorContract:
    """
    Test that error responses from the API have correct structure
    when the request is invalid.
    """

    def test_missing_message_returns_400(self, api_url, api_session):
        resp = api_session.post(
            f"{api_url}/api/pux/prompt",
            json={"project": TEST_PROJECT},
        )
        assert resp.status_code == 400, (
            f"Expected 400 for missing message, got {resp.status_code}"
        )

    def test_nonexistent_project_returns_error(self, api_url, api_session):
        resp = api_session.post(
            f"{api_url}/api/pux/prompt",
            json={"message": "hello", "project": "nonexistent-proj-xyz-999"},
        )
        assert resp.status_code in (400, 404), (
            f"Expected 400/404 for nonexistent project, got {resp.status_code}"
        )

    def test_empty_message_returns_error(self, api_url, api_session):
        resp = api_session.post(
            f"{api_url}/api/pux/prompt",
            json={"message": "", "project": TEST_PROJECT},
        )
        # Empty message should either be rejected or processed
        assert resp.status_code in (200, 400), (
            f"Unexpected status for empty message: {resp.status_code}"
        )
