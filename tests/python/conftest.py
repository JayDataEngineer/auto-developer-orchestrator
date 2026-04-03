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
        _SERVICES_AVAILABLE["browser"] = _probe(
            f"{API_BASE_URL}/api/pi/web/session", method="POST", expect_lt=400
        )

    skip_map = {
        "api": "API server unreachable",
        "sse": "API server unreachable",
        "sandbox": "Sandbox service unreachable",
        "browser": "Web browser service unreachable",
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
# Playwright config — pytest-playwright handles browser/page fixtures natively.
# Use `pytest --headed` to run with visible browser during development.
# ---------------------------------------------------------------------------
