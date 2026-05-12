"""Agent lifecycle fixtures — extracted from conftest.py."""

import pytest


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
