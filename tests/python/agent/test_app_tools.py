"""Test that app_deep_research is callable from an agent session."""
import json
import sys
import time

import requests

from utils.sse import post_and_stream

API = "http://localhost:3847"


def main():
    # Use the existing deep-research-engine sandbox
    project = "deep-research-engine"

    print(f"Sending prompt to project '{project}'...")
    print("The prompt asks the agent to call app_deep_research directly.")
    print("-" * 60)

    events = list(post_and_stream(
        requests.Session(),
        f"{API}/api/pux/prompt",
        {
            "message": 'Call the tool app_deep_research with query "test query". Just call it once and report what happened.',
            "project": project,
            "agentId": "app-tool-test",
        },
        timeout=120,
    ))

    print(f"\nReceived {len(events)} events\n")

    tool_calls_found = []
    text_output = []
    app_tool_results = []

    for evt_type, data in events:
        if evt_type == "tool_call":
            tool_name = data.get("tool", data.get("name", ""))
            args = data.get("args", data.get("arguments", {}))
            tool_calls_found.append(tool_name)
            print(f"[TOOL CALL] {tool_name}({json.dumps(args)[:120]})")
        elif evt_type == "tool_result":
            tool_name = data.get("tool", "")
            output = str(data.get("output", data.get("result", "")))[:200]
            if tool_name.startswith("app_"):
                app_tool_results.append(data)
            print(f"[TOOL RESULT] {tool_name}: {output[:150]}...")
        elif evt_type == "text_delta":
            text = data.get("text", data.get("content", ""))
            if text:
                text_output.append(text)
        elif evt_type == "error":
            print(f"[ERROR] {data}")
        elif evt_type in ("done", "complete"):
            print(f"[{evt_type.upper()}] {data}")

    print("\n" + "=" * 60)
    print("SUMMARY")
    print("=" * 60)
    print(f"Tool calls: {tool_calls_found}")
    print(f"App tool results: {len(app_tool_results)}")
    print(f"Text output length: {sum(len(t) for t in text_output)} chars")

    # Check if app_deep_research was called
    if "app_deep_research" in tool_calls_found:
        print("\n✓ app_deep_research was called by the agent!")
    elif any("app_" in tc for tc in tool_calls_found):
        print(f"\n✓ An app tool was called: {[tc for tc in tool_calls_found if 'app_' in tc]}")
    else:
        print(f"\n✗ app_deep_research was NOT called. Tools used: {tool_calls_found}")

    if text_output:
        full_text = "".join(text_output)
        print(f"\nAgent response (first 500 chars):\n{full_text[:500]}")


if __name__ == "__main__":
    main()
