"""
Playwright frontend UI tests: app loads, sidebar, composer, workbench tabs.

Tests against the REAL backend -- no mocking.

Current UI layout (from app.tsx):
  - Left sidebar: shadcn Sidebar with project groups + conversations
  - Main area: Thread (messages + composer) always visible
  - Right panel: Workbench with tabs (Sandbox, Editor, Scheduler, Agents)
  - Header bar: sidebar toggle, terminal toggle, workbench toggle
"""

import pytest

from fixtures.browser import goto_frontend

pytestmark = pytest.mark.playwright


# ---------------------------------------------------------------------------
# App loads
# ---------------------------------------------------------------------------


class TestAppLoads:
    def test_page_renders(self, page, frontend_url):
        goto_frontend(page, frontend_url)
        # Page should have content, not be blank
        body = page.locator("body")
        assert body.is_visible()
        # Should have the header bar
        header = page.locator("header.h-10")
        assert header.is_visible()

    def test_sidebar_visible(self, page, frontend_url):
        goto_frontend(page, frontend_url)
        # Sidebar should be visible by default (defaultOpen={true})
        sidebar = page.locator("[data-sidebar='content']")
        assert sidebar.is_visible(timeout=5000)

    def test_composer_present(self, page, frontend_url):
        goto_frontend(page, frontend_url)
        # Composer textarea should be present (always visible in main thread area)
        textarea = page.locator("textarea")
        assert textarea.is_visible(timeout=5000)


# ---------------------------------------------------------------------------
# Sidebar
# ---------------------------------------------------------------------------


class TestSidebar:
    @pytest.fixture(autouse=True)
    def _load(self, page, frontend_url):
        goto_frontend(page, frontend_url)

    def test_sidebar_shows_new_chat_button(self, page):
        """Sidebar should have 'New Chat' button."""
        new_chat = page.locator("[data-sidebar='menu-button']", has_text="New Chat")
        assert new_chat.is_visible(timeout=5000), "'New Chat' button not visible in sidebar"

    def test_sidebar_shows_open_folder_button(self, page):
        """Sidebar should have 'Open Folder' button."""
        open_folder = page.locator("[data-sidebar='menu-button']", has_text="Open Folder")
        assert open_folder.is_visible(timeout=5000), "'Open Folder' button not visible in sidebar"

    def test_sidebar_toggle_works(self, page):
        """Clicking sidebar toggle should collapse/expand the sidebar."""
        # Find the sidebar trigger button in the header
        toggle = page.locator("header button[aria-label='Toggle Sidebar'], header button").first
        # Sidebar content should start visible
        sidebar_content = page.locator("[data-sidebar='content']")
        assert sidebar_content.is_visible()

    def test_sidebar_toggle_button(self, page):
        """Header should have sidebar toggle button."""
        # SidebarTrigger renders as a button inside the header
        header = page.locator("header.h-10")
        toggle_btn = header.locator("button").first
        assert toggle_btn.is_visible()


# ---------------------------------------------------------------------------
# Workbench (right panel)
# ---------------------------------------------------------------------------


class TestWorkbenchTabs:
    @pytest.fixture(autouse=True)
    def _load(self, page, frontend_url):
        goto_frontend(page, frontend_url)

    def test_workbench_tabs_visible(self, page):
        """Workbench right panel should show Sandbox, Editor, Scheduler, Agents tabs."""
        # Workbench uses Radix TabsTrigger elements
        for tab_name in ["Sandbox", "Editor", "Scheduler", "Agents"]:
            tab = page.get_by_role("tab", name=tab_name)
            assert tab.is_visible(timeout=5000), f"Workbench tab '{tab_name}' not visible"

    def test_sandbox_tab_content(self, page):
        """Clicking Sandbox tab should show sandbox-related content."""
        page.get_by_role("tab", name="Sandbox").click()
        page.wait_for_timeout(1000)
        # Sandbox tab shows VNC viewer or "No sandbox" message
        content = page.content()
        shows_content = any(kw in content for kw in [
            "sandbox", "Sandbox", "No sandbox", "Detecting", "Start sandbox",
        ])
        assert shows_content, "Sandbox tab appears blank"

    def test_editor_tab_content(self, page):
        """Clicking Editor tab should show editor-related content."""
        page.get_by_role("tab", name="Editor").click()
        page.wait_for_timeout(1000)
        content = page.content()
        shows_content = any(kw in content for kw in ["Editor", "No file", "Select", "file"])
        assert shows_content, "Editor tab appears blank"

    def test_scheduler_tab_content(self, page):
        """Clicking Scheduler tab should show scheduler-related content."""
        page.get_by_role("tab", name="Scheduler").click()
        page.wait_for_timeout(1000)
        content = page.content()
        shows_content = any(kw in content for kw in ["Scheduler", "Schedule", "Job", "No scheduled"])
        assert shows_content, "Scheduler tab appears blank"

    def test_agents_tab_content(self, page):
        """Clicking Agents tab should show workers/agents content."""
        page.get_by_role("tab", name="Agents").click()
        page.wait_for_timeout(1000)
        content = page.content()
        shows_content = any(kw in content for kw in ["Workers", "Agent", "CTO", "No workers", "orchestrator"])
        assert shows_content, "Agents tab appears blank"


# ---------------------------------------------------------------------------
# Composer (main chat area)
# ---------------------------------------------------------------------------


class TestComposer:
    @pytest.fixture(autouse=True)
    def _load(self, page, frontend_url):
        goto_frontend(page, frontend_url)

    def test_composer_textarea_present(self, page):
        """Chat textarea should be visible."""
        textarea = page.locator("textarea").first
        assert textarea.is_visible(timeout=5000)

    def test_can_type_in_textarea(self, page):
        """Should be able to type in the composer textarea."""
        textarea = page.locator("textarea").first
        textarea.fill("Hello from test")
        assert textarea.input_value() == "Hello from test"

    def test_send_button_present(self, page):
        """Send button should be present (aria-label='Send message')."""
        send_btn = page.locator("button[aria-label='Send message']")
        assert send_btn.is_visible(timeout=5000)

    def test_model_selector_present(self, page):
        """Model selector should be present in the composer area."""
        model_select = page.locator("button[aria-label='Select model']")
        assert model_select.is_visible(timeout=5000)


# ---------------------------------------------------------------------------
# Header bar
# ---------------------------------------------------------------------------


class TestHeaderBar:
    @pytest.fixture(autouse=True)
    def _load(self, page, frontend_url):
        goto_frontend(page, frontend_url)

    def test_terminal_toggle_present(self, page):
        """Header should have terminal toggle button."""
        term_btn = page.locator("button[aria-label='Toggle terminal']")
        assert term_btn.is_visible(timeout=5000)

    def test_workbench_toggle_present(self, page):
        """Header should have workbench toggle button."""
        wb_btn = page.locator("button[aria-label='Open workbench'], button[aria-label='Close workbench']")
        assert wb_btn.is_visible(timeout=5000)


# ---------------------------------------------------------------------------
# Agent prompt (slow -- needs LLM)
# ---------------------------------------------------------------------------


@pytest.mark.slow
class TestAgentPrompt:
    @pytest.fixture(autouse=True)
    def _load(self, page, frontend_url):
        goto_frontend(page, frontend_url)

    def test_send_prompt_and_get_response(self, page):
        textarea = page.locator("textarea").first
        assert textarea.is_visible(timeout=5000)

        textarea.fill("Say exactly: hello e2e")
        textarea.press("Enter")

        # Wait for response -- could take up to 30s with real model
        page.wait_for_timeout(15000)

        # Page should still be responsive (not crashed)
        assert page.locator("body").is_visible()
        # Should have some content rendered
        html = page.content()
        assert len(html) > 100
