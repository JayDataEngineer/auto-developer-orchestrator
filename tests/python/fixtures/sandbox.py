"""Sandbox fixtures: create, cleanup, wait-for-ready helpers."""

import time


def create_sandbox(api_url, api_session, sandbox_id, project_path="/tmp",
                   policy="developer", timeout=120):
    """Create a sandbox. Returns the sandbox ID. Raises on failure."""
    resp = api_session.post(
        f"{api_url}/api/sandbox/",
        json={"id": sandbox_id, "project_path": project_path, "policy": policy},
        timeout=timeout,
    )
    assert resp.status_code in (200, 201), (
        f"Create sandbox failed ({resp.status_code}): {resp.text[:500]}"
    )
    return sandbox_id


def cleanup_sandbox(api_url, api_session, sandbox_id):
    """Best-effort cleanup of a sandbox (disable + delete)."""
    try:
        api_session.post(
            f"{api_url}/api/sandbox/{sandbox_id}/computer-use/disable",
            timeout=10,
        )
    except Exception:
        pass
    try:
        api_session.delete(
            f"{api_url}/api/sandbox/{sandbox_id}",
            timeout=10,
        )
    except Exception:
        pass


def wait_for_desktop_ready(api_url, api_session, sandbox_id, timeout=120):
    """Enable desktop mode and wait until the X11 endpoint responds."""
    api_session.post(
        f"{api_url}/api/sandbox/{sandbox_id}/computer-use/enable",
        timeout=timeout,
    )
    deadline = time.time() + timeout
    while time.time() < deadline:
        try:
            resp = api_session.get(
                f"{api_url}/api/sandbox/{sandbox_id}/x11/screenshot",
                params={"format": "json"}, timeout=10,
            )
            if resp.status_code == 200:
                return True
        except Exception:
            pass
        time.sleep(2)
    raise TimeoutError(f"Desktop not ready after {timeout}s for sandbox {sandbox_id}")


def wait_for_browser_ready(api_url, api_session, sandbox_id, timeout=120):
    """Enable browser mode and wait until CDP screenshot works."""
    api_session.post(
        f"{api_url}/api/sandbox/{sandbox_id}/computer-use/enable",
        timeout=timeout,
    )
    deadline = time.time() + timeout
    while time.time() < deadline:
        try:
            resp = api_session.get(
                f"{api_url}/api/sandbox/{sandbox_id}/computer-use/screenshot",
                params={"describe": "false"}, timeout=10,
            )
            if resp.status_code == 200:
                return True
        except Exception:
            pass
        time.sleep(2)
    raise TimeoutError(f"Browser not ready after {timeout}s for sandbox {sandbox_id}")
