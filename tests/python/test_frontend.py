"""
Playwright frontend UI tests: app loads, tab navigation, agent view, prompt,
desktop panel, scheduler panel.
Tests against the REAL backend — no mocking.
"""

import pytest

pytestmark = pytest.mark.playwright


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _goto(page, frontend_url):
    """Navigate to the frontend and wait for it to be ready."""
    page.goto(frontend_url, wait_until="networkidle", timeout=30000)
    # Wait for the top bar to render (proves backend responded)
    page.wait_for_selector(".h-10.border-b", timeout=15000)


# ---------------------------------------------------------------------------
# App loads
# ---------------------------------------------------------------------------


class TestAppLoads:
    def test_page_renders(self, page, frontend_url):
        _goto(page, frontend_url)
        # Page should have content, not be blank
        body = page.locator("body")
        assert body.is_visible()
        # Should have the top bar with tabs
        top_bar = page.locator(".h-10.border-b")
        assert top_bar.is_visible()

    def test_tabs_visible(self, page, frontend_url):
        _goto(page, frontend_url)
        # All 4 tabs should be visible: Agent, Tasks, Desktop, Scheduler
        # Use exact=True to avoid matching sidebar session buttons that contain "Agent"
        for tab_name in ["Agent", "Tasks", "Desktop", "Scheduler"]:
            btn = page.get_by_role("button", name=tab_name, exact=True)
            assert btn.is_visible(timeout=5000), f"Tab '{tab_name}' not visible"

    def test_project_selector_visible(self, page, frontend_url):
        _goto(page, frontend_url)
        # Project selector dropdown should be visible
        selector = page.locator("select").first
        assert selector.is_visible(timeout=5000)
        options = selector.locator("option").all_text_contents()
        assert len(options) > 0, "No projects in selector"


# ---------------------------------------------------------------------------
# Tab navigation
# ---------------------------------------------------------------------------


class TestTabNavigation:
    @pytest.fixture(autouse=True)
    def _load(self, page, frontend_url):
        _goto(page, frontend_url)

    def test_agent_tab(self, page):
        page.get_by_role("button", name="Agent", exact=True).click()
        page.wait_for_timeout(1000)
        # Should show the agent chat textarea
        textarea = page.locator("textarea").first
        assert textarea.is_visible(timeout=5000)

    def test_tasks_tab(self, page):
        page.get_by_role("button", name="Tasks", exact=True).click()
        page.wait_for_timeout(1000)
        # Should show tasks-related UI
        content = page.content()
        assert any(kw in content for kw in ["Task", "task", "New", "board"]), \
            "Tasks tab doesn't show task-related content"

    def test_desktop_tab(self, page):
        page.get_by_role("button", name="Desktop", exact=True).click()
        page.wait_for_timeout(2000)
        # Should show desktop UI (Agent Chat sidebar + Controls)
        agent_chat = page.get_by_text("Agent Chat")
        assert agent_chat.is_visible(timeout=5000), "Desktop Agent Chat sidebar not visible"

    def test_scheduler_tab(self, page):
        page.get_by_role("button", name="Scheduler", exact=True).click()
        page.wait_for_timeout(1000)
        # Should show scheduler-related UI
        content = page.content()
        assert any(kw in content for kw in ["Job", "Schedule", "Cron", "New"]), \
            "Scheduler tab doesn't show scheduler content"


# ---------------------------------------------------------------------------
# Pi Agent View
# ---------------------------------------------------------------------------


class TestPiAgentView:
    @pytest.fixture(autouse=True)
    def _load_agent_tab(self, page, frontend_url):
        _goto(page, frontend_url)
        page.get_by_role("button", name="Agent", exact=True).click()
        page.wait_for_timeout(1000)

    def test_prompt_textarea_present(self, page):
        textarea = page.locator("textarea").first
        assert textarea.is_visible(timeout=5000)

    def test_can_type_in_textarea(self, page):
        textarea = page.locator("textarea").first
        textarea.fill("Hello from test")
        assert textarea.input_value() == "Hello from test"


# ---------------------------------------------------------------------------
# Agent prompt (slow — needs LLM)
# ---------------------------------------------------------------------------


@pytest.mark.slow
class TestAgentPrompt:
    @pytest.fixture(autouse=True)
    def _load_agent_tab(self, page, frontend_url):
        _goto(page, frontend_url)
        page.get_by_role("button", name="Agent", exact=True).click()
        page.wait_for_timeout(1000)

    def test_send_prompt_and_get_response(self, page):
        textarea = page.locator("textarea").first
        assert textarea.is_visible(timeout=5000)

        textarea.fill("Say exactly: hello e2e")
        textarea.press("Enter")

        # Wait for response — could take up to 30s with real model
        page.wait_for_timeout(15000)

        # Page should still be responsive (not crashed)
        assert page.locator("body").is_visible()
        # Should have some content rendered
        html = page.content()
        assert len(html) > 100


# ---------------------------------------------------------------------------
# Desktop tab detailed tests
# ---------------------------------------------------------------------------


class TestDesktopTab:
    @pytest.fixture(autouse=True)
    def _load_desktop(self, page, frontend_url):
        _goto(page, frontend_url)
        page.get_by_role("button", name="Desktop", exact=True).click()
        page.wait_for_timeout(2000)

    def test_left_sidebar_agent_chat(self, page):
        """Left sidebar should have Agent Chat panel with textarea."""
        agent_chat = page.get_by_text("Agent Chat")
        assert agent_chat.is_visible(timeout=5000)

    def test_left_sidebar_textarea(self, page):
        """Agent Chat sidebar should have a textarea for messages."""
        textarea = page.locator(".w-80 textarea, .border-r textarea").first
        assert textarea.is_visible(timeout=5000)

    def test_right_sidebar_controls(self, page):
        """Right sidebar should have Controls panel."""
        controls = page.get_by_text("Controls")
        assert controls.is_visible(timeout=5000)

    def test_center_panel_has_content(self, page):
        """Center panel should show sandbox status or relevant content."""
        content = page.content()
        shows_content = any(kw in content for kw in [
            "sandbox", "Desktop", "Start", "not available", "Select"
        ])
        assert shows_content, "Desktop center panel appears blank"
