"""Composable test assertion helpers for SSE events and tool lifecycles."""

from .contract import validate_sse_event


def assert_no_contract_violations(events):
    """Assert all SSE events pass contract validation."""
    all_violations = []
    for event_type, data in events:
        violations = validate_sse_event(event_type, data)
        if violations:
            all_violations.append((event_type, violations))
    assert not all_violations, (
        "SSE contract violations:\n"
        + "\n".join(f"  {et}: {', '.join(v)}" for et, v in all_violations)
    )


def assert_tool_lifecycle(events):
    """Assert every tool_execution_start has a matching tool_execution_end."""
    starts = {data["toolId"]: data for et, data in events if et == "tool_execution_start"}
    ends = {data["toolId"]: data for et, data in events if et == "tool_execution_end"}
    missing = set(starts.keys()) - set(ends.keys())
    assert not missing, f"Tool calls without end events: {missing}"


def assert_agent_completed(events):
    """Assert the event stream contains at least one agent_end event."""
    ends = [data for et, data in events if et == "agent_end"]
    assert len(ends) >= 1, "No agent_end event found in stream"


def assert_event_received(events, event_type):
    """Assert at least one event of the given type was received."""
    matching = [data for et, data in events if et == event_type]
    assert len(matching) >= 1, f"No {event_type} events found"
    return matching
