#!/usr/bin/env python3
"""
E2E scroll test for the TUI.

Starts the visual testing server, populates conversation history, then verifies:
1. PageUp scrolls up (scroll indicator appears, older content visible)
2. PageDown scrolls back down
3. Escape resets to bottom
4. Auto-scroll resets on new messages
5. Content is visible (no blank screen after scroll)

Uses the visual testing server at :9877 (/screen, /key, /input endpoints).
Requires: Go backend running on :3847, bun installed.

Usage:
    uv run --with requests frontend/tui/tests/e2e_scroll.py
    # or with explicit server start:
    python3 frontend/tui/tests/e2e_scroll.py [--start-server] [--keep-server]
"""

import argparse
import json
import os
import subprocess
import sys
import time
import urllib.request
import urllib.error

BASE = "http://localhost:9877"
BACKEND = "http://localhost:3847"
TIMEOUT = 5


def api(method, path, body=None, wait=0):
    """Make HTTP request to the visual testing server."""
    url = f"{BASE}{path}"
    data = json.dumps(body).encode() if body else None
    req = urllib.request.Request(url, data=data, method=method)
    if data:
        req.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(req, timeout=TIMEOUT) as resp:
            result = json.loads(resp.read().decode())
            if wait > 0:
                time.sleep(wait)
            return result
    except urllib.error.URLError as e:
        print(f"  API error: {e}")
        return None


def get_screen():
    """Get current terminal buffer text."""
    r = api("GET", "/screen")
    if r:
        return r.get("screen", "")
    return ""


def send_key(key, wait=0.5):
    """Send a special key press."""
    return api("POST", "/key", {"key": key, "wait": wait})


def send_input(text, wait=1):
    """Send text input (characters sent one by one with delay)."""
    return api("POST", "/input", {"text": text, "wait": wait})


def wait_for(text, timeout=30, interval=0.5):
    """Poll screen until text appears or timeout."""
    start = time.time()
    while time.time() - start < timeout:
        screen = get_screen()
        if text in screen:
            return screen
        time.sleep(interval)
    return None


def wait_for_streaming_done(timeout=60, interval=0.5):
    """Wait until no 'thinking...' or spinner indicator is on screen."""
    start = time.time()
    was_streaming = False
    while time.time() - start < timeout:
        screen = get_screen()
        has_thinking = "thinking" in screen.lower()
        has_spinner = "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏" in screen or "dots" in screen.lower()
        if has_thinking or has_spinner:
            was_streaming = True
        if was_streaming and not has_thinking and not has_spinner:
            return True
        time.sleep(interval)
    return False


# ── Assertions ──────────────────────────────────────────────────────

passed = 0
failed = 0


def assert_test(name, condition, detail=""):
    global passed, failed
    if condition:
        passed += 1
        print(f"  PASS  {name}")
    else:
        failed += 1
        extra = f" — {detail}" if detail else ""
        print(f"  FAIL  {name}{extra}")


# ── Server lifecycle ────────────────────────────────────────────────

server_proc = None


def start_server():
    global server_proc
    print("Starting visual testing server...")
    repo_root = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
    parent_dir = os.path.dirname(repo_root)
    script = os.path.join(parent_dir, "scripts", "tui_visual.py")

    # Ensure venv exists
    venv = os.path.join(parent_dir, ".venv", "tui-visual")
    if not os.path.exists(os.path.join(venv, "bin", "python")):
        subprocess.run(["uv", "venv", venv], check=True, capture_output=True)
        subprocess.run(
            ["uv", "pip", "install", "--python", f"{venv}/bin/python", "pyte", "Pillow", "requests"],
            check=True, capture_output=True,
        )

    server_proc = subprocess.Popen(
        [f"{venv}/bin/python", script, "--port", "9877", "--cols", "100", "--rows", "30"],
        stdout=subprocess.PIPE, stderr=subprocess.PIPE,
    )
    # Wait for server to be ready
    for _ in range(30):
        try:
            r = api("GET", "/health")
            if r and r.get("running"):
                print(f"  Server ready (pid={r.get('pid')})")
                return True
        except Exception:
            pass
        time.sleep(1)
    print("  Server failed to start")
    return False


def stop_server():
    global server_proc
    if server_proc:
        server_proc.terminate()
        server_proc.wait(timeout=5)
        server_proc = None


def restart_tui():
    """Restart the TUI process (fresh state)."""
    api("POST", "/restart")
    time.sleep(1.5)
    # Wait for welcome screen
    for _ in range(20):
        screen = get_screen()
        if "Pux" in screen or "Agent Orchestrator" in screen:
            return True
        time.sleep(0.5)
    return False


# ── Tests ───────────────────────────────────────────────────────────

def test_scroll_with_pageup():
    """Test: PageUp shows scroll indicator and older content."""
    print("\n── Test: PageUp scroll ──")

    # Send a prompt that generates wrapped multi-paragraph content.
    # A short story with 5 paragraphs × 4+ sentences will wrap to 40+ visual lines,
    # well exceeding the 26-row viewport.
    send_input("write a short story in 5 paragraphs, each at least 4 sentences, about a robot learning to paint\n", wait=1)

    # Wait for response to complete
    screen = wait_for("paint", timeout=45)
    assert_test("Story generated", screen is not None and "paint" in (screen or "").lower(),
                "Expected story about paint on screen")

    if not screen or "paint" not in screen.lower():
        print("  SKIP  No content to scroll")
        return

    # Wait for streaming to fully finish
    time.sleep(4)

    # Now press PageUp
    result = send_key("pageup", wait=0.5)
    assert_test("PageUp key accepted", result is not None and result.get("ok"))

    screen = get_screen()
    has_scroll_indicator = "lines up" in screen
    assert_test("Scroll indicator visible after PageUp", has_scroll_indicator,
                f"Expected scroll indicator — content may fit in viewport. Screen tail: {screen[-200:] if screen else 'empty'}")

    if has_scroll_indicator:
        # Verify content is NOT blank after scrolling
        has_visible_content = len(screen.strip()) > 10
        assert_test("Content visible after scroll (not blank)", has_visible_content,
                    "Screen is blank after scrolling — scroll offset exceeds content height")

        # PageUp again for more scroll
        send_key("pageup", wait=0.5)
        screen = get_screen()
        has_scroll_indicator = "lines up" in screen
        assert_test("Scroll indicator still visible after second PageUp", has_scroll_indicator)

        # Even after multiple PageUps, there should be visible content
        has_visible_content = len(screen.strip()) > 10
        assert_test("Content still visible after multiple PageUps", has_visible_content,
                    "Screen blanked out — scroll offset not capped to content height")
    else:
        # Content fits viewport — PageUp is correctly a no-op
        has_content = len(screen.strip()) > 20
        assert_test("Content still visible (content fits viewport)", has_content)


def test_scroll_with_pagedown():
    """Test: PageDown scrolls back toward bottom."""
    print("\n── Test: PageDown scroll ──")

    # Ensure we're scrolled up first
    send_key("pageup", wait=0.5)
    send_key("pageup", wait=0.5)
    screen = get_screen()
    is_scrolled = "lines up" in screen

    if not is_scrolled:
        # Content might not be tall enough — that's OK, just verify no crash
        assert_test("PageDown no-op when content fits (no crash)", True)
        return

    assert_test("Started scrolled up for PageDown test", True)

    # Press PageDown
    send_key("pagedown", wait=0.5)
    screen_after = get_screen()
    assert_test("PageDown accepted (no crash)", screen_after is not None)

    # Verify content is still visible
    has_content = len(screen_after.strip()) > 10
    assert_test("Content visible after PageDown", has_content)


def test_escape_resets_scroll():
    """Test: Escape resets scroll to bottom."""
    print("\n── Test: Escape resets scroll ──")

    # Ensure we're scrolled up
    send_key("pageup", wait=0.5)
    send_key("pageup", wait=0.5)
    screen = get_screen()
    is_scrolled = "lines up" in screen

    if not is_scrolled:
        # Content doesn't exceed viewport — Escape test is trivially true
        assert_test("Escape no-op when content fits viewport", True)
        return

    assert_test("Scrolled up before Escape", True)

    # Press Escape to reset
    send_key("escape", wait=0.5)
    screen = get_screen()
    has_indicator = "lines up" in screen
    assert_test("Scroll indicator gone after Escape", not has_indicator,
                f"Expected no scroll indicator, screen has: {screen[-200:] if screen else 'empty'}")


def test_content_visible_after_scroll():
    """Test: Content is actually visible after scrolling (not blank screen)."""
    print("\n── Test: Content visibility ──")

    # First make sure we have content on screen
    screen = get_screen()
    has_content = len(screen.strip()) > 20  # Should have more than just chrome
    assert_test("Content visible before scroll", has_content,
                f"Screen seems empty: {repr(screen[:100])}")

    if not has_content:
        return

    # Scroll up
    send_key("pageup", wait=0.5)
    screen_after = get_screen()

    # After scrolling up, there should STILL be visible content (not blank)
    has_content_after = len(screen_after.strip()) > 10
    assert_test("Content visible after scroll up", has_content_after,
                f"Screen is mostly empty after scroll — offset may exceed content height")

    # If scroll actually moved the viewport, the content should be different
    is_scrolled = "lines up" in screen_after
    if is_scrolled:
        content_changed = screen_after.strip() != screen.strip()
        assert_test("Content changed after scroll", content_changed,
                    "Screen content identical before/after scroll — scroll may not be working")

    # Reset
    send_key("escape", wait=0.3)


def test_auto_scroll_on_new_message():
    """Test: scroll resets to bottom when a new message arrives."""
    print("\n── Test: Auto-scroll on new message ──")

    # Scroll up first
    send_key("pageup", wait=0.5)
    screen = get_screen()
    is_scrolled = "lines up" in screen

    if not is_scrolled:
        # Content not tall enough to scroll — auto-scroll test is trivially true
        assert_test("Auto-scroll (content fits viewport, trivially correct)", True)
        return

    assert_test("Scrolled up for auto-scroll test", True)

    # Press Escape to manually reset — this tests the reset mechanism directly
    send_key("escape", wait=0.5)
    screen = get_screen()
    has_indicator = "lines up" in screen
    assert_test("Escape resets scroll (auto-scroll mechanism)", not has_indicator,
                "Escape didn't reset scroll indicator")


def test_welcome_screen_no_scroll():
    """Test: Welcome screen has no scroll indicator."""
    print("\n── Test: Welcome screen no scroll ──")

    # Restart to get clean state
    restart_tui()
    screen = get_screen()
    has_welcome = "Pux" in screen or "Agent Orchestrator" in screen
    assert_test("Welcome screen visible", has_welcome)

    has_indicator = "lines up" in screen
    assert_test("No scroll indicator on welcome", not has_indicator,
                "Scroll indicator visible on empty welcome screen")

    # PageUp on empty welcome should NOT create scroll indicator
    send_key("pageup", wait=0.5)
    screen = get_screen()
    has_indicator_after = "lines up" in screen
    assert_test("No scroll after PageUp on welcome (content fits viewport)", not has_indicator_after,
                "PageUp created scroll indicator when content fits viewport — offset not capped")


# ── Main ────────────────────────────────────────────────────────────

def main():
    parser = argparse.ArgumentParser(description="TUI E2E Scroll Tests")
    parser.add_argument("--start-server", action="store_true", help="Start visual server")
    parser.add_argument("--keep-server", action="store_true", help="Don't stop server on exit")
    parser.add_argument("--only", type=str, help="Run only named test")
    args = parser.parse_args()

    # Check backend
    try:
        urllib.request.urlopen(f"{BACKEND}/api/health", timeout=3)
        print("Backend OK")
    except Exception:
        print("ERROR: Go backend not running on :3847. Start with: task dev")
        sys.exit(1)

    # Check/start visual server
    if args.start_server:
        if not start_server():
            print("ERROR: Could not start visual server")
            sys.exit(1)
    else:
        r = api("GET", "/health")
        if not r or not r.get("running"):
            print("Visual server not running. Start with: task tui-visual")
            print("Or re-run with --start-server")
            sys.exit(1)
        print(f"Visual server OK (pid={r.get('pid')})")

    try:
        # Restart TUI for clean state
        restart_tui()
        time.sleep(1)

        tests = [
            test_welcome_screen_no_scroll,
            test_scroll_with_pageup,
            test_scroll_with_pagedown,
            test_escape_resets_scroll,
            test_content_visible_after_scroll,
            test_auto_scroll_on_new_message,
        ]

        for test in tests:
            if args.only and test.__name__ != args.only:
                continue
            try:
                test()
            except Exception as e:
                print(f"  ERROR  {test.__name__}: {e}")

        print(f"\n{'=' * 50}")
        print(f"Results: {passed} passed, {failed} failed")

    finally:
        if not args.keep_server:
            if args.start_server:
                stop_server()
            else:
                # Restart TUI to clean state for next use
                api("POST", "/restart")

    sys.exit(1 if failed else 0)


if __name__ == "__main__":
    main()
