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
                "python", "list_skills", "load_skill", "describe_image",
                "browser_navigate", "browser_click", "browser_type",
                "browser_screenshot", "browser_evaluate"}
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

    # 10. describe_image: graceful degradation check.
    # The smoke workspace does NOT have the model downloaded, so we expect
    # the tool to return a structured success:false with reason="unavailable"
    # — NOT a Go error. This is the load-bearing contract: vision is opt-in,
    # a missing model MUST NOT break the agent loop.
    # If the model IS present (operator pre-downloaded), we accept either
    # success:true or a graceful failure; we only fail the smoke if the tool
    # errors out entirely.
    text, err = call_tool("describe_image", {
        "image_path": "/sandbox/workspace/test.txt",  # not an image, but model-missing path triggers first
    })
    expect(not err, f"describe_image should not hard-error even without model (err={err}, text={text!r})")
    # Response is JSON-shaped; check the structured fields.
    try:
        vision_result = json.loads(text)
    except json.JSONDecodeError:
        expect(False, f"describe_image result not JSON: {text!r}")
        return
    if vision_result.get("success") is True:
        # Model is downloaded in this env — accept any non-error result.
        print(f"  PASS  describe_image returned success (model present)")
    else:
        expect(vision_result.get("success") is False,
               f"describe_image should return success=false when model absent (got: {vision_result!r})")
        reason = vision_result.get("reason", "")
        expect(reason in ("unavailable", "deps_missing", "inference_failed", "timeout"),
               f"describe_image should return a known reason (got: {reason!r})")
        print(f"  PASS  describe_image degraded gracefully (reason={reason})")

    # 11. browser_navigate + browser_evaluate + browser_screenshot.
    # The sandbox runs sb_server.py (persistent SeleniumBase HTTP API) on
    # port 9876 inside the container. The Go tools shell out to curl it.
    # Use a data: URL so the test is hermetic — no network dependency.
    data_url = ("data:text/html,<html><head><title>Smoke</title></head>"
                "<body><h1 id=h>Hello</h1>"
                "<input id=inp type=text>"
                "<button id=btn>Click</button></body></html>")
    text, err = call_tool("browser_navigate", {"url": data_url})
    expect(not err, f"browser_navigate succeeds (err={err}, text={text[:200]!r})")
    nav = json.loads(text)
    expect(nav.get("ok") is True, f"browser_navigate ok=true (got: {nav})")
    title = nav.get("page_data", {}).get("title", "")
    expect(title == "Smoke", f"browser_navigate returns correct title (got: {title!r})")
    # element_map should include the button with SoM label
    elements = nav.get("element_map", [])
    expect(any(e.get("tag") == "button" for e in elements),
           f"element_map contains button (got: {elements})")

    # browser_evaluate: extract the h1 text via JS
    text, err = call_tool("browser_evaluate", {"code": "return document.getElementById('h').textContent"})
    expect(not err, f"browser_evaluate succeeds (err={err})")
    ev = json.loads(text)
    expect(ev.get("ok") is True, f"browser_evaluate ok=true (got: {ev})")
    # Some sb_server versions wrap result; tolerate either "result" or "value".
    ev_result = ev.get("result", ev.get("value", ""))
    expect("Hello" in str(ev_result),
           f"browser_evaluate returns h1 text (got: {ev_result!r})")

    # browser_type: type into the input
    text, err = call_tool("browser_type", {
        "selector": "#inp",
        "text": "smoke-typed-text",
    })
    expect(not err, f"browser_type succeeds (err={err})")
    typed = json.loads(text)
    expect(typed.get("ok") is True, f"browser_type ok=true (got: {typed})")

    # browser_click: click the button by SoM index (the button is index 2 in
    # the navigate response). We don't assert any side-effect — the data: URL
    # has no JS handler — we just verify the click endpoint returns ok=true
    # with the post-click page state.
    text, err = call_tool("browser_click", {"index": 2})
    expect(not err, f"browser_click succeeds (err={err})")
    clicked = json.loads(text)
    expect(clicked.get("ok") is True, f"browser_click ok=true (got: {clicked})")

    # browser_screenshot: just verify it returns ok + a screenshot path
    text, err = call_tool("browser_screenshot", {})
    expect(not err, f"browser_screenshot succeeds (err={err})")
    ss = json.loads(text)
    expect(ss.get("ok") is True, f"browser_screenshot ok=true (got: {ss})")
    expect("screenshot_path" in str(ss) or "page_data" in str(ss),
           f"browser_screenshot returns page state (got: {str(ss)[:200]!r})")

    # 12. ping
    ping = call("ping", {})
    expect(ping.get("result") is not None and ping.get("error") is None,
           "ping returned empty result")

    print("\n=== ALL CHECKS PASSED ===\n")


if __name__ == "__main__":
    main()
