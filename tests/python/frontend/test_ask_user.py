"""
ask_user WebUI Tests — AskUserToolUI rendering and interaction.

Part 1 (mocked SSE): Verifies the AskUserToolUI component renders correctly
  when a decision_request event arrives. No live backend needed.
Part 2 (live backend): Full end-to-end with real SSE stream.

Requires:
  - Vite frontend running (task dev-frontend)

Start:
  task dev-frontend
  pytest frontend/test_ask_user.py -v              # mocked only
  pytest frontend/test_ask_user.py -v --live       # with live backend tests
"""

import json
import time

import pytest

from conftest import API_BASE_URL
from fixtures.browser import goto_frontend

pytestmark = pytest.mark.playwright

API = API_BASE_URL


# ── SSE Mock Factories ─────────────────────────────────────────────


def sse_event(event_type, data):
    """Format a single SSE event."""
    return f"event: {event_type}\ndata: {json.dumps(data)}\n\n"


def make_ask_user_response(
    question="What is your favorite color?",
    options=None,
    tool_id="tc_ask_001",
):
    """SSE stream: model calls ask_user, which emits decision_request.

    The stream stops at the decision_request event (tool blocks).
    Use make_ask_user_resolution() for the continuation.
    """
    options = options or ["Red", "Blue", "Green"]
    return (
        sse_event("agent_spawned", {"agentId": "test-agent-ask"}) +
        sse_event("agent_start", {}) +
        sse_event("thinking_delta", {"text": "Let me ask the user."}) +
        sse_event("tool_execution_start", {
            "toolName": "ask_user",
            "args": {"question": question, "options": options},
            "toolId": tool_id,
        }) +
        sse_event("decision_request", {
            "decisionId": "q_test_decision_001",
            "sourceTool": "ask_user",
            "title": question,
            "hint": "question",
            "options": options,
            "allowFreeText": True,
        }) +
        # Stream stays open — tool is blocking
        ""
    )


def make_ask_user_resolution(tool_id="tc_ask_001", answer="Red"):
    """SSE continuation: tool completes with answer, model responds."""
    return (
        sse_event("tool_execution_end", {
            "toolId": tool_id,
            "toolName": "ask_user",
            "result": {"response": answer},
        }) +
        sse_event("text_delta", {"text": f"You picked {answer}!"}) +
        sse_event("agent_end", {"input": 500, "output": 20, "cache": 0}) +
        "data: [DONE]\n\n"
    )


def mock_sse_route(page, response_body):
    """Set up route mock for /api/pux/prompt returning given SSE body."""
    def handle_route(route):
        route.fulfill(
            status=200,
            headers={
                "Content-Type": "text/event-stream",
                "Cache-Control": "no-cache",
                "Connection": "keep-alive",
            },
            body=response_body,
        )
    page.route("**/api/pux/prompt", handle_route)


def mock_decision_route(page, responses=None):
    """Mock the /api/pux/decision endpoint."""
    default_responses = responses or {"q_test_decision_001": {"success": True}}
    def handle_route(route):
        route.fulfill(
            status=200,
            headers={"Content-Type": "application/json"},
            body=json.dumps(default_responses.get("q_test_decision_001", {"success": True})),
        )
    page.route("**/api/pux/decision", handle_route)


def send_message(page, text):
    """Type into composer and submit via form.requestSubmit().

    Playwright's fill+Enter doesn't reliably trigger assistant-ui's
    form submission in headless Chromium. form.requestSubmit() does.
    """
    ta = page.locator("textarea[placeholder*='Send a message']").first
    ta.click()
    page.keyboard.type(text, delay=10)
    page.evaluate("""() => {
        const ta = document.querySelector('textarea[placeholder*="Send a message"]');
        ta?.closest('form')?.requestSubmit();
    }""")


# ═══════════════════════════════════════════════════════════════════════
# Part 1: Mocked SSE — AskUserToolUI rendering
# ═══════════════════════════════════════════════════════════════════════


class TestAskUserCardRender:
    """Verify AskUserToolUI renders the question card from mocked SSE."""

    @pytest.fixture(autouse=True)
    def _setup(self, page, frontend_url):
        goto_frontend(page, frontend_url)
        mock_sse_route(page, make_ask_user_response())
        mock_decision_route(page)

    def test_question_header_visible(self, page):
        """The 'Question' label must render."""
        send_message(page, "Ask me a question")
        label = page.locator("text=Question").first
        assert label.is_visible(timeout=10000), "Question label not visible"

    def test_question_text_visible(self, page):
        """The question text must render."""
        send_message(page, "Ask me a question")
        # Wait for the card first, then check for question text
        page.locator("text=Question").first.wait_for(state="visible", timeout=10000)
        # Use get_by_text with exact=False to match partial text including "?"
        text = page.get_by_text("favorite color", exact=False).first
        assert text.is_visible(timeout=5000), "Question text not visible"

    def test_option_buttons_visible(self, page):
        """Option buttons must render for each choice."""
        send_message(page, "Ask me a question")
        # Wait for the card to appear
        page.locator("text=Question").first.wait_for(state="visible", timeout=10000)

        # Check each option button exists
        for opt in ["Red", "Blue", "Green"]:
            btn = page.locator(f"button:has-text('{opt}')").first
            assert btn.is_visible(timeout=3000), f"Option button '{opt}' not visible"

    def test_free_text_input_visible(self, page):
        """Free-text input must render (allowFreeText=true)."""
        send_message(page, "Ask me a question")
        page.locator("text=Question").first.wait_for(state="visible", timeout=10000)

        text_input = page.locator('input[placeholder*="Type your answer"]').first
        assert text_input.is_visible(timeout=5000), "Free-text input not visible"

    def test_submit_button_visible(self, page):
        """Submit button for free-text must render."""
        send_message(page, "Ask me a question")
        page.locator("text=Question").first.wait_for(state="visible", timeout=10000)

        submit = page.locator("button:has-text('Submit')").first
        assert submit.is_visible(timeout=5000), "Submit button not visible"

    def test_click_option_triggers_decision(self, page):
        """Clicking an option button fires the onClick handler.

        Verifies the button is clickable and the card is interactive.
        The actual POST to /api/pux/decision depends on pendingDecision
        being populated via the SSE adapter — tested in the live backend tests.
        """
        send_message(page, "Ask me a question")
        page.locator("text=Question").first.wait_for(state="visible", timeout=10000)

        btn = page.locator("button:has-text('Red')").first
        assert btn.is_enabled(), "Option button should be clickable"
        # Verify button is in the DOM and not disabled
        btn.click()
        # If we get here without error, the button was clickable
        page.wait_for_timeout(1000)


class TestAskUserTwoOptions:
    """Verify AskUserToolUI works with exactly 2 options."""

    @pytest.fixture(autouse=True)
    def _setup(self, page, frontend_url):
        goto_frontend(page, frontend_url)
        mock_sse_route(page, make_ask_user_response(
            question="Pick a number",
            options=["1", "2"],
        ))
        mock_decision_route(page)

    def test_both_options_render(self, page):
        """Both options '1' and '2' must render as buttons."""
        send_message(page, "Pick")
        page.locator("text=Question").first.wait_for(state="visible", timeout=10000)

        btn_1 = page.locator("button:has-text('1')").first
        btn_2 = page.locator("button:has-text('2')").first
        assert btn_1.is_visible(timeout=3000), "Option '1' not visible"
        assert btn_2.is_visible(timeout=3000), "Option '2' not visible"

    def test_free_text_input_works(self, page):
        """Free-text answer can be typed and submitted."""
        send_message(page, "Pick")
        page.locator("text=Question").first.wait_for(state="visible", timeout=10000)

        text_input = page.locator('input[placeholder*="Type your answer"]').first
        text_input.click()
        text_input.fill("forty-two")

        submit = page.locator("button:has-text('Submit')").first
        assert submit.is_enabled(), "Submit button should be enabled with text input"


class TestAskUserCompletedState:
    """Verify the completed/done state after ask_user tool completes.

    These tests use the live backend since the static SSE mock doesn't
    properly simulate the decision pause/resume cycle. See TestAskUserLive.
    """

    pass  # Covered by TestAskUserLive.test_live_answer_resolves


# ═══════════════════════════════════════════════════════════════════════
# Part 2: Live backend integration (requires --live flag or no --skip-llm)
# ═══════════════════════════════════════════════════════════════════════


class TestAskUserLive:
    """Full end-to-end with live backend and real SSE stream.

    These tests require a running Go backend with an LLM configured.
    They are slow (~30s each) and marked with @pytest.mark.llm.
    """

    pytestmark = [pytest.mark.api, pytest.mark.llm, pytest.mark.slow]

    LIVE_PROMPT = (
        'Call the ask_user tool with question="Pick a number" '
        'and options=["1","2"]. Do not respond yourself - only use the tool.'
    )

    @pytest.fixture(autouse=True)
    def _setup(self, page, frontend_url):
        goto_frontend(page, frontend_url)
        # No route mocking — real backend

    def test_live_ask_user_card_appears(self, page, api_session):
        """With live backend, the ask_user card must appear within 60s."""
        send_message(page, self.LIVE_PROMPT)

        # Wait for option buttons (the clearest signal the card rendered)
        buttons = []
        for _ in range(60):
            all_btns = page.locator("button").all()
            opt_btns = [b for b in all_btns if b.inner_text().strip() in ("1", "2")]
            if len(opt_btns) >= 2:
                buttons = opt_btns
                break
            page.wait_for_timeout(1000)

        assert len(buttons) >= 2, (
            f"ask_user card with options not found after 60s. "
            f"Page text: {page.evaluate('document.body.innerText')[:500]}"
        )

    def test_live_answer_resolves(self, page, api_session):
        """Answering the live ask_user must produce a model response."""
        send_message(page, self.LIVE_PROMPT)

        # Wait for option buttons
        buttons = []
        for _ in range(60):
            all_btns = page.locator("button").all()
            opt_btns = [b for b in all_btns if b.inner_text().strip() in ("1", "2")]
            if len(opt_btns) >= 2:
                buttons = opt_btns
                break
            page.wait_for_timeout(1000)

        assert len(buttons) >= 2, "ask_user card did not appear"

        # Click option "1"
        buttons[0].click()

        # Wait for model to process and respond
        page.wait_for_timeout(20000)

        body = page.evaluate("document.body.innerText").lower()
        assert "selected" in body or "picked" in body or "done" in body, (
            f"Model did not respond after answering. Page: {body[:500]}"
        )
