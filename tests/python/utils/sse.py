"""
SSE streaming helpers — extracted from conftest.py for reuse.
"""

import json


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
            if data_buf:
                try:
                    data = json.loads(data_buf)
                except json.JSONDecodeError:
                    data = {"raw": data_buf}
                yield (event_type or "message", data)
                event_type = None
                data_buf = ""
        else:
            data_buf += line

    if data_buf:
        try:
            data = json.loads(data_buf)
        except json.JSONDecodeError:
            data = {"raw": data_buf}
        yield (event_type or "message", data)


def stream_prompt(api_url, api_session, project, message,
                  agent_id="default", model="qwen-local-primary", timeout=120):
    """Send a prompt to a Pi agent and collect all SSE events."""
    return list(post_and_stream(
        api_session,
        f"{api_url}/api/pux/prompt",
        {
            "message": message,
            "project": project,
            "agentId": agent_id,
            "model": model,
        },
        timeout=timeout,
    ))


def stream_pux_prompt(api_url, api_session, project, message,
                      agent_id="default", timeout=300):
    """Send a prompt via the Pux agent endpoint and collect all SSE events."""
    return list(post_and_stream(
        api_session,
        f"{api_url}/api/pux/prompt",
        {
            "message": message,
            "project": project,
            "agentId": agent_id,
        },
        timeout=timeout,
    ))


def collect_events(events, event_type):
    """Filter SSE events by type. Returns list of data dicts."""
    return [data for et, data in events if et == event_type]


def collect_text(events):
    """Join all text_delta events into a single string."""
    return "".join(data.get("text", "") for et, data in events if et == "text_delta")
