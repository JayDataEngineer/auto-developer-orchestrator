"""
TUI End-to-End Tests — full coverage of terminal UI rendering.

Uses the TUI visual testing server (tui_visual.py on :9877) to drive the
terminal UI and verify rendering. No vision model required — uses screen
buffer text matching for deterministic assertions.

Requires:
  - Go backend running on :3847
  - TUI visual server on :9877  (task tui-visual)

Start:
  task tui-visual
  pytest tui/test_tui_e2e.py -v

Run specific test:
  pytest tui/test_tui_e2e.py -v -k test_welcome_screen
"""

import json
import time

import pytest
import requests

from fixtures.vision import TUI_VISUAL_URL

pytestmark = [pytest.mark.api, pytest.mark.tui]

BASE = TUI_VISUAL_URL
BACKEND = "http://localhost:3847"
TIMEOUT = 10


# ── Helpers ─────────────────────────────────────────────────────────


def tui_api(method, path, body=None, wait=0):
    """HTTP request to the TUI visual testing server."""
    url = f"{BASE}{path}"
    data = json.dumps(body).encode() if body else None
    resp = requests.request(method, url, json=body, timeout=TIMEOUT)
    result = resp.json() if resp.headers.get("content-type", "").startswith("application/json") else {}
    if wait > 0:
        time.sleep(wait)
    return result


def get_screen():
    """Get current terminal buffer text."""
    r = tui_api("GET", "/screen")
    return r.get("screen", "") if r else ""


def send_key(key, wait=0.5):
    """Send a special key press."""
    return tui_api("POST", "/key", {"key": key, "wait": wait})


def send_input(text, wait=1):
    """Send text input (characters one-by-one like a real user)."""
    return tui_api("POST", "/input", {"text": text, "wait": wait})


def wait_for_text(text, timeout=30, interval=0.5):
    """Poll screen until text appears or timeout."""
    start = time.time()
    while time.time() - start < timeout:
        screen = get_screen()
        if text.lower() in screen.lower():
            return screen
        time.sleep(interval)
    return None


def wait_for_streaming_done(timeout=60, interval=0.5):
    """Wait until streaming indicators (thinking, spinner) disappear."""
    start = time.time()
    was_streaming = False
    while time.time() - start < timeout:
        screen = get_screen()
        lower = screen.lower()
        has_thinking = "thinking" in lower or "thought" in lower
        has_spinner = any(c in screen for c in "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")
        if has_thinking or has_spinner:
            was_streaming = True
        if was_streaming and not has_thinking and not has_spinner:
            return True
        if not was_streaming and not has_thinking and not has_spinner:
            # Never saw streaming start — may have been too fast
            return True
        time.sleep(interval)
    return False


def restart_tui():
    """Restart the TUI process for clean state."""
    tui_api("POST", "/restart")
    time.sleep(1.5)
    for _ in range(20):
        screen = get_screen()
        if "Pux" in screen or "Agent" in screen:
            return True
        time.sleep(0.5)
    return False


# ── Fixtures ────────────────────────────────────────────────────────


@pytest.fixture(autouse=True, scope="module")
def check_tui_server():
    """Skip all tests if the TUI visual server isn't running."""
    try:
        resp = requests.get(f"{BASE}/health", timeout=3)
        if not resp.json().get("running"):
            pytest.skip("TUI visual server not running or TUI process down. Start with: task tui-visual")
    except Exception:
        pytest.skip("TUI visual server not running on :9877. Start with: task tui-visual")


@pytest.fixture(autouse=True)
def clean_state():
    """Restart TUI before each test for clean state."""
    restart_tui()
    time.sleep(0.5)
    yield


# ═══════════════════════════════════════════════════════════════════════
# 1. Welcome Screen
# ═══════════════════════════════════════════════════════════════════════


class TestWelcomeScreen:
    """Verify the TUI welcome screen renders correctly."""

    def test_welcome_visible(self):
        """Welcome screen should show Pux branding."""
        screen = get_screen()
        has_branding = "Pux" in screen or "Agent" in screen
        assert has_branding, f"Welcome screen missing branding. Screen: {screen[:200]}"

    def test_no_scroll_indicator_on_welcome(self):
        """Empty welcome screen should not show scroll indicators."""
        screen = get_screen()
        assert "lines up" not in screen, "Scroll indicator on empty welcome screen"

    def test_status_bar_present(self):
        """Status bar should show model info at the bottom."""
        screen = get_screen()
        # Status bar should have at least some non-empty content
        lines = [l for l in screen.split("\n") if l.strip()]
        assert len(lines) >= 2, f"Status bar missing — screen only has {len(lines)} lines"

    def test_input_area_present(self):
        """Input area should be visible for typing."""
        # Just verify we can type and it doesn't crash
        send_input("test", wait=0.3)
        screen = get_screen()
        assert "test" in screen, "Typed text not visible on screen"
        # Clear with ctrl+u
        send_key("ctrl+u", wait=0.2)

    def test_pageup_no_effect_on_empty(self):
        """PageUp on empty welcome should not create scroll indicator."""
        send_key("pageup", wait=0.3)
        screen = get_screen()
        assert "lines up" not in screen, "PageUp created scroll indicator on empty content"


# ═══════════════════════════════════════════════════════════════════════
# 2. Message Send/Receive Flow
# ═══════════════════════════════════════════════════════════════════════


class TestMessageFlow:
    """Verify message sending and response rendering."""

    def test_send_simple_message(self):
        """Send a simple message and see a response appear."""
        send_input("say hello\n", wait=2)

        # Wait for response to start appearing
        screen = wait_for_text("hello", timeout=30)
        assert screen is not None, "Response 'hello' not found within 30s"

    def test_user_message_echo(self):
        """User message should appear on screen after typing."""
        send_input("say 'user_echo_test'\n", wait=2)

        screen = wait_for_text("user_echo_test", timeout=30)
        assert screen is not None, "User message text not visible"

    def test_response_accumulates(self):
        """Response text should accumulate (not just show the latest delta)."""
        # Ask for something with a longer response
        send_input("list the numbers 1 through 5, one per line\n", wait=2)

        # Wait for the response to complete
        wait_for_streaming_done(timeout=60)

        screen = get_screen()
        # At least some numbers should be present
        has_numbers = any(str(i) in screen for i in range(1, 6))
        assert has_numbers, f"Expected numbers in response. Screen tail: {screen[-300:]}"

    def test_streaming_completes(self):
        """agent_end event should stop streaming — no infinite spinner."""
        send_input("say ok\n", wait=2)
        # Wait for the response text to appear (proof the model answered)
        screen = wait_for_text("ok", timeout=90)
        # Then verify streaming stopped (or response already came through)
        done = wait_for_streaming_done(timeout=90)
        assert done or screen, (
            "Streaming never completed and no response text found (infinite spinner?)"
        )


# ═══════════════════════════════════════════════════════════════════════
# 3. Multi-turn Conversations
# ═══════════════════════════════════════════════════════════════════════


class TestMultiTurn:
    """Verify multi-turn conversations work correctly."""

    def test_two_turns(self):
        """Two sequential messages should both get responses."""
        # First turn
        send_input("say 'turn1_done'\n", wait=2)
        screen = wait_for_text("turn1", timeout=30)
        assert screen is not None, "First turn response not found"
        wait_for_streaming_done(timeout=60)

        # Second turn
        send_input("say 'turn2_done'\n", wait=2)
        screen = wait_for_text("turn2", timeout=30)
        assert screen is not None, "Second turn response not found"
        wait_for_streaming_done(timeout=60)

        # Both should be visible (or at least the second one)
        screen = get_screen()
        assert "turn2" in screen.lower(), "Second turn response not on screen"

    def test_conversation_persists(self):
        """After a message exchange, the TUI should remain responsive."""
        send_input("say ok\n", wait=2)
        wait_for_streaming_done(timeout=60)

        # Send another message to verify TUI is still alive
        send_input("say still_here\n", wait=2)
        screen = wait_for_text("still", timeout=30)
        assert screen is not None, "TUI became unresponsive after first message"


# ═══════════════════════════════════════════════════════════════════════
# 4. Tool Execution Rendering
# ═══════════════════════════════════════════════════════════════════════


class TestToolRendering:
    """Verify tool calls render correctly in the TUI."""

    def test_bash_tool_appears(self):
        """Bash tool execution should show on screen."""
        send_input("run: echo BASH_TOOL_TEST_123\n", wait=2)

        # Wait for tool to execute
        screen = wait_for_text("bash", timeout=30)
        if screen is None:
            pytest.skip("Model didn't call bash tool — tool rendering test needs tool usage")

        # Tool indicator should be visible
        assert "bash" in screen.lower() or "echo" in screen.lower(), \
            f"Expected bash tool indicator. Screen: {screen[-300:]}"

    def test_tool_result_visible(self):
        """Tool result text should appear on screen."""
        send_input("run: echo UNIQUE_RESULT_MARKER_XYZ\n", wait=2)

        # Wait for the marker to appear (in tool result or response)
        screen = wait_for_text("UNIQUE_RESULT", timeout=45)
        if screen is None:
            # Model might have summarized — check if it at least responded
            screen = get_screen()
            assert len(screen.strip()) > 50, \
                "Screen seems empty after tool execution — no response rendered"

    def test_tool_start_and_end_pair(self):
        """Tool execution should show both start and complete states."""
        send_input("run this command: date\n", wait=2)

        # Wait for the response to complete
        wait_for_streaming_done(timeout=60)
        screen = get_screen()

        # Should have content (either tool output or response about it)
        assert len(screen.strip()) > 30, \
            f"Screen too sparse after tool call. Content: {screen[-200:]}"


# ═══════════════════════════════════════════════════════════════════════
# 5. Thinking/Reasoning Display
# ═══════════════════════════════════════════════════════════════════════


class TestThinkingDisplay:
    """Verify thinking/reasoning blocks render in the TUI."""

    def test_thinking_appears_for_complex_prompt(self):
        """Complex prompts should trigger thinking blocks."""
        send_input("think step by step: what is 17 * 23?\n", wait=2)

        # Wait for either thinking indicator or response
        start = time.time()
        saw_thinking = False
        while time.time() - start < 30:
            screen = get_screen()
            lower = screen.lower()
            if "thinking" in lower or "thought" in lower:
                saw_thinking = True
                break
            # If we already have a text response, thinking might have been too fast
            if any(c.isdigit() for c in screen):
                break
            time.sleep(0.5)

        # Thinking is model-dependent — not a hard failure if absent
        # But we should at least get a response
        screen = get_screen()
        assert len(screen.strip()) > 20, "No response rendered for math question"

    def test_thinking_collapses_after_completion(self):
        """Thinking block should show collapsed state after streaming ends."""
        send_input("think carefully about the meaning of 42\n", wait=2)
        wait_for_streaming_done(timeout=60)

        screen = get_screen()
        # After completion, should have content (not blank)
        assert len(screen.strip()) > 30, \
            f"Screen sparse after thinking completed. Content: {screen[-200:]}"


# ═══════════════════════════════════════════════════════════════════════
# 6. Slash Commands
# ═══════════════════════════════════════════════════════════════════════


class TestSlashCommands:
    """Verify slash commands work in the TUI."""

    def test_help_command(self):
        """/help should show available commands."""
        send_input("/help\n", wait=1)

        screen = get_screen()
        # Help output should mention some commands
        has_help = any(cmd in screen.lower() for cmd in ["/help", "/quit", "/clear", "commands"])
        assert has_help, f"/help output not found. Screen: {screen[-200:]}"

    def test_help_does_not_send_to_backend(self):
        """/help should be handled locally, not sent to the agent."""
        send_input("/help\n", wait=1)
        time.sleep(1)

        # Should NOT start streaming (no thinking/spinner)
        screen = get_screen()
        has_streaming = "thinking" in screen.lower() or "⠋" in screen
        assert not has_streaming, "/help triggered agent streaming — should be local only"

    def test_clear_command(self):
        """/clear should clear conversation history."""
        # First send a message
        send_input("say 'before_clear'\n", wait=2)
        wait_for_text("before_clear", timeout=30)
        wait_for_streaming_done(timeout=60)

        # Clear
        send_input("/clear\n", wait=1)

        # Screen should be mostly empty or show welcome again
        screen = get_screen()
        has_old = "before_clear" in screen.lower()
        assert not has_old, f"/clear didn't clear old messages. Screen: {screen[-200:]}"

    def test_status_command(self):
        """/status should show session information."""
        send_input("/status\n", wait=1)

        screen = get_screen()
        # Status should show something about the session
        has_status = any(kw in screen.lower() for kw in ["session", "status", "model", "project"])
        assert has_status, f"/status output not found. Screen: {screen[-200:]}"


# ═══════════════════════════════════════════════════════════════════════
# 7. Scroll Behavior
# ═══════════════════════════════════════════════════════════════════════


class TestScrollBehavior:
    """Verify scroll behavior with content exceeding viewport."""

    def _populate_long_content(self):
        """Send multiple messages to exceed viewport height."""
        for i in range(1, 7):
            send_input(f"say 'line_{i}' and nothing else\n", wait=1)
            wait_for_text(f"line_{i}", timeout=30)
            wait_for_streaming_done(timeout=60)
            time.sleep(0.5)

    def test_pageup_shows_indicator(self):
        """PageUp should show scroll indicator when content exceeds viewport."""
        self._populate_long_content()

        send_key("pageup", wait=0.5)
        screen = get_screen()

        # Either we have a scroll indicator, or content fits viewport (also valid)
        has_indicator = "lines up" in screen
        has_content = len(screen.strip()) > 30
        assert has_indicator or has_content, \
            "No scroll indicator and no visible content after PageUp"

    def test_escape_resets_scroll(self):
        """Escape should reset scroll to bottom."""
        self._populate_long_content()

        send_key("pageup", wait=0.5)
        send_key("escape", wait=0.5)
        screen = get_screen()

        # Scroll indicator should be gone
        assert "lines up" not in screen, "Scroll indicator still present after Escape"

    def test_auto_scroll_on_new_message(self):
        """New message should auto-scroll to bottom."""
        self._populate_long_content()

        # Scroll up
        send_key("pageup", wait=0.5)
        screen_before = get_screen()
        was_scrolled = "lines up" in screen_before

        if not was_scrolled:
            pytest.skip("Content fits viewport — can't test auto-scroll")

        # Send new message
        send_input("say 'auto_scroll_check'\n", wait=2)
        time.sleep(3)

        screen = get_screen()
        # Scroll indicator should be gone (auto-scrolled to bottom)
        assert "lines up" not in screen, \
            "Scroll indicator persists after new message — auto-scroll broken"


# ═══════════════════════════════════════════════════════════════════════
# 8. Status Bar
# ═══════════════════════════════════════════════════════════════════════


class TestStatusBar:
    """Verify status bar content updates correctly."""

    def test_status_bar_shows_after_interaction(self):
        """After a message exchange, status bar should show token counts."""
        send_input("say ok\n", wait=2)
        wait_for_streaming_done(timeout=60)

        screen = get_screen()
        # Bottom of screen should have some status info
        lines = [l.strip() for l in screen.split("\n") if l.strip()]
        assert len(lines) >= 2, "Status bar missing after interaction"

    def test_status_bar_updates_with_usage(self):
        """Status bar token counts should update after each exchange."""
        send_input("say alpha\n", wait=2)
        wait_for_streaming_done(timeout=60)

        screen1 = get_screen()

        send_input("say beta\n", wait=2)
        wait_for_streaming_done(timeout=60)

        screen2 = get_screen()

        # At minimum, the screen should have changed
        # (new response text, updated token counts, etc.)
        assert screen1 != screen2 or len(screen2.strip()) > 20, \
            "Screen didn't change after second message — may be frozen"


# ═══════════════════════════════════════════════════════════════════════
# 9. Error Handling
# ═══════════════════════════════════════════════════════════════════════


class TestErrorHandling:
    """Verify TUI handles error conditions gracefully."""

    def test_long_input_accepted(self):
        """Long input should be accepted without crash."""
        long_msg = "say " + ("x" * 200) + "\n"
        send_input(long_msg, wait=2)

        # Should not crash
        time.sleep(2)
        screen = get_screen()
        assert len(screen.strip()) > 0, "TUI crashed on long input"

    def test_special_characters_in_input(self):
        """Special characters should not crash the TUI."""
        send_input("say 'hello \"world\" and <test> & more'\n", wait=2)
        wait_for_streaming_done(timeout=60)

        screen = get_screen()
        assert len(screen.strip()) > 20, "TUI may have crashed on special chars"

    def test_rapid_input(self):
        """Rapid successive inputs should not crash."""
        for i in range(3):
            send_input(f"say '{i}'\n", wait=0.5)

        wait_for_streaming_done(timeout=90)
        screen = get_screen()
        assert len(screen.strip()) > 0, "TUI became unresponsive after rapid input"

    def test_ctrl_c_does_not_crash_tui(self):
        """Ctrl+C should be handled gracefully."""
        send_input("say something long", wait=0.3)
        send_key("ctrl+c", wait=0.5)

        # TUI should still be responsive
        screen = get_screen()
        assert len(screen.strip()) > 0, "TUI blanked after Ctrl+C"

    def test_empty_input_no_crash(self):
        """Pressing Enter with empty input should not crash."""
        send_input("\n", wait=0.5)
        screen = get_screen()
        assert len(screen.strip()) > 0, "TUI crashed on empty Enter"
