#!/usr/bin/env python3
"""Smoke test the running Pux MCP server end-to-end.

Drives the full MCP contract: initialize, tools/list, then exercises each
registered tool with a real call. Verifies the round-trip works against a
live Docker sandbox.
"""
import json
import os
import sys
import urllib.request

ADDR = os.environ.get("PUX_MCP_ADDR", "127.0.0.1:19876")
URL = f"http://{ADDR}"
SESSION = None


def call(method, params=None, *, is_notification=False):
    """Send one JSON-RPC request. Returns the parsed result dict."""
    global SESSION
    req_id = None if is_notification else call._next
    if not is_notification:
        call._next += 1
    envelope = {
        "jsonrpc": "2.0",
        "id": req_id,
        "method": method,
    }
    if params is not None:
        envelope["params"] = params
    body = json.dumps(envelope).encode("utf-8")

    r = urllib.request.Request(URL, data=body, method="POST")
    r.add_header("Content-Type", "application/json")
    if SESSION:
        r.add_header("Mcp-Session-Id", SESSION)
    with urllib.request.urlopen(r) as resp:
        if SESSION is None and resp.headers.get("Mcp-Session-Id"):
            SESSION = resp.headers["Mcp-Session-Id"]
            print(f"[session] {SESSION}")
        if is_notification:
            return None
        return json.loads(resp.read())


call._next = 1


def call_tool(name, args):
    """Invoke a tool and return (text, is_error)."""
    result = call("tools/call", {"name": name, "arguments": args})
    if "error" in result:
        return json.dumps(result["error"]), True
    content = result["result"]["content"]
    return content[0]["text"], result["result"].get("isError", False)


def expect(cond, label):
    if cond:
        print(f"  PASS  {label}")
    else:
        print(f"  FAIL  {label}")
        sys.exit(1)


def main():
    print("\n=== MCP smoke test ===\n")

    # 1. initialize
    init = call("initialize", {
        "protocolVersion": "2025-03-26",
        "clientInfo": {"name": "smoke-test", "version": "0.1"},
    })
    expect(init["result"]["protocolVersion"] == "2025-03-26",
           f"protocol version handshake (got {init['result']['protocolVersion']})")
    expect(init["result"]["serverInfo"]["name"] == "pux-mcp",
           "server name in init response")

    # notifications/initialized
    call("notifications/initialized", is_notification=True)
    print("  PASS  notifications/initialized (no response expected)")

    # 2. tools/list
    listed = call("tools/list", {})
    names = [t["name"] for t in listed["result"]["tools"]]
    print(f"\n  advertised tools: {names}")
    expected = {"bash", "file_read", "file_write", "file_edit", "file_grep", "file_glob",
                "python", "list_skills", "load_skill"}
    missing = expected - set(names)
    expect(not missing, f"all expected tools advertised (missing: {missing})")

    # 3. bash echo
    text, err = call_tool("bash", {"command": "echo hello-from-bash"})
    expect(not err, f"bash echo succeeds (got err={err})")
    expect("hello-from-bash" in text, f"bash echo output correct (got: {text!r})")

    # 4. bash: confirm we're really in a container
    text, err = call_tool("bash", {"command": "cat /etc/os-release | head -1"})
    expect(not err, "container probe: bash succeeded")
    expect("PRETTY_NAME" in text, f"container file readable (got: {text!r})")

    # 5. file_read: confirm project mount is visible
    text, err = call_tool("file_read", {"file_path": "/sandbox/workspace/test.txt"})
    expect(not err, f"file_read on mounted file succeeds (err={err})")
    expect("hello world" in text, f"file_read content correct (got: {text!r})")

    # 6. file_write + file_read roundtrip
    text, err = call_tool("file_write", {"file_path": "/sandbox/workspace/from-mcp.txt",
                                          "content": "written by mcp client",
                                          "overwrite": True})
    expect(not err, f"file_write succeeds (err={err})")
    text, err = call_tool("file_read", {"file_path": "/sandbox/workspace/from-mcp.txt"})
    expect(not err and "written by mcp client" in text,
           f"file_read returns what we wrote (got: {text!r})")

    # 7. python tool: confirm sandbox-installed python works
    text, err = call_tool("python", {"code": "print(sum(range(10)))"})
    expect(not err, f"python tool succeeded (err={err})")
    expect("45" in text, f"python output correct (got: {text!r})")

    # 8. unknown tool: should error with method-not-found
    text, err = call_tool("nope", {})
    expect(err, "unknown tool returns isError=true")
    expect("unknown tool" in text.lower() or "method not found" in text.lower(),
           f"unknown tool error message correct (got: {text!r})")

    # 9. list_skills / load_skill roundtrip
    # Smoke workspace has skills/smoke-test/SKILL.md written before server boot.
    text, err = call_tool("list_skills", {})
    expect(not err, f"list_skills succeeds (err={err})")
    expect("smoke-test" in text, f"list_skills surfaces smoke-test skill (got: {text!r})")
    text, err = call_tool("load_skill", {"name": "smoke-test"})
    expect(not err, f"load_skill succeeds (err={err})")
    expect("Specialized guidance for the smoke test" in text,
           f"load_skill returns skill body (got: {text!r})")

    # 10. ping
    ping = call("ping", {})
    expect(ping.get("result") is not None and ping.get("error") is None,
           "ping returned empty result")

    print("\n=== ALL CHECKS PASSED ===\n")


if __name__ == "__main__":
    main()
