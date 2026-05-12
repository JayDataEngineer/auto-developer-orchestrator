"""
SSE event contract validation — mirrors frontend TypeScript interfaces.

These types and validators are the single source of truth for the SSE contract.
If the backend violates them, the frontend will crash or silently drop data.
"""

# All valid SSE event types (mirrors src/lib/pi-events.ts)
VALID_SSE_EVENT_TYPES = {
    "text_delta", "thinking_delta",
    "tool_execution_start", "tool_execution_end",
    "agent_start", "agent_end", "agent_spawned",
    "compaction_start", "compaction_end",
    "error", "state_update",
    "branch_created", "commit_created", "push_complete", "pr_created",
    "web_update",
    "approval_request", "question_asked",
    # Orchestrator events
    "plan_created", "plan_updated",
    "artifact_created", "artifact_updated",
    "subagent_start", "subagent_end",
    # SSE protocol events
    "message",
}

# Required fields for each event type (mirrors PiSSEEvent discriminated union)
SSE_EVENT_REQUIRED_FIELDS = {
    "text_delta": {"text"},
    "thinking_delta": {"text"},
    "tool_execution_start": {"toolName", "args", "toolId"},
    "tool_execution_end": {"toolId"},
    "agent_start": set(),
    "agent_end": {"input", "output", "cache"},
    "agent_spawned": {"agentId"},
    "compaction_start": set(),
    "compaction_end": {"compactedMessages", "keptMessages"},
    "error": {"error"},
    "state_update": {"model", "input", "output", "cache"},
    "branch_created": {"branch"},
    "commit_created": {"message", "branch"},
    "push_complete": {"branch"},
    "pr_created": {"url", "number", "title"},
    "web_update": {"url", "title", "screenshot", "elements"},
    "approval_request": {"requestId", "type", "message", "risk"},
    "question_asked": {"requestId", "type", "message", "risk"},
    "plan_created": set(),
    "plan_updated": set(),
    "artifact_created": set(),
    "artifact_updated": set(),
    "subagent_start": set(),
    "subagent_end": set(),
    "message": set(),
}

# Type constraints for fields the frontend uses in arithmetic
SSE_NUMERIC_FIELDS = {
    "agent_end": {"input", "output", "cache"},
    "state_update": {"input", "output", "cache"},
    "pr_created": {"number"},
}

# Allowed values for enum-like fields
VALID_APPROVAL_TYPES = {"tool_confirm", "plan", "question"}
VALID_RISK_LEVELS = {"low", "medium", "high"}


def validate_sse_event(event_type, data):
    """
    Validate an SSE event against the frontend's TypeScript interface contract.

    Returns a list of contract violations (empty list = fully compliant).
    """
    violations = []

    if event_type not in VALID_SSE_EVENT_TYPES:
        violations.append(f"Unknown event type: {event_type!r}")
        return violations

    required = SSE_EVENT_REQUIRED_FIELDS.get(event_type, set())

    if not isinstance(data, dict):
        violations.append(f"Event data is not a dict: {type(data).__name__}")
        return violations

    for field in required:
        if field not in data:
            violations.append(f"Missing required field: {field!r}")

    numeric_fields = SSE_NUMERIC_FIELDS.get(event_type, set())
    for field in numeric_fields:
        if field in data and not isinstance(data[field], (int, float)):
            violations.append(
                f"Field {field!r} must be numeric, got {type(data[field]).__name__}: {data[field]!r}"
            )

    if event_type == "text_delta":
        if "text" in data and not isinstance(data["text"], str):
            violations.append(f"text_delta.text must be str, got {type(data['text']).__name__}")

    if event_type == "thinking_delta":
        if "text" in data and not isinstance(data["text"], str):
            violations.append(f"thinking_delta.text must be str, got {type(data['text']).__name__}")

    if event_type == "tool_execution_start":
        if "toolName" in data:
            if not isinstance(data["toolName"], str) or len(data["toolName"]) == 0:
                violations.append(f"toolName must be non-empty string, got: {data['toolName']!r}")
        if "toolId" in data:
            if not isinstance(data["toolId"], str) or len(data["toolId"]) == 0:
                violations.append(f"toolId must be non-empty string, got: {data['toolId']!r}")
        if "args" in data and not isinstance(data["args"], dict):
            violations.append(f"args must be dict/object, got {type(data['args']).__name__}")

    if event_type == "tool_execution_end":
        if "toolId" in data:
            if not isinstance(data["toolId"], str) or len(data["toolId"]) == 0:
                violations.append(f"toolId must be non-empty string, got: {data['toolId']!r}")
        if "result" not in data and "error" not in data:
            violations.append("tool_execution_end must have 'result' or 'error'")

    if event_type == "agent_spawned":
        if "agentId" in data and (not isinstance(data["agentId"], str) or len(data["agentId"]) == 0):
            violations.append(f"agentId must be non-empty string, got: {data['agentId']!r}")

    if event_type == "error":
        if "error" in data and not isinstance(data["error"], str):
            violations.append(f"error field must be str, got {type(data['error']).__name__}")

    if event_type == "branch_created":
        if "branch" in data and (not isinstance(data["branch"], str) or len(data["branch"]) == 0):
            violations.append(f"branch must be non-empty string, got: {data['branch']!r}")

    if event_type in ("approval_request", "question_asked"):
        if "type" in data and data["type"] not in VALID_APPROVAL_TYPES:
            violations.append(f"Invalid approval type: {data['type']!r}, expected one of {VALID_APPROVAL_TYPES}")
        if "risk" in data and data["risk"] not in VALID_RISK_LEVELS:
            violations.append(f"Invalid risk level: {data['risk']!r}, expected one of {VALID_RISK_LEVELS}")
        if "requestId" in data and (not isinstance(data["requestId"], str) or len(data["requestId"]) == 0):
            violations.append(f"requestId must be non-empty string, got: {data['requestId']!r}")
        if "message" in data and not isinstance(data["message"], str):
            violations.append(f"message must be str, got {type(data['message']).__name__}")

    return violations
