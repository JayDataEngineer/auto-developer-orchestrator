"""
Shared fixtures, service probes, and SSE helper for the e2e test suite.
"""

import json
import os
import time

import pytest
import requests
from requests.adapters import HTTPAdapter
from urllib3.util.retry import Retry

# ---------------------------------------------------------------------------
# URL configuration
# ---------------------------------------------------------------------------

API_BASE_URL = os.environ.get("API_BASE_URL", "http://localhost:3847")
FRONTEND_BASE_URL = os.environ.get("FRONTEND_BASE_URL", "http://localhost:5174")


# ---------------------------------------------------------------------------
# Fixtures
# ---------------------------------------------------------------------------


@pytest.fixture(scope="session")
def api_url():
    return API_BASE_URL


@pytest.fixture(scope="session")
def frontend_url():
    return FRONTEND_BASE_URL


@pytest.fixture(scope="session")
def api_session():
    """requests.Session with retry logic for transient connection errors."""
    s = requests.Session()
    retry = Retry(total=3, backoff_factor=0.5, status_forcelist=[502, 503, 504])
    adapter = HTTPAdapter(max_retries=retry)
    s.mount("http://", adapter)
    s.mount("https://", adapter)
    s.headers.update({"Content-Type": "application/json"})
    return s


@pytest.fixture(scope="session")
def ensure_sandbox(api_url, api_session, test_project):
    """
    Ensure at least one sandbox exists for the test project.
    Creates one if none exist (idempotent).
    """
    resp = api_session.get(f"{api_url}/api/sandbox/", timeout=10)
    if resp.status_code == 200:
        data = resp.json()
        sandboxes = data if isinstance(data, list) else data.get("sandboxes", [])
        # Check if a sandbox for this project already exists
        for sb in sandboxes:
            sb_id = sb.get("id", sb) if isinstance(sb, dict) else sb
            if sb_id == test_project or sb_id == f"sandbox-{test_project}":
                return sb_id
        # No sandbox for this project — create one
        if len(sandboxes) == 0:
            create_resp = api_session.post(
                f"{api_url}/api/sandbox/",
                json={
                    "id": test_project,
                    "project_path": f"/app/projects/{test_project}",
                    "policy": "developer",
                },
                timeout=120,
            )
            if create_resp.status_code in (200, 201):
                return test_project
    return None


@pytest.fixture(scope="session")
def test_project(api_url, api_session):
    """
    Provide a project name for tests. If E2E_TEST_PROJECT env var is set,
    use that (must be an already-registered project). Otherwise, pick the
    first existing project from the API. Falls back to 'deep-research-engine'.
    """
    # Allow override via env var
    env_project = os.environ.get("E2E_TEST_PROJECT")
    if env_project:
        return env_project

    # List existing projects and pick the first one
    resp = api_session.get(f"{api_url}/api/projects")
    if resp.status_code == 200:
        projects = resp.json().get("projects", [])
        if projects:
            return projects[0]

    return "deep-research-engine"


# ---------------------------------------------------------------------------
# SSE streaming helper
# ---------------------------------------------------------------------------


def post_and_stream(session, url, payload, timeout=120):
    """
    POST *payload* to *url* with ``stream=True`` and yield
    ``(event_type, data_dict)`` tuples parsed from the SSE response.

    Mirrors the frontend ``parseSSEEvent`` logic.
    """
    resp = session.post(url, json=payload, stream=True, timeout=timeout)
    resp.raise_for_status()

    event_type = None
    data_buf = ""

    for raw_line in resp.iter_lines(decode_unicode=True):
        if raw_line is None:
            continue
        line = raw_line.strip()

        if line.startswith("event:"):
            event_type = line[len("event:"):].strip()
        elif line.startswith("data:"):
            data_buf += line[len("data:"):].strip()
        elif line == "":
            # End of event — emit if we have data
            if data_buf:
                try:
                    data = json.loads(data_buf)
                except json.JSONDecodeError:
                    data = {"raw": data_buf}
                yield (event_type or "message", data)
                event_type = None
                data_buf = ""
        else:
            # Accumulate multi-line data
            data_buf += line

    # Flush any trailing data
    if data_buf:
        try:
            data = json.loads(data_buf)
        except json.JSONDecodeError:
            data = {"raw": data_buf}
        yield (event_type or "message", data)


# ---------------------------------------------------------------------------
# Service availability probes + auto-skip
# ---------------------------------------------------------------------------

_SERVICES_AVAILABLE = {}


def _probe(url, method="GET", json_body=None, timeout=5, expect_lt=400):
    """Return True if the endpoint responds within *timeout* seconds."""
    try:
        if method == "GET":
            r = requests.get(url, timeout=timeout)
        else:
            r = requests.post(url, json=json_body or {}, timeout=timeout)
        return r.status_code < expect_lt
    except Exception:
        return False


def pytest_collection_modifyitems(config, items):
    """Auto-skip tests whose required services are unreachable."""
    global _SERVICES_AVAILABLE

    # Probe each service once
    if not _SERVICES_AVAILABLE:
        _SERVICES_AVAILABLE["api"] = _probe(f"{API_BASE_URL}/api/health")
        _SERVICES_AVAILABLE["frontend"] = _probe(FRONTEND_BASE_URL)
        _SERVICES_AVAILABLE["sandbox"] = _probe(f"{API_BASE_URL}/api/sandbox", expect_lt=500)

    skip_map = {
        "api": "API server unreachable",
        "sse": "API server unreachable",
        "sandbox": "Sandbox service unreachable",
        "playwright": "Frontend server unreachable",
    }

    for item in items:
        for marker in item.iter_markers():
            marker_name = marker.name
            if marker_name in skip_map and not _SERVICES_AVAILABLE.get(marker_name, True):
                # For sse marker, check api availability
                service_key = "api" if marker_name == "sse" else marker_name
                if not _SERVICES_AVAILABLE.get(service_key, True):
                    reason = skip_map[marker_name]
                    item.add_marker(pytest.mark.skip(reason=reason))
                    break


# ---------------------------------------------------------------------------
# SSE event contract — TypeScript interface mirror
# ---------------------------------------------------------------------------

# These sets mirror the TypeScript types in src/lib/pi-events.ts exactly.
# Every field listed here is read by the frontend reducer (agentReducer.ts)
# and MUST be present in the backend's SSE output.

VALID_SSE_EVENT_TYPES = {
    "text_delta", "thinking_delta",
    "tool_execution_start", "tool_execution_end",
    "agent_start", "agent_end", "agent_spawned",
    "compaction_start", "compaction_end",
    "error", "state_update",
    "branch_created", "commit_created", "push_complete", "pr_created",
    "web_update",
    "approval_request", "question_asked",
}

# Required fields for each event type (mirrors PiSSEEvent discriminated union)
SSE_EVENT_REQUIRED_FIELDS = {
    "text_delta": {"text"},
    "thinking_delta": {"text"},
    "tool_execution_start": {"toolName", "args", "toolId"},
    "tool_execution_end": {"toolId"},  # result or error expected in practice
    "agent_start": set(),              # data: Record<string, never>
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
}

# Type constraints for fields the frontend uses in arithmetic or comparison
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

    # 1. Event type must be known
    if event_type not in VALID_SSE_EVENT_TYPES:
        violations.append(f"Unknown event type: {event_type!r}")
        return violations

    required = SSE_EVENT_REQUIRED_FIELDS.get(event_type, set())

    # 2. data must be a dict (except for agent_start/compaction_start which can be empty)
    if not isinstance(data, dict):
        violations.append(f"Event data is not a dict: {type(data).__name__}")
        return violations

    # 3. All required fields must be present
    for field in required:
        if field not in data:
            violations.append(f"Missing required field: {field!r}")

    # 4. Numeric fields must be numbers (frontend does arithmetic on them)
    numeric_fields = SSE_NUMERIC_FIELDS.get(event_type, set())
    for field in numeric_fields:
        if field in data and not isinstance(data[field], (int, float)):
            violations.append(
                f"Field {field!r} must be numeric, got {type(data[field]).__name__}: {data[field]!r}"
            )

    # 5. Specific field type checks
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
        # result or error should be present (frontend checks both)
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


# ---------------------------------------------------------------------------
# Agent lifecycle helpers
# ---------------------------------------------------------------------------


def spawn_agent(api_url, api_session, project, agent_id="default"):
    """Spawn a Pi agent and return the agent ID. Skips on max agents."""
    resp = api_session.post(
        f"{api_url}/api/pi/agent/spawn",
        json={"project": project, "agentId": agent_id},
        timeout=30,
    )
    data = resp.json()
    if data.get("error", "").startswith("max agents"):
        pytest.skip("Max agents reached")
    assert resp.status_code == 200, f"Spawn failed ({resp.status_code}): {data}"
    return data.get("agentId") or data.get("agent_id") or agent_id


def destroy_agent(api_url, api_session, project, agent_id="default"):
    """Destroy a Pi agent. Returns the response status code."""
    resp = api_session.post(
        f"{api_url}/api/pi/agent/destroy",
        json={"project": project, "agentId": agent_id},
        timeout=30,
    )
    return resp.status_code


def stream_prompt(api_url, api_session, project, message,
                  agent_id="default", model="qwen-local-primary", timeout=120):
    """
    Send a prompt to a Pi agent and collect all SSE events.

    Returns list of (event_type, data_dict) tuples.
    Spawns an agent if needed, but does NOT destroy it afterward
    (caller manages lifecycle).
    """
    return list(post_and_stream(
        api_session,
        f"{api_url}/api/pi/prompt",
        {
            "message": message,
            "project": project,
            "agentId": agent_id,
            "model": model,
        },
        timeout=timeout,
    ))


# ---------------------------------------------------------------------------
# Playwright config — pytest-playwright handles browser/page fixtures natively.
# Use `pytest --headed` to run with visible browser during development.
# ---------------------------------------------------------------------------
