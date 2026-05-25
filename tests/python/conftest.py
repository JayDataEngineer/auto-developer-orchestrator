"""
Root conftest: session-scoped fixtures and service probe auto-skip.
Helper functions are in utils/ and fixtures/ packages.
"""

import os

import pytest
import requests
from requests.adapters import HTTPAdapter
from urllib3.util.retry import Retry

# URL configuration — importable by test files
API_BASE_URL = os.environ.get("API_BASE_URL", "http://localhost:3847")
FRONTEND_BASE_URL = os.environ.get("FRONTEND_BASE_URL", "http://localhost:5175")


# ---------------------------------------------------------------------------
# Session fixtures
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
    use that. Otherwise, pick the first existing project from the API.
    Falls back to 'deep-research-engine'.
    """
    env_project = os.environ.get("E2E_TEST_PROJECT")
    if env_project:
        return env_project

    resp = api_session.get(f"{api_url}/api/projects")
    if resp.status_code == 200:
        projects = resp.json().get("projects", [])
        if projects:
            # projects is a list of dicts with "name" key
            first = projects[0]
            return first["name"] if isinstance(first, dict) else first

    return "deep-research-engine"


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
        for sb in sandboxes:
            sb_id = sb.get("id", sb) if isinstance(sb, dict) else sb
            if sb_id == test_project or sb_id == f"sandbox-{test_project}":
                return sb_id
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
    """Auto-skip tests whose required services are unreachable, or when --skip-llm is set."""
    global _SERVICES_AVAILABLE

    skip_llm = config.getoption("--skip-llm", default=False)

    if not _SERVICES_AVAILABLE:
        _SERVICES_AVAILABLE["api"] = _probe(f"{API_BASE_URL}/api/health")
        _SERVICES_AVAILABLE["frontend"] = _probe(FRONTEND_BASE_URL)
        _SERVICES_AVAILABLE["sandbox"] = _probe(
            f"{API_BASE_URL}/api/sandbox", expect_lt=500
        )

    # Probe TUI visual server
    _SERVICES_AVAILABLE["tui_visual"] = _probe("http://localhost:9877/health")

    skip_map = {
        "api": "API server unreachable",
        "sse": "API server unreachable",
        "sandbox": "Sandbox service unreachable",
        "browser": "Sandbox service unreachable",
        "desktop": "Sandbox service unreachable",
        "agent": "API server unreachable",
        "playwright": "Frontend server unreachable",
        "tui": "TUI visual server unreachable (start: task tui-visual)",
    }

    for item in items:
        # Skip LLM-dependent tests when --skip-llm is passed
        if skip_llm:
            for marker in item.iter_markers():
                if marker.name == "llm":
                    item.add_marker(pytest.mark.skip(reason="--skip-llm flag provided"))
                    break

        for marker in item.iter_markers():
            marker_name = marker.name
            if marker_name in skip_map:
                service_key = marker_name
                if marker_name in ("sse", "agent"):
                    service_key = "api"
                elif marker_name in ("browser", "desktop"):
                    service_key = "sandbox"
                elif marker_name == "tui":
                    service_key = "tui_visual"
                if not _SERVICES_AVAILABLE.get(service_key, True):
                    reason = skip_map[marker_name]
                    item.add_marker(pytest.mark.skip(reason=reason))
                    break


def pytest_addoption(parser):
    parser.addoption("--skip-llm", action="store_true", default=False,
                     help="Skip tests that require LLM generation")
