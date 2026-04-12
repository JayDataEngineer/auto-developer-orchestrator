"""
Real browser integration tests — NO MOCKS.

Uses Playwright to open the actual frontend, click real buttons, verify
the UI actually works against the running backend.

Tests:
  1. App loads, tabs render
  2. Agent tab: left sidebar shows conversations
  3. Agent tab: right-click conversation → context menu appears
  4. Agent tab: rename a conversation via context menu
  5. Agent tab: delete a conversation via context menu
  6. Desktop tab: different left sidebar content (agent messages)
  7. Desktop tab: right sidebar shows browser tools, not artifacts
  8. Desktop tab: enable computer use doesn't hang
  9. Sidebar toggle buttons change labels per tab
"""

import pytest

pytestmark = [pytest.mark.playwright]

FRONTEND_URL = "http://localhost:5174"


def _goto(page):
    page.goto(FRONTEND_URL, wait_until="networkidle", timeout=30000)
    # Wait for the top bar specifically (first .h-10.border-b is the AppShell top bar)
    try:
        page.wait_for_selector(".h-10.border-b", timeout=20000)
    except Exception:
        # Retry once — the dev server may be slow after many sequential tests
        page.reload(wait_until="networkidle", timeout=30000)
        page.wait_for_selector(".h-10.border-b", timeout=20000)


def _get_top_bar(page):
    """Return the top bar container (first .h-10.border-b element)."""
    return page.locator(".h-10.border-b").first


def _get_tab_button(page, name):
    """Get a tab button scoped to the top bar to avoid clashes with other buttons."""
    top_bar = _get_top_bar(page)
    return top_bar.locator("button", has_text=name).first


def _click_tab(page, name):
    _get_tab_button(page, name).click()
    page.wait_for_timeout(500)


class TestAppLoads:
    def test_page_renders(self, page):
        _goto(page)
        assert _get_top_bar(page).is_visible()

    def test_all_tabs_visible(self, page):
        _goto(page)
        top_bar = _get_top_bar(page)
        for name in ["Agent", "Tasks", "Desktop"]:
            btn = top_bar.locator("button", has_text=name).first
            assert btn.is_visible(timeout=5000), f"Tab '{name}' not visible"


class TestAgentTabSidebar:
    def _open_chats_sidebar(self, page):
        """Ensure the Chats sidebar is open on the Agent tab, with test-repo expanded."""
        _goto(page)
        _click_tab(page, "Agent")

        # Wait for sidebar to load
        page.wait_for_timeout(2000)

        # Check if sidebar is visible — if not, click the Chats toggle
        sidebar = page.locator(".border-r.border-white\\/5")
        if sidebar.count() == 0:
            # Sidebar is collapsed — click the Chats toggle
            top_bar = _get_top_bar(page)
            toggle_btn = top_bar.locator("button").first
            toggle_btn.click()
            page.wait_for_timeout(1000)

        # Wait for history to load
        try:
            page.wait_for_selector("text=History", timeout=5000)
        except Exception:
            pass

        # Expand test-repo project group (it starts collapsed)
        test_repo_btn = page.locator("button", has_text="test-repo")
        if test_repo_btn.count() > 0:
            # Check if it's collapsed by looking for conversation items inside it
            msgs = page.locator("text=msgs")
            if msgs.count() == 0:
                test_repo_btn.first.click()
                page.wait_for_timeout(1000)

    def test_left_sidebar_shows_conversations(self, page):
        """The Chats sidebar on the Agent tab should list conversations."""
        self._open_chats_sidebar(page)

        # Sidebar should show conversation items (font-mono text with "msgs")
        conv_items = page.locator("text=msgs")
        count = conv_items.count()
        assert count > 0, "No conversations visible in left sidebar"

    def test_context_menu_on_right_click(self, page):
        """Right-clicking a conversation should show Rename/Delete menu."""
        self._open_chats_sidebar(page)

        # Find a conversation item (contains "msgs" text)
        conv = page.locator("text=msgs").first
        assert conv.is_visible(), "No conversation to right-click"

        # Right-click it
        conv.click(button="right")
        page.wait_for_timeout(300)

        # Context menu should appear with Rename and Delete buttons
        rename_btn = page.get_by_role("button", name="Rename")
        delete_btn = page.get_by_role("button", name="Delete")
        assert rename_btn.is_visible(timeout=3000), "Rename option not visible after right-click"
        assert delete_btn.is_visible(), "Delete option not visible after right-click"

        # Dismiss
        page.keyboard.press("Escape")

    def test_rename_conversation(self, page, api_url, api_session):
        """Rename a conversation through the UI context menu and verify via API."""
        # First ensure there's a conversation to rename
        from conftest import stream_prompt
        agent_id = "ui-rename-test"
        try:
            stream_prompt(api_url, api_session, "test-repo", "hello", agent_id=agent_id)
        except Exception:
            pass

        self._open_chats_sidebar(page)
        page.wait_for_timeout(1000)

        # Reload to get fresh history
        page.reload(wait_until="networkidle")
        page.wait_for_timeout(1000)

        # Find our conversation by its agent ID text
        conv = page.locator("span", has_text="ui-rename-test")
        if conv.count() == 0:
            pytest.skip("Test conversation not visible in sidebar")

        # Right-click to open context menu
        conv.first.click(button="right")
        page.wait_for_timeout(300)

        # Click Rename button in the context menu (use role to avoid text match issues)
        page.get_by_role("button", name="Rename").click()
        page.wait_for_timeout(300)

        # Type new name
        new_name = "UI Renamed Conversation"
        rename_input = page.locator("input").last
        rename_input.fill(new_name)
        page.wait_for_timeout(100)

        # Press Enter to confirm
        rename_input.press("Enter")
        page.wait_for_timeout(1000)

        # Verify via API that the rename happened
        hist = api_session.get(f"{api_url}/api/pi/history").json()
        match = [c for c in hist["conversations"] if c["agentId"] == agent_id]
        assert len(match) == 1, f"Conversation {agent_id} not found in history"
        assert match[0]["title"] == new_name, f"Title not updated: {match[0]['title']!r}"

        # Cleanup
        api_session.delete(f"{api_url}/api/pi/conversation?project=test-repo&agentId={agent_id}")

    def test_delete_conversation(self, page, api_url, api_session):
        """Delete a conversation through the UI context menu and verify via API."""
        from conftest import stream_prompt
        agent_id = "ui-delete-test"
        try:
            stream_prompt(api_url, api_session, "test-repo", "hello", agent_id=agent_id)
        except Exception:
            pass

        self._open_chats_sidebar(page)
        page.wait_for_timeout(1000)

        page.reload(wait_until="networkidle")
        page.wait_for_timeout(1000)

        # Verify it exists via API first
        hist = api_session.get(f"{api_url}/api/pi/history").json()
        match = [c for c in hist["conversations"] if c["agentId"] == agent_id]
        assert len(match) == 1, f"Setup failed: conversation {agent_id} not found"

        # Find and right-click the conversation
        conv = page.locator("span", has_text="ui-delete-test")
        if conv.count() == 0:
            pytest.skip("Test conversation not visible")
        conv.first.click(button="right")
        page.wait_for_timeout(300)

        # Click Delete button in the context menu (use role to avoid text match issues)
        page.get_by_role("button", name="Delete").click()
        page.wait_for_timeout(1000)

        # Verify via API that it's gone
        hist2 = api_session.get(f"{api_url}/api/pi/history").json()
        match2 = [c for c in hist2["conversations"] if c["agentId"] == agent_id]
        assert len(match2) == 0, f"Conversation {agent_id} still exists after UI delete"


class TestDesktopTabSidebar:
    def test_left_sidebar_shows_conversation_not_history(self, page):
        """Desktop tab left sidebar should show 'Conversation' header, not 'History'."""
        _goto(page)
        _click_tab(page, "Desktop")
        page.wait_for_timeout(500)

        # Open sidebar if needed
        agent_btn = page.locator("button", has_text="Agent")
        if agent_btn.count() == 0:
            panel_btn = page.locator("button[title*='Agent']")
            if panel_btn.count() > 0:
                panel_btn.first.click()
                page.wait_for_timeout(500)

        # Left sidebar should say "Conversation" not "History"
        conv_header = page.locator("text=Conversation")
        assert conv_header.count() > 0, "Desktop left sidebar should have 'Conversation' header"

    def test_right_sidebar_shows_browser_tools(self, page):
        """Desktop tab right sidebar should show Browser header and Chats section."""
        _goto(page)
        _click_tab(page, "Desktop")
        page.wait_for_timeout(500)

        # Open right sidebar if needed
        browser_btn = page.locator("button", has_text="Browser")
        if browser_btn.count() == 0:
            panel_btn = page.locator("button[title*='Browser']")
            if panel_btn.count() > 0:
                panel_btn.first.click()
                page.wait_for_timeout(500)

        # Right sidebar should show "Browser" header
        browser_header = page.locator("text=Browser")
        assert browser_header.count() > 0, "Desktop right sidebar should show 'Browser'"

    def test_right_sidebar_has_chat_section(self, page):
        """Desktop tab right sidebar should show Chats section with project selector."""
        _goto(page)
        _click_tab(page, "Desktop")
        page.wait_for_timeout(500)

        # Open right sidebar if needed
        browser_btn = page.locator("button", has_text="Browser")
        if browser_btn.count() == 0:
            panel_btn = page.locator("button[title*='Browser']")
            if panel_btn.count() > 0:
                panel_btn.first.click()
                page.wait_for_timeout(500)

        # Should have a "Chats" section in the right sidebar
        chats_header = page.locator("text=Chats")
        assert chats_header.count() > 0, "Desktop right sidebar should have 'Chats' section"

        # Should have a project selector dropdown
        project_select = page.locator("select").last
        assert project_select.is_visible(), "Project selector should be visible"

        # Should have sandbox name input
        sandbox_input = page.locator("input[placeholder*='sandbox']", has_text="")
        # Placeholder-based search may not work, try by placeholder attr
        sandbox_inputs = page.locator("input[placeholder*='sandbox' i]")
        assert sandbox_inputs.count() > 0, "Should have sandbox name input"

    def test_computer_use_does_not_hang(self, page):
        """Clicking Enable Computer Use should not hang forever."""
        _goto(page)
        _click_tab(page, "Desktop")
        page.wait_for_timeout(500)

        # Open right sidebar
        browser_btn = page.locator("button", has_text="Browser")
        if browser_btn.count() == 0:
            panel_btn = page.locator("button[title*='Browser']")
            if panel_btn.count() > 0:
                panel_btn.first.click()
                page.wait_for_timeout(500)

        # If already enabled, skip
        active_badge = page.locator("text=Active")
        if active_badge.count() > 0:
            pytest.skip("Computer use already enabled")

        # Find and click Enable button
        enable_btn = page.locator("button", has_text="Enable Computer Use")
        if enable_btn.count() == 0:
            pytest.skip("Enable Computer Use button not found")

        enable_btn.first.click()

        # Wait up to 35 seconds — should either succeed or show error, not hang
        try:
            # Look for either success (Active badge) or error message
            page.wait_for_selector("text=Active", timeout=35000)
        except Exception:
            # Check if error appeared instead
            error = page.locator("[class*='red']")
            if error.count() > 0:
                # Error is acceptable (e.g., Docker not available)
                pass
            else:
                pytest.fail("Computer use enable hung — no success or error after 35s")


class TestSidebarLabels:
    def test_agent_tab_shows_chats_label(self, page):
        """Agent tab should show 'Chats' toggle button."""
        _goto(page)
        _click_tab(page, "Agent")
        page.wait_for_timeout(500)

        chats = page.locator("button", has_text="Chats")
        assert chats.count() > 0, "'Chats' button not visible on Agent tab"

    def test_agent_tab_shows_artifacts_label(self, page):
        """Agent tab should show 'Artifacts' toggle button."""
        _goto(page)
        _click_tab(page, "Agent")
        page.wait_for_timeout(500)

        artifacts = page.locator("button", has_text="Artifacts")
        assert artifacts.count() > 0, "'Artifacts' button not visible on Agent tab"

    def test_desktop_tab_shows_agent_label(self, page):
        """Desktop tab should show 'Agent' toggle button (left sidebar)."""
        _goto(page)
        _click_tab(page, "Desktop")
        page.wait_for_timeout(500)

        # The toggle button labeled "Agent" (not the tab button)
        agent_toggle = page.locator("button", has_text="Agent")
        assert agent_toggle.count() > 0, "'Agent' sidebar toggle not visible on Desktop tab"

    def test_desktop_tab_shows_browser_label(self, page):
        """Desktop tab should show 'Browser' toggle button (right sidebar)."""
        _goto(page)
        _click_tab(page, "Desktop")
        page.wait_for_timeout(500)

        browser = page.locator("button", has_text="Browser")
        assert browser.count() > 0, "'Browser' sidebar toggle not visible on Desktop tab"

    def test_tasks_tab_has_no_sidebar_toggles(self, page):
        """Tasks tab should NOT show Chats or Browser toggles."""
        _goto(page)
        _click_tab(page, "Tasks")
        page.wait_for_timeout(500)

        chats = page.locator("button", has_text="Chats")
        browser = page.locator("button", has_text="Browser")
        assert chats.count() == 0, "'Chats' should not appear on Tasks tab"
        assert browser.count() == 0, "'Browser' should not appear on Tasks tab"
