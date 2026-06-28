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
                "browser_screenshot", "browser_evaluate",
                "desktop_screenshot", "desktop_click", "desktop_type", "desktop_key"}
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

    # 13. desktop_screenshot + desktop_click + desktop_type + desktop_key.
    # The sandbox runs Xvfb on DISPLAY=:99 (auto-enabled alongside browser
    # mode). desktop_observe.py ships at /usr/local/bin/. We just verify
    # each tool returns ok=true — the desktop is empty (just fluxbox wm)
    # so we don't assert any specific UI state.
    text, err = call_tool("desktop_screenshot", {})
    expect(not err, f"desktop_screenshot succeeds (err={err})")
    ss = json.loads(text)
    expect(ss.get("ok") is True, f"desktop_screenshot ok=true (got: {str(ss)[:200]!r})")
    expect("image_b64" in ss, f"desktop_screenshot returns image (got keys: {list(ss.keys())})")
    expect("resolution" in ss, f"desktop_screenshot returns resolution (got keys: {list(ss.keys())})")
    # Pick a safe coord for click (center of a 1280x720 desktop — definitely empty).
    width = ss.get("resolution", {}).get("width", 1280)
    height = ss.get("resolution", {}).get("height", 720)
    click_x, click_y = width // 2, height // 2

    text, err = call_tool("desktop_click", {"x": click_x, "y": click_y})
    expect(not err, f"desktop_click succeeds (err={err})")
    click = json.loads(text)
    expect(click.get("ok") is True, f"desktop_click ok=true (got: {click})")
    expect(click.get("x") == click_x, f"desktop_click echoes x (got: {click})")

    text, err = call_tool("desktop_type", {"text": "smoke-typed", "clear": False})
    expect(not err, f"desktop_type succeeds (err={err})")
    typed = json.loads(text)
    expect(typed.get("ok") is True, f"desktop_type ok=true (got: {typed})")

    text, err = call_tool("desktop_key", {"keys": "Escape"})
    expect(not err, f"desktop_key succeeds (err={err})")
    keyed = json.loads(text)
    expect(keyed.get("ok") is True, f"desktop_key ok=true (got: {keyed})")

    # 14. Dispatch surface (dispatch_task / get_task_status / list_orgs).
    # Opt-in: the surface is only registered when PUX_LLM_API_KEY is set at
    # server boot. When enabled, exercise the full dispatch → poll → complete
    # flow against the shipped _demo org. When disabled, skip cleanly.
    has_dispatch = "dispatch_task" in names
    if not has_dispatch:
        print("\n  SKIP  dispatch surface (set PUX_LLM_API_KEY on the server to enable)")
    else:
        # list_orgs must surface _demo (the shipped example template).
        text, err = call_tool("list_orgs", {})
        expect(not err, f"list_orgs succeeds (err={err})")
        listed = json.loads(text)
        org_names = [o["name"] for o in listed.get("orgs", [])]
        expect("_demo" in org_names,
               f"list_orgs surfaces shipped _demo template (got: {org_names})")
        demo = next(o for o in listed["orgs"] if o["name"] == "_demo")
        expect("researcher" in demo.get("roles", []),
               f"_demo declares researcher role (got: {demo['roles']})")

        # If no LLM key in THIS env, we can't actually drive a dispatch (the
        # server has one but our test doesn't pass it). Verify the wire shape
        # by calling dispatch_task with an unknown org — must error cleanly.
        text, err = call_tool("dispatch_task", {
            "org_name": "does-not-exist",
            "task_description": "noop",
        })
        expect(err, "dispatch_task rejects unknown org with isError=true")

        # If the test harness has a key, run the full happy path.
        if os.environ.get("PUX_LLM_API_KEY"):
            print("\n  --- driving full dispatch flow against _demo ---")
            text, err = call_tool("dispatch_task", {
                "org_name": "_demo",
                "task_description": (
                    "Write the literal string 'hello from dispatch' to "
                    "/workspace/dispatch_smoke.txt, then read it back and "
                    "include the contents verbatim in your final response."
                ),
            })
            expect(not err, f"dispatch_task accepts _demo (err={err})")
            disp = json.loads(text)
            task_id = disp.get("task_id")
            expect(bool(task_id), f"dispatch_task returns task_id (got: {disp})")
            expect(disp.get("status") == "pending",
                   f"dispatch_task initial status=pending (got: {disp})")

            # Poll for completion (timeout 180s — first-run latency + LLM).
            deadline = 180
            interval = 1.0
            final = None
            import time as _time
            t0 = _time.time()
            while _time.time() - t0 < deadline:
                text, err = call_tool("get_task_status", {"task_id": task_id})
                expect(not err, f"get_task_status succeeds (err={err})")
                status = json.loads(text).get("status")
                if status in ("complete", "failed"):
                    final = json.loads(text)
                    break
                _time.sleep(interval)
            expect(final is not None,
                   f"task reached terminal state within {deadline}s (last: {status})")
            print(f"  PASS  task {task_id} reached status={final['status']}")
            if final["status"] == "complete":
                expect("hello from dispatch" in (final.get("result") or ""),
                       f"task result contains expected string (got: {final.get('result')!r})")
            else:
                # Surface the error verbatim so failures are debuggable.
                print(f"  NOTE  task failed: {final.get('error')!r}")
                # Don't fail the smoke test on LLM-side failures (network,
                # quota, etc.) — the contract is "tool returned a final state".
        else:
            print("\n  SKIP  full dispatch flow (set PUX_LLM_API_KEY in smoke env to exercise)")

    print("\n=== ALL CHECKS PASSED ===\n")


if __name__ == "__main__":
    main()
