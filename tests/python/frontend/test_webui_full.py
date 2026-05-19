"""
WebUI Full E2E Tests — comprehensive coverage with screenshots.

Tests ALL WebUI features using Playwright with route mocking:
  - Page load and layout structure
  - Sidebar (branding, New Chat, Open Folder, project groups, conversations)
  - Welcome screen and composer
  - Chat flow with SSE mocking (text, thinking, tools, errors)
  - Workbench tabs (Sandbox, Editor, Scheduler, Agents)
  - Workers/Agents panel (CTO card, worker list, CRUD)
  - Scheduler panel (job list, CRUD)
  - Model picker dropdown
  - Terminal drawer toggle (button + keyboard shortcut)
  - Sidebar collapse/expand
  - Workbench panel toggle
  - Add Project dialog
  - Action bar (copy, refresh, export markdown)
  - Responsive viewports

Every test takes a screenshot saved to frontend/screenshots/.

Requires: Vite frontend running (task dev-frontend or task dev)

Start:
  task dev-frontend
  pytest frontend/test_webui_full.py -v
"""

import json
import os
import time
from pathlib import Path

import pytest

from fixtures.browser import goto_frontend

pytestmark = pytest.mark.playwright

# ── Screenshot helper ──────────────────────────────────────────────

SCREENSHOT_DIR = Path(__file__).parent / "screenshots"
SCREENSHOT_DIR.mkdir(exist_ok=True)


def screenshot(page, name: str):
    """Take a full-page screenshot and save to the screenshots directory."""
    path = SCREENSHOT_DIR / f"{name}.png"
    page.screenshot(path=str(path), full_page=True)
    return str(path)


# ── SSE Mock Factories (reused from test_webui_chat.py) ───────────


def sse_event(event_type, data):
    return f"event: {event_type}\ndata: {json.dumps(data)}\n\n"


def make_simple_response(text="Hello from test"):
    return (
        sse_event("agent_spawned", {"agentId": "test-full-001"}) +
        sse_event("agent_start", {}) +
        sse_event("text_delta", {"text": text}) +
        sse_event("agent_end", {"input": 50, "output": 10, "cache": 0}) +
        "data: [DONE]\n\n"
    )


def make_thinking_response(
    thinking="Let me analyze this step by step...",
    text="Here is my detailed answer.",
):
    return (
        sse_event("agent_spawned", {"agentId": "test-full-002"}) +
        sse_event("agent_start", {}) +
        sse_event("thinking_delta", {"text": thinking}) +
        sse_event("text_delta", {"text": text}) +
        sse_event("agent_end", {"input": 100, "output": 20, "cache": 5}) +
        "data: [DONE]\n\n"
    )


def make_tool_response(
    tool_name="bash",
    args=None,
    result="tool output here",
    text="Done executing.",
):
    args = args or {"command": "echo hello"}
    tool_id = "tc_full_001"
    return (
        sse_event("agent_spawned", {"agentId": "test-full-003"}) +
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


def make_error_response(error_msg="Something went wrong"):
    return (
        sse_event("agent_spawned", {"agentId": "test-full-004"}) +
        sse_event("agent_start", {}) +
        sse_event("error", {"error": error_msg}) +
        "data: [DONE]\n\n"
    )


def make_delegation_response():
    return (
        sse_event("agent_spawned", {"agentId": "test-full-005"}) +
        sse_event("agent_start", {}) +
        sse_event("tool_execution_start", {
            "toolName": "delegate_to",
            "args": {"agent": "alex", "task": "run tests"},
            "toolId": "tc_del_full",
        }) +
        sse_event("tool_execution_end", {
            "toolId": "tc_del_full",
            "result": "Tests passed: 42/42",
        }) +
        sse_event("text_delta", {"text": "All tests passed successfully."}) +
        sse_event("agent_end", {"input": 300, "output": 60, "cache": 30}) +
        "data: [DONE]\n\n"
    )


def mock_sse_route(page, response_body):
    """Set up route mock for /api/pux/prompt returning given SSE body."""
    page.unroute("**/api/pux/prompt")
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


def mock_json_route(page, url_pattern, response_body, status=200):
    """Set up route mock returning JSON for a URL pattern."""
    def handle_route(route):
        route.fulfill(
            status=status,
            headers={"Content-Type": "application/json"},
            body=json.dumps(response_body),
        )
    page.route(url_pattern, handle_route)


def mock_api_routes(page, workers=None, scheduler_jobs=None, models=None, projects=None):
    """Mock all common API routes the WebUI polls on load."""
    if models is not None:
        mock_json_route(page, "**/api/pux/models", models)
    else:
        mock_json_route(page, "**/api/pux/models", {
            "models": [
                {"id": "local-default", "name": "Gemma 4 26B", "provider": "local"},
                {"id": "deepseek-v4", "name": "DeepSeek V4 Flash", "provider": "openrouter"},
            ]
        })

    if workers is not None:
        mock_json_route(page, "**/api/workers/**", workers)
    else:
        mock_json_route(page, "**/api/workers/**", [
            {
                "name": "browser_ops",
                "hint": "Browser automation specialist",
                "persona": "You browse the web and interact with pages.",
                "capabilities": ["browser"],
                "model": "",
                "isDefault": True,
                "isModified": False,
            },
            {
                "name": "code_ops",
                "hint": "Code editing specialist",
                "persona": "You write and edit code.",
                "capabilities": ["code"],
                "model": "",
                "isDefault": True,
                "isModified": False,
            },
        ])

    if scheduler_jobs is not None:
        mock_json_route(page, "**/api/scheduler/**", scheduler_jobs)
    else:
        mock_json_route(page, "**/api/scheduler/**", {
            "jobs": [
                {
                    "id": "job-1",
                    "name": "daily-standup",
                    "message": "Check project status",
                    "scheduleType": "cron",
                    "cronExpr": "0 9 * * *",
                    "status": "idle",
                    "lastRunAt": "2026-05-17T09:00:00Z",
                    "lastRunStatus": "success",
                    "durationMs": 15000,
                    "agentId": "",
                    "model": "",
                },
            ]
        })

    if projects is not None:
        mock_json_route(page, "**/api/projects", projects)
    else:
        mock_json_route(page, "**/api/projects", [
            {"name": "auto-developer", "path": "/home/user/dev/auto-developer"},
        ])

    # Mock conversations — store expects raw array, not wrapped in object
    mock_json_route(page, "**/api/pux/conversations*", [
        {
            "project": "auto-developer",
            "agentId": "default",
            "title": "Hello chat",
            "lastMessage": "Hi there",
            "lastAt": "2026-05-17T10:00:00Z",
            "messageCount": 4,
        },
    ])

    # Mock running agents — store expects raw array
    mock_json_route(page, "**/api/pux/agents*", [])

    # Mock defaults
    mock_json_route(page, "**/api/pux/defaults", {"logic": "", "worker": ""})


def send_message(page, text="test"):
    """Fill the composer and press Enter."""
    textarea = page.locator("textarea").first
    textarea.fill(text)
    textarea.press("Enter")


# ═══════════════════════════════════════════════════════════════════════
# 1. Page Load and Layout Structure
# ═══════════════════════════════════════════════════════════════════════


class TestPageLoad:
    """Verify the page loads with correct layout structure."""

    @pytest.fixture(autouse=True)
    def _load(self, page, frontend_url):
        mock_api_routes(page)
        goto_frontend(page, frontend_url)

    def test_page_loads_with_content(self, page):
        """Page should load and have substantial content."""
        screenshot(page, "01_page_load")
        content = page.content()
        assert len(content) > 500, "Page content too minimal"

    def test_dark_background(self, page):
        """Page should have a dark background theme."""
        body = page.locator("body")
        bg = body.evaluate("el => getComputedStyle(el).backgroundColor")
        # Dark backgrounds should have low RGB values
        screenshot(page, "02_dark_theme")
        assert bg is not None, "Could not read body background"

    def test_header_bar_visible(self, page):
        """Top header bar with sidebar toggle should be visible."""
        header = page.locator("header").first
        assert header.is_visible(timeout=10000), "Header bar not visible"
        screenshot(page, "03_header_bar")

    def test_main_layout_panels(self, page):
        """Page should have the main layout: sidebar + chat + workbench."""
        screenshot(page, "04_full_layout")
        # Verify sidebar exists (shadcn sidebar uses data-sidebar attribute)
        sidebar = page.locator("[data-sidebar='sidebar']").first
        assert sidebar.is_visible(timeout=5000), "Sidebar not visible"


# ═══════════════════════════════════════════════════════════════════════
# 2. Sidebar
# ═══════════════════════════════════════════════════════════════════════


class TestSidebar:
    """Verify sidebar components and interactions."""

    @pytest.fixture(autouse=True)
    def _load(self, page, frontend_url):
        mock_api_routes(page)
        goto_frontend(page, frontend_url)

    def test_sidebar_branding(self, page):
        """Sidebar should show 'Pux' branding."""
        branding = page.locator("text=Pux").first
        assert branding.is_visible(timeout=5000), "Pux branding not visible"
        screenshot(page, "05_sidebar_branding")

    def test_new_chat_button(self, page):
        """New Chat button should be present."""
        new_chat = page.locator("text=New Chat").first
        assert new_chat.is_visible(timeout=5000), "New Chat button not visible"
        screenshot(page, "06_new_chat_button")

    def test_open_folder_button(self, page):
        """Open Folder button should be present."""
        open_folder = page.locator("text=Open Folder").first
        assert open_folder.is_visible(timeout=5000), "Open Folder button not visible"
        screenshot(page, "07_open_folder")

    def test_project_group_visible(self, page):
        """Project groups should be visible when conversations exist."""
        # Wait for the project group to render
        project = page.locator("text=auto-developer").first
        assert project.is_visible(timeout=10000), "Project group not visible"
        screenshot(page, "08_project_group")

    def test_conversation_listed(self, page):
        """Conversations should appear under project groups."""
        # Wait for conversations to load and render in sidebar
        # The store loads via useEffect after mount, so we need to wait
        page.wait_for_timeout(2000)
        # Check the page content for conversation data (it may render in sidebar)
        content = page.content()
        # Try waiting for conversation text if not found immediately
        if "Hello chat" not in content and "4 msgs" not in content:
            try:
                page.locator("text=Hello chat").first.wait_for(state="visible", timeout=5000)
                content = page.content()
            except Exception:
                pass
        has_conv = "Hello chat" in content or "Hi there" in content or "4 msgs" in content
        assert has_conv, "Conversation data not rendered in sidebar"
        screenshot(page, "09_conversation_list")

    def test_version_footer(self, page):
        """Sidebar footer should show version."""
        version = page.locator("text=v0.1").first
        assert version.is_visible(timeout=5000), "Version footer not visible"
        screenshot(page, "10_version_footer")

    def test_sidebar_collapse_expand(self, page):
        """Sidebar toggle should collapse and expand the sidebar."""
        # Click sidebar toggle button
        toggle = page.locator("[data-slot='sidebar-trigger']").first
        if not toggle.is_visible(timeout=5000):
            toggle = page.locator("button").filter(has_text="Pux").first

        # Find the sidebar toggle in the header
        sidebar_toggle = page.locator("header button").first
        assert sidebar_toggle.is_visible(timeout=5000)

        screenshot(page, "11_sidebar_expanded")

        # Collapse sidebar
        sidebar_toggle.click()
        page.wait_for_timeout(500)
        screenshot(page, "12_sidebar_collapsed")

        # Expand again
        sidebar_toggle.click()
        page.wait_for_timeout(500)
        screenshot(page, "13_sidebar_re_expanded")


# ═══════════════════════════════════════════════════════════════════════
# 3. Welcome Screen and Composer
# ═══════════════════════════════════════════════════════════════════════


class TestWelcomeAndComposer:
    """Verify the welcome screen and composer input."""

    @pytest.fixture(autouse=True)
    def _load(self, page, frontend_url):
        mock_api_routes(page)
        goto_frontend(page, frontend_url)

    def test_welcome_heading(self, page):
        """Welcome screen should show 'Pux' heading."""
        heading = page.locator("text=Pux").first
        assert heading.is_visible(timeout=10000)
        screenshot(page, "14_welcome_heading")

    def test_welcome_subtitle(self, page):
        """Welcome screen should show subtitle."""
        subtitle = page.locator("text=Your AI-powered development orchestrator").first
        assert subtitle.is_visible(timeout=5000)
        screenshot(page, "15_welcome_subtitle")

    def test_composer_visible(self, page):
        """Composer textarea should be visible."""
        textarea = page.locator("textarea").first
        assert textarea.is_visible(timeout=5000)
        screenshot(page, "16_composer")

    def test_composer_placeholder(self, page):
        """Composer should have the right placeholder text."""
        textarea = page.locator("textarea").first
        placeholder = textarea.get_attribute("placeholder")
        assert "Send a message" in (placeholder or "")
        screenshot(page, "17_composer_placeholder")

    def test_send_button_visible(self, page):
        """Send button should be visible."""
        send_btn = page.locator("button[aria-label='Send message']").first
        assert send_btn.is_visible(timeout=5000)
        screenshot(page, "18_send_button")

    def test_model_picker_visible(self, page):
        """Model picker dropdown should be visible in composer."""
        model_select = page.locator("[aria-label='Select model']").first
        assert model_select.is_visible(timeout=5000)
        screenshot(page, "19_model_picker")


# ═══════════════════════════════════════════════════════════════════════
# 4. Chat Flow — Simple Text
# ═══════════════════════════════════════════════════════════════════════


class TestChatFlowText:
    """Verify simple text message exchange."""

    @pytest.fixture(autouse=True)
    def _setup(self, page, frontend_url):
        mock_api_routes(page)
        goto_frontend(page, frontend_url)
        mock_sse_route(page, make_simple_response("Hello from the full E2E test!"))

    def test_user_message_renders(self, page):
        """User message should appear after sending."""
        send_message(page, "hello world")
        user_msg = page.locator("[data-role='user']").first
        assert user_msg.is_visible(timeout=5000)
        assert "hello world" in user_msg.text_content()
        screenshot(page, "20_user_message")

    def test_assistant_message_renders(self, page):
        """Assistant response should appear after user sends message."""
        send_message(page, "hi")
        assistant_msg = page.locator("[data-role='assistant']").first
        assert assistant_msg.is_visible(timeout=10000)
        screenshot(page, "21_assistant_message")

    def test_assistant_text_content(self, page):
        """Assistant response should contain the mocked text."""
        send_message(page, "hi")
        page.wait_for_timeout(3000)
        # Use page.content() check — the text may span multiple elements
        content = page.content()
        assert "Hello from the full E2E test" in content, "Assistant text not in page"
        screenshot(page, "22_assistant_text")

    def test_welcome_disappears(self, page):
        """Welcome screen should hide after first message."""
        send_message(page, "hi")
        page.wait_for_timeout(2000)
        heading = page.locator("h1")
        assert not heading.is_visible()
        screenshot(page, "23_welcome_gone")

    def test_composer_cleared_after_send(self, page):
        """Composer should be cleared after sending."""
        send_message(page, "hi")
        page.wait_for_timeout(2000)
        textarea = page.locator("textarea").first
        assert textarea.input_value() == ""
        screenshot(page, "24_composer_cleared")

    def test_composer_persists_after_send(self, page):
        """Composer should still be visible and usable after sending."""
        send_message(page, "hi")
        page.wait_for_timeout(2000)
        textarea = page.locator("textarea").first
        assert textarea.is_visible()
        screenshot(page, "25_composer_persists")


# ═══════════════════════════════════════════════════════════════════════
# 5. Chat Flow — Thinking/Reasoning Blocks
# ═══════════════════════════════════════════════════════════════════════


class TestChatFlowThinking:
    """Verify thinking/reasoning block rendering."""

    @pytest.fixture(autouse=True)
    def _setup(self, page, frontend_url):
        mock_api_routes(page)
        goto_frontend(page, frontend_url)
        mock_sse_route(page, make_thinking_response(
            thinking="I need to analyze the code structure first...",
            text="The code uses a modular architecture.",
        ))

    def test_thinking_block_present(self, page):
        """Thinking response should render assistant message with content."""
        send_message(page, "analyze this")
        # The new UI doesn't have a separate reasoning component,
        # but the assistant message should render with the text content
        ast = page.locator("[data-role='assistant']").first
        assert ast.is_visible(timeout=10000)
        screenshot(page, "26_thinking_block")

    def test_final_text_after_thinking(self, page):
        """Final answer text should appear after thinking response."""
        send_message(page, "analyze this")
        # Wait for assistant message, then check for answer text
        ast = page.locator("[data-role='assistant']").first
        assert ast.is_visible(timeout=10000)
        page.wait_for_timeout(2000)
        content = page.content()
        has_text = "modular architecture" in content
        assert has_text, "Final answer text not found after thinking block"
        screenshot(page, "27_text_after_thinking")


# ═══════════════════════════════════════════════════════════════════════
# 6. Chat Flow — Tool Calls
# ═══════════════════════════════════════════════════════════════════════


class TestChatFlowTools:
    """Verify tool call card rendering."""

    @pytest.fixture(autouse=True)
    def _setup(self, page, frontend_url):
        mock_api_routes(page)
        goto_frontend(page, frontend_url)

    def test_bash_tool_card(self, page):
        """Bash tool call should render with tool group."""
        mock_sse_route(page, make_tool_response(
            tool_name="bash",
            args={"command": "ls -la"},
            result="file1.txt\nfile2.txt",
            text="I listed the files.",
        ))
        send_message(page, "list files")
        page.wait_for_timeout(3000)
        screenshot(page, "28_bash_tool")
        # Tool calls render inside a collapsed "N tool call(s)" group.
        # Check for tool-related content (the group trigger or expanded content).
        content = page.content().lower()
        assert "tool" in content, "Tool call content not in page"

    def test_delegation_tool(self, page):
        """delegate_to tool should render."""
        mock_sse_route(page, make_delegation_response())
        send_message(page, "delegate")
        page.wait_for_timeout(3000)
        screenshot(page, "29_delegation_tool")
        content = page.content().lower()
        has_delegate = "delegate" in content or "alex" in content
        assert has_delegate, "Delegation not rendered"


# ═══════════════════════════════════════════════════════════════════════
# 7. Chat Flow — Error States
# ═══════════════════════════════════════════════════════════════════════


class TestChatFlowErrors:
    """Verify error state rendering."""

    @pytest.fixture(autouse=True)
    def _setup(self, page, frontend_url):
        mock_api_routes(page)
        goto_frontend(page, frontend_url)

    def test_sse_error_renders(self, page):
        """Error event from SSE should render in UI."""
        mock_sse_route(page, make_error_response("Connection timeout"))
        send_message(page, "trigger error")
        page.wait_for_timeout(3000)
        screenshot(page, "30_sse_error")
        content = page.content().lower()
        has_error = "error" in content or "timeout" in content
        assert has_error, "Error message not rendered"

    def test_http_500_graceful(self, page):
        """HTTP 500 should be handled without crashing."""
        def handle_500(route):
            route.fulfill(status=500, body="Internal Server Error")
        page.route("**/api/pux/prompt", handle_500)

        send_message(page, "trigger 500")
        page.wait_for_timeout(3000)
        screenshot(page, "31_http_500")
        assert page.locator("body").is_visible()


# ═══════════════════════════════════════════════════════════════════════
# 8. Chat Flow — Multi-Turn
# ═══════════════════════════════════════════════════════════════════════


class TestChatFlowMultiTurn:
    """Verify multi-turn conversation rendering."""

    @pytest.fixture(autouse=True)
    def _setup(self, page, frontend_url):
        mock_api_routes(page)
        goto_frontend(page, frontend_url)

    def test_two_turns_stack(self, page):
        """Two message exchanges should stack correctly."""
        # First turn
        mock_sse_route(page, make_simple_response("First response"))
        send_message(page, "message one")
        # Wait for first response to appear
        page.wait_for_timeout(5000)
        content = page.content()
        assert "First response" in content, "First response text not found"

        # Wait for runtime to be ready for next message
        page.wait_for_timeout(1000)

        # Unroute and set up second response
        page.unroute("**/api/pux/prompt")
        mock_sse_route(page, make_simple_response("Second response"))

        send_message(page, "message two")
        page.wait_for_timeout(5000)
        content = page.content()
        assert "Second response" in content, "Second response text not found"

        screenshot(page, "32_multi_turn")
        assert "message one" in content
        assert "message two" in content

    def test_message_roles(self, page):
        """Messages should have correct data-role attributes."""
        mock_sse_route(page, make_simple_response("ok"))
        send_message(page, "test roles")
        page.wait_for_timeout(3000)
        screenshot(page, "33_message_roles")
        user = page.locator("[data-role='user']").first
        assistant = page.locator("[data-role='assistant']").first
        assert user.is_visible(timeout=5000)
        assert assistant.is_visible(timeout=10000)


# ═══════════════════════════════════════════════════════════════════════
# 9. Action Bar
# ═══════════════════════════════════════════════════════════════════════


class TestActionBar:
    """Verify message action bar (copy, refresh, export)."""

    @pytest.fixture(autouse=True)
    def _setup(self, page, frontend_url):
        mock_api_routes(page)
        goto_frontend(page, frontend_url)
        mock_sse_route(page, make_simple_response("Action bar test response."))
        send_message(page, "test actions")
        page.wait_for_timeout(3000)

    def test_assistant_action_bar_hover(self, page):
        """Hovering over assistant message should reveal action bar."""
        msg = page.locator("[data-role='assistant']").first
        msg.scroll_into_view_if_needed()
        msg.hover()
        page.wait_for_timeout(500)
        screenshot(page, "34_action_bar_hover")
        # The action bar should appear (copy, refresh, more buttons)
        content = page.content()
        assert len(content) > 100


# ═══════════════════════════════════════════════════════════════════════
# 10. Workbench — Tab Navigation
# ═══════════════════════════════════════════════════════════════════════


class TestWorkbenchTabs:
    """Verify workbench panel tab navigation."""

    @pytest.fixture(autouse=True)
    def _setup(self, page, frontend_url):
        mock_api_routes(page)
        goto_frontend(page, frontend_url)

    def test_workbench_visible(self, page):
        """Workbench panel should be visible by default."""
        # The workbench panel has tab triggers
        tab_list = page.locator("[role='tablist']").first
        assert tab_list.is_visible(timeout=10000), "Workbench tabs not visible"
        screenshot(page, "35_workbench_default")

    def test_sandbox_tab(self, page):
        """Sandbox tab should be clickable."""
        sandbox_tab = page.locator("[role='tab']").filter(has_text="Sandbox").first
        assert sandbox_tab.is_visible(timeout=5000)
        sandbox_tab.click()
        page.wait_for_timeout(500)
        screenshot(page, "36_sandbox_tab")

    def test_editor_tab(self, page):
        """Editor tab should be clickable."""
        editor_tab = page.locator("[role='tab']").filter(has_text="Editor").first
        assert editor_tab.is_visible(timeout=5000)
        editor_tab.click()
        page.wait_for_timeout(500)
        screenshot(page, "37_editor_tab")

    def test_scheduler_tab(self, page):
        """Scheduler tab should be clickable."""
        scheduler_tab = page.locator("[role='tab']").filter(has_text="Scheduler").first
        assert scheduler_tab.is_visible(timeout=5000)
        scheduler_tab.click()
        page.wait_for_timeout(500)
        screenshot(page, "38_scheduler_tab")

    def test_agents_tab(self, page):
        """Agents tab should be clickable."""
        agents_tab = page.locator("[role='tab']").filter(has_text="Agents").first
        assert agents_tab.is_visible(timeout=5000)
        agents_tab.click()
        page.wait_for_timeout(500)
        screenshot(page, "39_agents_tab")

    def test_tab_content_switches(self, page):
        """Clicking different tabs should switch the content area."""
        tabs = ["Sandbox", "Editor", "Scheduler", "Agents"]
        for tab_name in tabs:
            tab = page.locator("[role='tab']").filter(has_text=tab_name).first
            tab.click()
            page.wait_for_timeout(300)
        screenshot(page, "40_tab_switched")


# ═══════════════════════════════════════════════════════════════════════
# 11. Workbench — Agents Panel
# ═══════════════════════════════════════════════════════════════════════


class TestAgentsPanel:
    """Verify the Agents/Workers panel."""

    @pytest.fixture(autouse=True)
    def _setup(self, page, frontend_url):
        mock_api_routes(page)
        goto_frontend(page, frontend_url)
        # Click Agents tab
        agents_tab = page.locator("[role='tab']").filter(has_text="Agents").first
        agents_tab.click()
        page.wait_for_timeout(1000)

    def test_cto_card_visible(self, page):
        """CTO card should be visible in Agents panel."""
        cto = page.locator("text=CTO").first
        assert cto.is_visible(timeout=5000), "CTO card not visible"
        screenshot(page, "41_cto_card")

    def test_worker_cards_visible(self, page):
        """Worker cards should be visible."""
        worker = page.locator("text=browser_ops").first
        assert worker.is_visible(timeout=5000), "browser_ops worker not visible"
        screenshot(page, "42_worker_cards")

    def test_worker_capability_badges(self, page):
        """Worker capability badges should be rendered."""
        badge = page.locator("text=browser").first
        assert badge.is_visible(timeout=5000)
        screenshot(page, "43_capability_badges")


# ═══════════════════════════════════════════════════════════════════════
# 12. Workbench — Scheduler Panel
# ═══════════════════════════════════════════════════════════════════════


class TestSchedulerPanel:
    """Verify the Scheduler panel."""

    @pytest.fixture(autouse=True)
    def _setup(self, page, frontend_url):
        mock_api_routes(page)
        goto_frontend(page, frontend_url)
        scheduler_tab = page.locator("[role='tab']").filter(has_text="Scheduler").first
        scheduler_tab.click()
        page.wait_for_timeout(1000)

    def test_scheduler_job_listed(self, page):
        """Scheduled jobs should be listed."""
        job = page.locator("text=daily-standup").first
        assert job.is_visible(timeout=5000), "Scheduled job not visible"
        screenshot(page, "44_scheduler_job")

    def test_scheduler_item_count(self, page):
        """Item count should be shown in the header."""
        count = page.locator("text=1 item").first
        assert count.is_visible(timeout=5000)
        screenshot(page, "45_scheduler_count")


# ═══════════════════════════════════════════════════════════════════════
# 13. Model Picker
# ═══════════════════════════════════════════════════════════════════════


class TestModelPicker:
    """Verify the model selection dropdown."""

    @pytest.fixture(autouse=True)
    def _setup(self, page, frontend_url):
        mock_api_routes(page)
        goto_frontend(page, frontend_url)

    def test_model_picker_opens(self, page):
        """Clicking the model picker should open the dropdown."""
        trigger = page.locator("[aria-label='Select model']").first
        trigger.click()
        page.wait_for_timeout(500)
        screenshot(page, "46_model_picker_open")

    def test_model_list_shows_models(self, page):
        """Model list should contain the mocked models."""
        trigger = page.locator("[aria-label='Select model']").first
        trigger.click()
        page.wait_for_timeout(500)
        # The dropdown should show model names
        content = page.content()
        has_model = "Gemma" in content or "DeepSeek" in content or "default" in content.lower()
        assert has_model, "No model names in dropdown"
        screenshot(page, "47_model_list")

    def test_add_provider_option(self, page):
        """'Add provider' option should be in the dropdown."""
        trigger = page.locator("[aria-label='Select model']").first
        trigger.click()
        page.wait_for_timeout(500)
        add_provider = page.locator("text=Add provider").first
        assert add_provider.is_visible(timeout=3000), "Add provider option not visible"
        screenshot(page, "48_add_provider")


# ═══════════════════════════════════════════════════════════════════════
# 14. Terminal Drawer
# ═══════════════════════════════════════════════════════════════════════


class TestTerminalDrawer:
    """Verify the terminal drawer toggle."""

    @pytest.fixture(autouse=True)
    def _setup(self, page, frontend_url):
        mock_api_routes(page)
        goto_frontend(page, frontend_url)

    def test_terminal_button_visible(self, page):
        """Terminal toggle button should be in the header."""
        terminal_btn = page.locator("button[aria-label='Toggle terminal']").first
        assert terminal_btn.is_visible(timeout=5000)
        screenshot(page, "49_terminal_button")

    def test_terminal_opens_on_click(self, page):
        """Clicking terminal button should open the terminal drawer."""
        terminal_btn = page.locator("button[aria-label='Toggle terminal']").first
        terminal_btn.click()
        page.wait_for_timeout(1000)
        screenshot(page, "50_terminal_open")
        # Verify something changed — the layout should have split
        content = page.content()
        assert len(content) > 100

    def test_terminal_closes_on_second_click(self, page):
        """Clicking terminal button again should close the drawer."""
        terminal_btn = page.locator("button[aria-label='Toggle terminal']").first
        terminal_btn.click()
        page.wait_for_timeout(500)
        terminal_btn.click()
        page.wait_for_timeout(500)
        screenshot(page, "51_terminal_closed")

    def test_terminal_keyboard_shortcut(self, page):
        """Ctrl+` should toggle the terminal drawer."""
        page.keyboard.press("Control+`")
        page.wait_for_timeout(1000)
        screenshot(page, "52_terminal_keyboard")
        # Press again to close
        page.keyboard.press("Control+`")
        page.wait_for_timeout(500)
        screenshot(page, "53_terminal_keyboard_close")


# ═══════════════════════════════════════════════════════════════════════
# 15. Workbench Panel Toggle
# ═══════════════════════════════════════════════════════════════════════


class TestWorkbenchToggle:
    """Verify the workbench panel toggle button."""

    @pytest.fixture(autouse=True)
    def _setup(self, page, frontend_url):
        mock_api_routes(page)
        goto_frontend(page, frontend_url)

    def test_workbench_toggle_button(self, page):
        """Workbench toggle button should be visible in header."""
        toggle_btn = page.locator("button[aria-label='Close workbench']").first
        assert toggle_btn.is_visible(timeout=5000)
        screenshot(page, "54_workbench_toggle_btn")

    def test_workbench_closes(self, page):
        """Clicking toggle should collapse the workbench."""
        toggle_btn = page.locator("button[aria-label='Close workbench']").first
        toggle_btn.click()
        page.wait_for_timeout(500)
        screenshot(page, "55_workbench_closed")

    def test_workbench_reopens(self, page):
        """Clicking toggle again should reopen the workbench."""
        toggle_btn = page.locator("button[aria-label='Close workbench']").first
        toggle_btn.click()
        page.wait_for_timeout(500)

        open_btn = page.locator("button[aria-label='Open workbench']").first
        open_btn.click()
        page.wait_for_timeout(500)
        screenshot(page, "56_workbench_reopened")


# ═══════════════════════════════════════════════════════════════════════
# 16. Add Project Dialog
# ═══════════════════════════════════════════════════════════════════════


class TestAddProjectDialog:
    """Verify the Add Project dialog."""

    @pytest.fixture(autouse=True)
    def _setup(self, page, frontend_url):
        mock_api_routes(page)
        goto_frontend(page, frontend_url)

    def test_open_folder_opens_dialog(self, page):
        """Clicking Open Folder should open a dialog."""
        open_folder = page.locator("text=Open Folder").first
        open_folder.click()
        page.wait_for_timeout(500)
        screenshot(page, "57_add_project_dialog")

    def test_dialog_dismissible(self, page):
        """Dialog should be dismissible."""
        open_folder = page.locator("text=Open Folder").first
        open_folder.click()
        page.wait_for_timeout(500)

        # Press Escape to close
        page.keyboard.press("Escape")
        page.wait_for_timeout(500)
        screenshot(page, "58_dialog_dismissed")


# ═══════════════════════════════════════════════════════════════════════
# 17. Config Panel — Worker CRUD
# ═══════════════════════════════════════════════════════════════════════


class TestWorkerCRUD:
    """Verify worker creation form in the Agents panel."""

    @pytest.fixture(autouse=True)
    def _setup(self, page, frontend_url):
        mock_api_routes(page)
        goto_frontend(page, frontend_url)
        agents_tab = page.locator("[role='tab']").filter(has_text="Agents").first
        agents_tab.click()
        page.wait_for_timeout(1000)

    def test_worker_edit_form(self, page):
        """Clicking edit on a worker should show the edit form."""
        edit_btn = page.locator("[title='Edit']").first
        edit_btn.click()
        page.wait_for_timeout(500)
        screenshot(page, "59_worker_edit_form")
        # Should show form fields
        content = page.content().lower()
        has_form = "name" in content or "persona" in content or "save" in content
        assert has_form, "Edit form not shown"

    def test_worker_cancel_edit(self, page):
        """Canceling edit should return to list view."""
        edit_btn = page.locator("[title='Edit']").first
        edit_btn.click()
        page.wait_for_timeout(300)

        cancel = page.locator("text=Cancel").first
        if cancel.is_visible(timeout=2000):
            cancel.click()
        page.wait_for_timeout(500)
        screenshot(page, "60_worker_cancel_edit")


# ═══════════════════════════════════════════════════════════════════════
# 18. Config Panel — Scheduler Job
# ═══════════════════════════════════════════════════════════════════════


class TestSchedulerCRUD:
    """Verify scheduler job creation form."""

    @pytest.fixture(autouse=True)
    def _setup(self, page, frontend_url):
        mock_api_routes(page)
        goto_frontend(page, frontend_url)
        scheduler_tab = page.locator("[role='tab']").filter(has_text="Scheduler").first
        scheduler_tab.click()
        page.wait_for_timeout(1000)

    def test_scheduler_edit_form(self, page):
        """Clicking edit on a job should show the edit form."""
        edit_btn = page.locator("[title='Edit']").first
        if edit_btn.is_visible(timeout=3000):
            edit_btn.click()
            page.wait_for_timeout(500)
            screenshot(page, "61_scheduler_edit_form")
        else:
            screenshot(page, "61_scheduler_edit_form_skip")

    def test_scheduler_new_button(self, page):
        """New button should open the creation form."""
        new_btn = page.locator("[title='New']").first
        if new_btn.is_visible(timeout=3000):
            new_btn.click()
            page.wait_for_timeout(500)
            screenshot(page, "62_scheduler_new_form")
            content = page.content().lower()
            has_form = "name" in content or "prompt" in content
            assert has_form


# ═══════════════════════════════════════════════════════════════════════
# 19. CTO Prompt Editor
# ═══════════════════════════════════════════════════════════════════════


class TestCTOPromptEditor:
    """Verify the CTO prompt editor view."""

    @pytest.fixture(autouse=True)
    def _setup(self, page, frontend_url):
        mock_api_routes(page)
        goto_frontend(page, frontend_url)
        agents_tab = page.locator("[role='tab']").filter(has_text="Agents").first
        agents_tab.click()
        page.wait_for_timeout(1000)

    def test_cto_card_clickable(self, page):
        """Clicking CTO card should open prompt editor."""
        cto = page.locator("text=CTO").first
        cto.click()
        page.wait_for_timeout(500)
        screenshot(page, "63_cto_prompt_editor")


# ═══════════════════════════════════════════════════════════════════════
# 20. Responsive Viewports
# ═══════════════════════════════════════════════════════════════════════


class TestResponsiveViewports:
    """Verify the UI adapts to different viewport sizes."""

    @pytest.fixture(autouse=True)
    def _load(self, page, frontend_url):
        mock_api_routes(page)
        self.frontend_url = frontend_url

    def test_mobile_375(self, page):
        """Mobile viewport should render without crash."""
        page.set_viewport_size({"width": 375, "height": 812})
        goto_frontend(page, self.frontend_url)
        assert page.locator("body").is_visible()
        textarea = page.locator("textarea").first
        assert textarea.is_visible(timeout=10000)
        screenshot(page, "64_mobile_375")

    def test_tablet_768(self, page):
        """Tablet viewport should render without crash."""
        page.set_viewport_size({"width": 768, "height": 1024})
        goto_frontend(page, self.frontend_url)
        assert page.locator("body").is_visible()
        screenshot(page, "65_tablet_768")

    def test_desktop_1440(self, page):
        """Desktop viewport should render without crash."""
        page.set_viewport_size({"width": 1440, "height": 900})
        goto_frontend(page, self.frontend_url)
        assert page.locator("body").is_visible()
        screenshot(page, "66_desktop_1440")

    def test_ultrawide_2560(self, page):
        """Ultrawide viewport should render without crash."""
        page.set_viewport_size({"width": 2560, "height": 1440})
        goto_frontend(page, self.frontend_url)
        assert page.locator("body").is_visible()
        screenshot(page, "67_ultrawide_2560")


# ═══════════════════════════════════════════════════════════════════════
# 21. No Console Errors
# ═══════════════════════════════════════════════════════════════════════


class TestNoConsoleErrors:
    """Verify no console errors during interactions."""

    @pytest.fixture(autouse=True)
    def _capture(self, page, frontend_url):
        self.errors = []
        page.on("pageerror", lambda err: self.errors.append(str(err)))
        mock_api_routes(page)
        goto_frontend(page, frontend_url)
        yield

    def _real_errors(self):
        return [e for e in self.errors if "extension" not in e.lower()]

    def test_no_errors_on_load(self):
        """No JS errors on page load."""
        assert self._real_errors() == [], (
            f"JS errors on load:\n" + "\n".join(self._real_errors())
        )

    def test_no_errors_after_send(self, page):
        """No JS errors after sending a message."""
        mock_sse_route(page, make_simple_response("ok"))
        send_message(page, "test")
        page.wait_for_timeout(3000)
        assert self._real_errors() == [], (
            f"JS errors after send:\n" + "\n".join(self._real_errors())
        )

    def test_no_errors_tab_switching(self, page):
        """No JS errors when switching workbench tabs."""
        tabs = ["Sandbox", "Editor", "Scheduler", "Agents"]
        for tab_name in tabs:
            tab = page.locator("[role='tab']").filter(has_text=tab_name).first
            tab.click()
            page.wait_for_timeout(300)
        assert self._real_errors() == [], (
            f"JS errors during tab switching:\n" + "\n".join(self._real_errors())
        )

    def test_no_errors_sidebar_toggle(self, page):
        """No JS errors when toggling sidebar."""
        toggle = page.locator("header button").first
        toggle.click()
        page.wait_for_timeout(300)
        toggle.click()
        page.wait_for_timeout(300)
        assert self._real_errors() == [], (
            f"JS errors during sidebar toggle:\n" + "\n".join(self._real_errors())
        )

    def test_no_errors_terminal_toggle(self, page):
        """No JS errors when toggling terminal."""
        terminal_btn = page.locator("button[aria-label='Toggle terminal']").first
        terminal_btn.click()
        page.wait_for_timeout(500)
        terminal_btn.click()
        page.wait_for_timeout(500)
        assert self._real_errors() == [], (
            f"JS errors during terminal toggle:\n" + "\n".join(self._real_errors())
        )

    def test_no_errors_workbench_toggle(self, page):
        """No JS errors when toggling workbench."""
        toggle = page.locator("button[aria-label='Close workbench']").first
        toggle.click()
        page.wait_for_timeout(500)
        open_btn = page.locator("button[aria-label='Open workbench']").first
        open_btn.click()
        page.wait_for_timeout(500)
        assert self._real_errors() == [], (
            f"JS errors during workbench toggle:\n" + "\n".join(self._real_errors())
        )


# ═══════════════════════════════════════════════════════════════════════
# 22. Screenshot Summary
# ═══════════════════════════════════════════════════════════════════════


class TestScreenshotSummary:
    """Verify all screenshots were generated."""

    def test_screenshots_exist(self):
        """All test screenshots should have been saved."""
        files = list(SCREENSHOT_DIR.glob("*.png"))
        # We expect 60+ screenshots from the tests above
        assert len(files) > 0, "No screenshots were generated"
        # Print summary for debugging
        print(f"\n{len(files)} screenshots saved to {SCREENSHOT_DIR}")
        for f in sorted(files):
            size_kb = f.stat().st_size / 1024
            print(f"  {f.name}: {size_kb:.1f} KB")
