"""
ask_user TUI Tests — QuestionDialog rendering and interaction.

Verifies that the TUI's QuestionDialog component renders correctly when
the ask_user tool fires, and that answering via keyboard resolves the
decision.

Requires:
  - Go backend running on :3847
  - TUI visual server on :9877  (task tui-visual)

Start:
  task tui-visual
  pytest tui/test_ask_user.py -v
"""

import json
import time

import pytest
import requests

from fixtures.vision import TUI_VISUAL_URL

pytestmark = [pytest.mark.api, pytest.mark.tui, pytest.mark.slow, pytest.mark.llm]

BASE = TUI_VISUAL_URL
BACKEND = "http://localhost:3847"

ASK_USER_PROMPT = (
    'Call the ask_user tool with question="Pick a number" '
    'and options=["1","2"]. Do not respond yourself - only use the tool.'
)


# ── Helpers ─────────────────────────────────────────────────────────


def tui_api(method, path, body=None):
    resp = requests.request(method, f"{BASE}{path}", json=body, timeout=10)
    return resp.json()


def get_screen():
    return tui_api("GET", "/screen").get("screen", "")


def send_key(key, wait=0.5):
    return tui_api("POST", "/key", {"key": key, "wait": wait})


def send_input(text, wait=1):
    return tui_api("POST", "/input", {"text": text, "wait": wait})


def wait_for_text(text, timeout=45, interval=0.5):
    start = time.time()
    while time.time() - start < timeout:
        screen = get_screen()
        if text.lower() in screen.lower():
            return screen
        time.sleep(interval)
    return None


def wait_for_question_dialog(timeout=90, interval=0.5):
    """Wait for the QuestionDialog to appear (has '? Question' header)."""
    start = time.time()
    while time.time() - start < timeout:
        screen = get_screen()
        if "? Question" in screen:
            return screen
        time.sleep(interval)
    return None


def wait_for_streaming_done(timeout=90, interval=0.5):
    start = time.time()
    was_streaming = False
    while time.time() - start < timeout:
        screen = get_screen()
        lower = screen.lower()
        has_thinking = "thinking" in lower
        if has_thinking:
            was_streaming = True
        if was_streaming and not has_thinking:
            return True
        if not was_streaming and not has_thinking:
            return True
        time.sleep(interval)
    return False


def restart_tui():
    tui_api("POST", "/restart")
    time.sleep(1.5)


# ── Fixtures ────────────────────────────────────────────────────────


@pytest.fixture(autouse=True)
def clean_tui():
    """Restart TUI for clean state before each test."""
    restart_tui()
    yield


# ── Tests ───────────────────────────────────────────────────────────


class TestAskUserTUI:
    """Verify ask_user QuestionDialog rendering and interaction in TUI."""

    def test_question_dialog_renders(self):
        """After ask_user fires, the QuestionDialog must show the question and options."""
        send_input(ASK_USER_PROMPT + "\n", wait=2)

        # Wait for the question dialog — "? Question" is the header
        screen = wait_for_question_dialog(timeout=90)
        assert screen is not None, (
            "Question dialog did not appear within 90s. "
            f"Screen: {get_screen()[:500]}"
        )

        # Verify the question text rendered
        assert "pick a number" in screen.lower(), (
            f"Question text not found. Screen: {screen[:500]}"
        )

        # Verify input prompt
        lower = screen.lower()
        assert "enter to submit" in lower or "type answer" in lower, (
            f"Input prompt not found. Screen: {screen[:500]}"
        )

    def test_question_dialog_answer_by_number(self):
        """Typing option number + Enter must resolve the decision."""
        send_input(ASK_USER_PROMPT + "\n", wait=2)

        screen = wait_for_question_dialog(timeout=90)
        assert screen is not None, "Question dialog did not appear"

        # Type option number and submit
        send_input("1")
        send_key("enter", wait=10)

        # Wait for streaming to finish
        wait_for_streaming_done(timeout=60)

        # Verify tool shows as done
        screen = get_screen()
        lower = screen.lower()
        assert "done" in lower or "selected" in lower or "picked" in lower, (
            f"Tool did not complete after answering. Screen: {screen[:500]}"
        )

    def test_question_dialog_answer_by_text(self):
        """Typing free-text answer + Enter must resolve the decision."""
        send_input(ASK_USER_PROMPT + "\n", wait=2)

        screen = wait_for_question_dialog(timeout=90)
        assert screen is not None, "Question dialog did not appear"

        # Type a free-text answer
        send_input("forty-two")
        send_key("enter", wait=10)

        # Wait for streaming to finish
        wait_for_streaming_done(timeout=60)

        # Verify model received the answer
        screen = get_screen()
        lower = screen.lower()
        assert "forty-two" in lower or "42" in lower or "done" in lower, (
            f"Free-text answer not processed. Screen: {screen[:500]}"
        )

    def test_question_dialog_backspace_and_correct(self):
        """User can backspace and retype before submitting."""
        send_input(ASK_USER_PROMPT + "\n", wait=2)

        screen = wait_for_question_dialog(timeout=90)
        assert screen is not None, "Question dialog did not appear"

        # Type wrong input, backspace, correct it
        send_input("3")
        send_key("backspace", wait=0.3)
        send_input("2")
        send_key("enter", wait=10)

        wait_for_streaming_done(timeout=60)

        screen = get_screen()
        lower = screen.lower()
        assert "done" in lower or "2" in lower, (
            f"Corrected answer not processed. Screen: {screen[:500]}"
        )
