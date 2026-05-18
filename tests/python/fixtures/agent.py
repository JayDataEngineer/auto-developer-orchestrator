"""Agent lifecycle fixtures — extracted from conftest.py."""

import pytest


def spawn_agent(api_url, api_session, project, agent_id="default"):
    """Ensure a Pux agent session exists by checking agent-status.

    In the Pux architecture, agents are created implicitly when a prompt
    is sent. This fixture checks if an agent is already active; if not,
    it sends a lightweight prompt to create one.
    """
    # Check if agent already exists
    resp = api_session.get(
        f"{api_url}/api/pux/agent-status",
        params={"project": project},
        timeout=10,
    )
    if resp.status_code == 200:
        data = resp.json()
        agents = data.get("agents", []) if isinstance(data, dict) else []
        for a in agents:
            if isinstance(a, dict) and a.get("agentId", a.get("agent_id")) == agent_id:
                return agent_id

    # Agent doesn't exist — send a lightweight prompt to implicitly create it
    resp = api_session.post(
        f"{api_url}/api/pux/prompt",
        json={"message": "ping", "project": project, "agentId": agent_id},
        timeout=30,
        stream=True,
    )
    # Consume the response to avoid connection leaks
    if resp.status_code == 200:
        for _ in resp.iter_lines():
            break  # Just consume the first event and move on
        resp.close()

    return agent_id


def destroy_agent(api_url, api_session, project, agent_id="default"):
    """Clean up a Pux agent session.

    In the Pux architecture, agents are cleaned up by deleting their
    conversation data. Returns the response status code.
    """
    resp = api_session.delete(
        f"{api_url}/api/pux/conversation",
        params={"project": project, "agentId": agent_id},
        timeout=10,
    )
    return resp.status_code
