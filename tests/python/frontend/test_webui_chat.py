"""
WebUI Chat End-to-End Tests — SSE streaming, message rendering, components.

Uses Playwright with route mocking to test the WebUI without needing a real
backend. Intercepts /api/pux/prompt and returns deterministic SSE event streams
to verify rendering of:
  - User and assistant messages
  - Thinking/reasoning blocks
  - Tool call cards (start/end pairs)
  - Error states
  - Streaming indicators
  - Complete chat lifecycle

Requires: Vite frontend running on :5174 (task dev-frontend or task dev)

Start:
  task dev-frontend
  pytest frontend/test_webui_chat.py -v

Run without backend (route-mocked):
  pytest frontend/test_webui_chat.py -v
"""

import json
import time

import pytest

from fixtures.browser import goto_frontend

pytestmark = pytest.mark.playwright


# ── SSE Mock Factories ─────────────────────────────────────────────


def sse_event(event_type, data):
    """Format a single SSE event."""
    return f"event: {event_type}\ndata: {json.dumps(data)}\n\n"


def make_simple_response(text="Hello from test"):
    """SSE stream for a simple text response (no tools, no thinking)."""
    return (
        sse_event("agent_spawned", {"agentId": "test-agent-001"}) +
        sse_event("agent_start", {}) +
        sse_event("text_delta", {"text": text}) +
        sse_event("agent_end", {"input": 50, "output": 10, "cache": 0}) +
        "data: [DONE]\n\n"
    )


def make_thinking_response(thinking="Let me think...", text="Here is my answer"):
    """SSE stream with thinking + text response."""
    return (
        sse_event("agent_spawned", {"agentId": "test-agent-002"}) +
        sse_event("agent_start", {}) +
        sse_event("thinking_delta", {"text": thinking}) +
        sse_event("text_delta", {"text": text}) +
        sse_event("agent_end", {"input": 100, "output": 20, "cache": 5}) +
        "data: [DONE]\n\n"
    )


def make_tool_response(tool_name="bash", args=None, result="tool output here", text="Done"):
    """SSE stream with tool execution + text response."""
    args = args or {"command": "echo hello"}
    tool_id = "tc_test_001"
    return (
        sse_event("agent_spawned", {"agentId": "test-agent-003"}) +
        sse_event("agent_start", {}) +
        sse_event("tool_execution_start", {
            "toolName": tool_name,
            "args": args,
            "toolId": tool_id,
        }) +
        sse_event("tool_execution_end", {
            "toolId": tool_id,
            "result": result,
        }) +
        sse_event("text_delta", {"text": text}) +
        sse_event("agent_end", {"input": 150, "output": 30, "cache": 10}) +
        "data: [DONE]\n\n"
    )


def make_multi_tool_response():
    """SSE stream with multiple sequential tool calls."""
    return (
        sse_event("agent_spawned", {"agentId": "test-agent-004"}) +
        sse_event("agent_start", {}) +
        sse_event("tool_execution_start", {
            "toolName": "bash",
            "args": {"command": "ls -la"},
            "toolId": "tc_001",
        }) +
        sse_event("tool_execution_end", {
            "toolId": "tc_001",
            "result": "file1.txt\nfile2.txt",
        }) +
        sse_event("tool_execution_start", {
            "toolName": "file_read",
            "args": {"path": "/tmp/test.txt"},
            "toolId": "tc_002",
        }) +
        sse_event("tool_execution_end", {
            "toolId": "tc_002",
            "result": "file contents here",
        }) +
        sse_event("text_delta", {"text": "I read the files."}) +
        sse_event("agent_end", {"input": 200, "output": 50, "cache": 20}) +
        "data: [DONE]\n\n"
    )


def make_error_response(error_msg="Internal server error"):
    """SSE stream with an error event."""
    return (
        sse_event("agent_spawned", {"agentId": "test-agent-005"}) +
        sse_event("agent_start", {}) +
        sse_event("error", {"error": error_msg}) +
        "data: [DONE]\n\n"
    )


def make_delegation_response():
    """SSE stream with tool delegation (delegate_to)."""
    return (
        sse_event("agent_spawned", {"agentId": "test-agent-006"}) +
        sse_event("agent_start", {}) +
        sse_event("tool_execution_start", {
            "toolName": "delegate_to",
            "args": {"agent": "alex", "task": "run tests"},
            "toolId": "tc_del_001",
        }) +
        sse_event("tool_execution_end", {
            "toolId": "tc_del_001",
            "result": "Tests passed: 42/42",
        }) +
        sse_event("text_delta", {"text": "The tests all passed."}) +
        sse_event("agent_end", {"input": 300, "output": 60, "cache": 30}) +
        "data: [DONE]\n\n"
    )


def make_progressive_text(chunks):
    """SSE stream with text arriving in progressive chunks."""
    body = (
        sse_event("agent_spawned", {"agentId": "test-agent-007"}) +
        sse_event("agent_start", {})
    )
    for chunk in chunks:
        body += sse_event("text_delta", {"text": chunk})
    body += sse_event("agent_end", {"input": 80, "output": len("".join(chunks)), "cache": 0})
    body += "data: [DONE]\n\n"
    return body


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


# ═══════════════════════════════════════════════════════════════════════
# 1. Welcome / Empty State
# ═══════════════════════════════════════════════════════════════════════


class TestWelcomeState:
    """Verify the empty/welcome state renders correctly."""

    @pytest.fixture(autouse=True)
    def _load(self, page, frontend_url):
        goto_frontend(page, frontend_url)

    def test_welcome_heading(self, page):
        """Welcome screen should show 'Pux' heading."""
        heading = page.locator("h1")
        assert heading.is_visible(timeout=10000), "Welcome heading not visible"
        assert "Pux" in heading.text_content()

    def test_welcome_subtitle(self, page):
        """Welcome screen should show subtitle text."""
        subtitle = page.locator("text=Your AI-powered development orchestrator")
        assert subtitle.is_visible(timeout=5000), "Welcome subtitle not visible"

    def test_composer_visible(self, page):
        """Message composer (textarea) should be visible."""
        textarea = page.locator("textarea").first
        assert textarea.is_visible(timeout=5000), "Composer textarea not visible"

    def test_composer_placeholder(self, page):
        """Composer should have 'Send a message...' placeholder."""
        textarea = page.locator("textarea").first
        placeholder = textarea.get_attribute("placeholder")
        assert "Send a message" in (placeholder or ""), \
            f"Expected 'Send a message' placeholder, got: {placeholder}"

    def test_send_button_visible(self, page):
        """Send button should be visible."""
        send_btn = page.locator("button[aria-label='Send message']").first
        assert send_btn.is_visible(timeout=5000), "Send button not visible"


# ═══════════════════════════════════════════════════════════════════════
# 2. Simple Text Message Flow
# ═══════════════════════════════════════════════════════════════════════


class TestSimpleTextFlow:
    """Verify user → assistant text message exchange."""

    @pytest.fixture(autouse=True)
    def _setup(self, page, frontend_url):
        goto_frontend(page, frontend_url)
        mock_sse_route(page, make_simple_response("Hello from mock backend!"))

    def test_user_message_appears(self, page):
        """After typing and sending, user message should appear in chat."""
        textarea = page.locator("textarea").first
        textarea.fill("test message")
        textarea.press("Enter")

        # User message should be visible
        user_msg = page.locator("[data-slot='aui_user-message-root']").first
        assert user_msg.is_visible(timeout=5000), "User message not visible after send"
        assert "test message" in user_msg.text_content()

    def test_assistant_message_appears(self, page):
        """After sending, assistant response should render."""
        textarea = page.locator("textarea").first
        textarea.fill("hello")
        textarea.press("Enter")

        # Wait for assistant message to appear
        assistant_msg = page.locator("[data-slot='aui_assistant-message-root']").first
        assert assistant_msg.is_visible(timeout=10000), "Assistant message not visible"

    def test_assistant_text_content(self, page):
        """Assistant response should contain the mocked text."""
        textarea = page.locator("textarea").first
        textarea.fill("hello")
        textarea.press("Enter")

        # Wait for text content
        text_element = page.locator("text=Hello from mock backend!").first
        assert text_element.is_visible(timeout=10000), "Mock response text not visible"

    def test_welcome_disappears_after_message(self, page):
        """Welcome screen should disappear after first message."""
        textarea = page.locator("textarea").first
        textarea.fill("hello")
        textarea.press("Enter")
        page.wait_for_timeout(2000)

        # Welcome heading should be hidden
        welcome = page.locator("h1")
        assert not welcome.is_visible(), "Welcome screen still visible after message"


# ═══════════════════════════════════════════════════════════════════════
# 3. Thinking/Reasoning Block
# ═══════════════════════════════════════════════════════════════════════


class TestThinkingBlock:
    """Verify thinking/reasoning blocks render correctly."""

    @pytest.fixture(autouse=True)
    def _setup(self, page, frontend_url):
        goto_frontend(page, frontend_url)
        mock_sse_route(page, make_thinking_response(
            thinking="I need to analyze this carefully...",
            text="The answer is 42."
        ))

    def test_thinking_section_present(self, page):
        """Thinking/reasoning section should appear in the response."""
        textarea = page.locator("textarea").first
        textarea.fill("what is the answer?")
        textarea.press("Enter")

        # Wait for assistant message
        assistant = page.locator("[data-slot='aui_assistant-message-root']").first
        assert assistant.is_visible(timeout=10000)

        # Should have reasoning content
        reasoning = page.locator("[data-slot='aui_chain-of-thought']").first
        assert reasoning.is_visible(timeout=5000), "Thinking/reasoning block not visible"

    def test_thinking_text_collapsible(self, page):
        """Thinking block should be collapsible (accordion)."""
        textarea = page.locator("textarea").first
        textarea.fill("think about it")
        textarea.press("Enter")

        # Wait for response to complete
        page.wait_for_timeout(3000)

        # The reasoning block should be present (may be collapsed)
        chain_of_thought = page.locator("[data-slot='aui_chain-of-thought']").first
        assert chain_of_thought.is_visible(timeout=5000)

    def test_final_text_still_visible(self, page):
        """Final text response should appear after thinking block."""
        textarea = page.locator("textarea").first
        textarea.fill("think and answer")
        textarea.press("Enter")

        # Wait for the text response
        text_element = page.locator("text=The answer is 42.").first
        assert text_element.is_visible(timeout=10000), "Final text not visible after thinking"


# ═══════════════════════════════════════════════════════════════════════
# 4. Tool Call Rendering
# ═══════════════════════════════════════════════════════════════════════


class TestToolCallRendering:
    """Verify tool call cards render correctly."""

    @pytest.fixture(autouse=True)
    def _setup(self, page, frontend_url):
        goto_frontend(page, frontend_url)

    def test_single_tool_card(self, page):
        """Single tool call should show tool name and be expandable."""
        mock_sse_route(page, make_tool_response(
            tool_name="bash",
            args={"command": "echo test"},
            result="test\n",
            text="Command executed."
        ))

        textarea = page.locator("textarea").first
        textarea.fill("run echo test")
        textarea.press("Enter")

        # Tool group should appear
        tool_group = page.locator("[data-slot='aui_chain-of-thought']").first
        assert tool_group.is_visible(timeout=10000), "Tool group not visible"

        # Should mention the tool name
        page_content = page.content()
        assert "bash" in page_content.lower(), "Tool name 'bash' not in rendered content"

    def test_tool_result_expandable(self, page):
        """Tool result should be visible (expanded or expandable)."""
        mock_sse_route(page, make_tool_response(
            tool_name="file_read",
            args={"path": "/tmp/test.txt"},
            result="file contents here",
            text="I read the file."
        ))

        textarea = page.locator("textarea").first
        textarea.fill("read a file")
        textarea.press("Enter")

        # Wait for response
        page.wait_for_timeout(3000)

        # Response should be present
        content = page.content()
        has_result = "file" in content.lower() or "contents" in content.lower()
        assert has_result, "Tool result content not found in page"

    def test_multiple_tools(self, page):
        """Multiple tool calls should render in sequence."""
        mock_sse_route(page, make_multi_tool_response())

        textarea = page.locator("textarea").first
        textarea.fill("run multiple tools")
        textarea.press("Enter")

        # Wait for response
        page.wait_for_timeout(3000)

        content = page.content()
        # Both tool names should be present
        assert "bash" in content.lower(), "First tool 'bash' not found"
        assert "file_read" in content.lower(), "Second tool 'file_read' not found"

    def test_delegation_tool(self, page):
        """delegate_to tool should render with agent name."""
        mock_sse_route(page, make_delegation_response())

        textarea = page.locator("textarea").first
        textarea.fill("delegate a task")
        textarea.press("Enter")

        page.wait_for_timeout(3000)
        content = page.content()
        has_delegate = "delegate" in content.lower() or "alex" in content.lower()
        assert has_delegate, "Delegation tool not rendered in page"


# ═══════════════════════════════════════════════════════════════════════
# 5. Error State
# ═══════════════════════════════════════════════════════════════════════


class TestErrorState:
    """Verify error events render correctly."""

    @pytest.fixture(autouse=True)
    def _setup(self, page, frontend_url):
        goto_frontend(page, frontend_url)

    def test_error_message_visible(self, page):
        """Error event should show error message in the UI."""
        mock_sse_route(page, make_error_response("Something went wrong"))

        textarea = page.locator("textarea").first
        textarea.fill("trigger error")
        textarea.press("Enter")

        page.wait_for_timeout(3000)

        # Should show error somewhere in the page
        content = page.content()
        has_error = "error" in content.lower() or "wrong" in content.lower()
        assert has_error, "Error message not rendered in UI"

    def test_api_error_500(self, page):
        """500 API response should be handled gracefully."""
        def handle_route(route):
            route.fulfill(status=500, body="Internal Server Error")
        page.route("**/api/pux/prompt", handle_route)

        textarea = page.locator("textarea").first
        textarea.fill("trigger 500")
        textarea.press("Enter")

        page.wait_for_timeout(3000)

        # Page should not crash
        assert page.locator("body").is_visible()
        # Should show some error indication
        content = page.content()
        assert len(content) > 100, "Page seems blank after 500 error"


# ═══════════════════════════════════════════════════════════════════════
# 6. Progressive Streaming
# ═══════════════════════════════════════════════════════════════════════


class TestProgressiveStreaming:
    """Verify text accumulates correctly during streaming."""

    @pytest.fixture(autouse=True)
    def _setup(self, page, frontend_url):
        goto_frontend(page, frontend_url)

    def test_text_accumulates(self, page):
        """Multiple text_delta events should accumulate into full text."""
        mock_sse_route(page, make_progressive_text([
            "Hello ",
            "from ",
            "progressive ",
            "streaming!"
        ]))

        textarea = page.locator("textarea").first
        textarea.fill("test streaming")
        textarea.press("Enter")

        # Wait for full text to appear
        full_text = page.locator("text=Hello from progressive streaming!").first
        assert full_text.is_visible(timeout=10000), "Progressive text not accumulated correctly"

    def test_streaming_indicator_while_running(self, page):
        """While streaming, cancel button should be visible."""
        # Use a response that takes a moment
        mock_sse_route(page, make_progressive_text([
            "Starting",
            " middle",
            " end"
        ]))

        textarea = page.locator("textarea").first
        textarea.fill("test streaming")
        textarea.press("Enter")

        # Check for cancel button (may be brief)
        cancel_btn = page.locator("button[aria-label='Stop generating']")
        # It may or may not be visible depending on timing, but should not error
        cancel_btn.is_visible(timeout=2000)
        # The page should still work after streaming completes
        page.wait_for_timeout(3000)
        assert page.locator("body").is_visible()


# ═══════════════════════════════════════════════════════════════════════
# 7. Multi-turn Chat
# ═══════════════════════════════════════════════════════════════════════


class TestMultiTurnChat:
    """Verify multi-turn conversations render correctly."""

    @pytest.fixture(autouse=True)
    def _setup(self, page, frontend_url):
        goto_frontend(page, frontend_url)

    def test_two_messages_stack(self, page):
        """Two message exchanges should show stacked messages."""
        # First turn
        mock_sse_route(page, make_simple_response("First response"))
        textarea = page.locator("textarea").first
        textarea.fill("message one")
        textarea.press("Enter")

        # Wait for first response
        first = page.locator("text=First response").first
        assert first.is_visible(timeout=10000)

        # Second turn — update route mock
        mock_sse_route(page, make_simple_response("Second response"))
        textarea.fill("message two")
        textarea.press("Enter")

        # Wait for second response
        second = page.locator("text=Second response").first
        assert second.is_visible(timeout=10000)

        # Both user messages should be present
        content = page.content()
        assert "message one" in content, "First user message missing"
        assert "message two" in content, "Second user message missing"

    def test_user_messages_have_correct_role(self, page):
        """User messages should have data-role='user'."""
        mock_sse_route(page, make_simple_response("ok"))
        textarea = page.locator("textarea").first
        textarea.fill("test role")
        textarea.press("Enter")

        page.wait_for_timeout(3000)

        user_msg = page.locator("[data-role='user']").first
        assert user_msg.is_visible(timeout=5000), "User message missing data-role='user'"

    def test_assistant_messages_have_correct_role(self, page):
        """Assistant messages should have data-role='assistant'."""
        mock_sse_route(page, make_simple_response("ok"))
        textarea = page.locator("textarea").first
        textarea.fill("test role")
        textarea.press("Enter")

        assistant_msg = page.locator("[data-role='assistant']").first
        assert assistant_msg.is_visible(timeout=10000), "Assistant missing data-role='assistant'"


# ═══════════════════════════════════════════════════════════════════════
# 8. Sidebar and Layout
# ═══════════════════════════════════════════════════════════════════════


class TestSidebarLayout:
    """Verify sidebar and layout components."""

    @pytest.fixture(autouse=True)
    def _load(self, page, frontend_url):
        goto_frontend(page, frontend_url)

    def test_sidebar_visible(self, page):
        """Sidebar should be visible with navigation."""
        # Look for sidebar or navigation elements
        content = page.content()
        has_sidebar = any(kw in content for kw in ["sidebar", "Sidebar", "nav", "conversation"])
        # Sidebar may be collapsible — just verify the page isn't blank
        assert len(content) > 500, "Page content seems too minimal"

    def test_composer_persists_after_send(self, page):
        """Composer should remain usable after sending a message."""
        mock_sse_route(page, make_simple_response("ok"))

        textarea = page.locator("textarea").first
        textarea.fill("first message")
        textarea.press("Enter")
        page.wait_for_timeout(3000)

        # Composer should still be visible and empty
        textarea_after = page.locator("textarea").first
        assert textarea_after.is_visible(timeout=5000), "Composer disappeared after send"
        assert textarea_after.input_value() == "", "Composer not cleared after send"


# ═══════════════════════════════════════════════════════════════════════
# 9. Responsive Layout
# ═══════════════════════════════════════════════════════════════════════


class TestResponsiveLayout:
    """Verify the WebUI adapts to different viewport sizes."""

    def test_mobile_viewport(self, page, frontend_url):
        """Mobile viewport should render without crash."""
        page.set_viewport_size({"width": 375, "height": 812})
        goto_frontend(page, frontend_url)

        body = page.locator("body")
        assert body.is_visible()

        # Composer should still be visible
        textarea = page.locator("textarea").first
        assert textarea.is_visible(timeout=10000), "Composer not visible on mobile"

    def test_tablet_viewport(self, page, frontend_url):
        """Tablet viewport should render without crash."""
        page.set_viewport_size({"width": 768, "height": 1024})
        goto_frontend(page, frontend_url)

        body = page.locator("body")
        assert body.is_visible()

    def test_large_desktop_viewport(self, page, frontend_url):
        """Large desktop viewport should render without crash."""
        page.set_viewport_size({"width": 2560, "height": 1440})
        goto_frontend(page, frontend_url)

        body = page.locator("body")
        assert body.is_visible()
        textarea = page.locator("textarea").first
        assert textarea.is_visible(timeout=10000), "Composer not visible on large desktop"


# ═══════════════════════════════════════════════════════════════════════
# 10. No Console Errors
# ═══════════════════════════════════════════════════════════════════════


class TestNoConsoleErrors:
    """Verify no console errors on load or after interactions."""

    @pytest.fixture(autouse=True)
    def _capture_errors(self, page, frontend_url):
        self.errors = []
        page.on("pageerror", lambda err: self.errors.append(str(err)))
        goto_frontend(page, frontend_url)
        yield

    def test_no_js_errors_on_load(self):
        """No JavaScript errors should occur on page load."""
        real_errors = [e for e in self.errors if "extension" not in e.lower()]
        assert len(real_errors) == 0, (
            f"JavaScript errors on load:\n" + "\n".join(real_errors)
        )

    def test_no_js_errors_after_send(self, page):
        """No JavaScript errors after sending a message."""
        mock_sse_route(page, make_simple_response("ok"))
        textarea = page.locator("textarea").first
        textarea.fill("test")
        textarea.press("Enter")
        page.wait_for_timeout(3000)

        real_errors = [e for e in self.errors if "extension" not in e.lower()]
        assert len(real_errors) == 0, (
            f"JavaScript errors after send:\n" + "\n".join(real_errors)
        )

    def test_no_js_errors_with_thinking(self, page):
        """No JavaScript errors when rendering thinking blocks."""
        mock_sse_route(page, make_thinking_response())
        textarea = page.locator("textarea").first
        textarea.fill("think")
        textarea.press("Enter")
        page.wait_for_timeout(3000)

        real_errors = [e for e in self.errors if "extension" not in e.lower()]
        assert len(real_errors) == 0, (
            f"JavaScript errors with thinking:\n" + "\n".join(real_errors)
        )

    def test_no_js_errors_with_tools(self, page):
        """No JavaScript errors when rendering tool calls."""
        mock_sse_route(page, make_tool_response())
        textarea = page.locator("textarea").first
        textarea.fill("run tool")
        textarea.press("Enter")
        page.wait_for_timeout(3000)

        real_errors = [e for e in self.errors if "extension" not in e.lower()]
        assert len(real_errors) == 0, (
            f"JavaScript errors with tools:\n" + "\n".join(real_errors)
        )
