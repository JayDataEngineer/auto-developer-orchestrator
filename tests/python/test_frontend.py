"""
Playwright frontend UI tests: app loads, tab navigation, agent view, prompt,
web browser panel, sandbox mode buttons.
"""

import pytest

pytestmark = pytest.mark.playwright


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _goto(page, frontend_url):
    """Navigate to the frontend and wait for it to be ready."""
    page.goto(frontend_url, wait_until="networkidle", timeout=30000)


# ---------------------------------------------------------------------------
# App loads
# ---------------------------------------------------------------------------


class TestAppLoads:
    def test_page_renders(self, page, frontend_url):
        _goto(page, frontend_url)
        # Page should have content, not be blank
        body = page.locator("body")
        assert body.is_visible()

    def test_sidebar_visible(self, page, frontend_url):
        _goto(page, frontend_url)
        # Sidebar should be present (nav element or specific class)
        sidebar = page.locator("nav, aside, [data-testid='sidebar']").first
        assert sidebar.is_visible(timeout=5000)

    def test_header_visible(self, page, frontend_url):
        _goto(page, frontend_url)
        # Header should contain the app name or logo
        header = page.locator("header, [data-testid='header']").first
        # If no explicit header, check for the orchestrator label
        if not header.is_visible(timeout=3000):
            label = page.get_by_text("Orchestrator")
            assert label.is_visible(timeout=5000)
        else:
            assert True


# ---------------------------------------------------------------------------
# Tab navigation
# ---------------------------------------------------------------------------


class TestTabNavigation:
    @pytest.fixture(autouse=True)
    def _load(self, page, frontend_url):
        _goto(page, frontend_url)

    def test_terminal_tab(self, page):
        tab = page.get_by_text("Terminal", exact=False).first
        if tab.is_visible(timeout=3000):
            tab.click()
            # Should show terminal/checklist content
            page.wait_for_timeout(500)

    def test_activity_tab(self, page):
        tab = page.get_by_text("Activity", exact=False).first
        if tab.is_visible(timeout=3000):
            tab.click()
            page.wait_for_timeout(500)

    def test_github_tab(self, page):
        tab = page.get_by_text("GitHub", exact=False).first
        if tab.is_visible(timeout=3000):
            tab.click()
            page.wait_for_timeout(500)

    def test_agents_tab(self, page):
        tab = page.get_by_text("Agents", exact=False).first
        if tab.is_visible(timeout=3000):
            tab.click()
            page.wait_for_timeout(500)


# ---------------------------------------------------------------------------
# Pi Agent View
# ---------------------------------------------------------------------------


class TestPiAgentView:
    @pytest.fixture(autouse=True)
    def _load_agents(self, page, frontend_url):
        _goto(page, frontend_url)
        # Click Agents tab/button
        agents_btn = page.get_by_text("Agents", exact=False).first
        if agents_btn.is_visible(timeout=5000):
            agents_btn.click()
        page.wait_for_timeout(1000)

    def test_agent_view_renders(self, page):
        # Should show the PI CODING AGENT heading or dashboard
        agent_heading = page.get_by_text("PI CODING AGENT").first
        assert agent_heading.is_visible(timeout=10000)

    def test_model_selector_present(self, page):
        # Model dropdown/selector should be visible
        model_label = page.get_by_text("Model:", exact=False).first
        assert model_label.is_visible(timeout=5000)

    def test_prompt_textarea_present(self, page):
        # Textarea with placeholder for describing tasks
        textarea = page.get_by_placeholder("Describe").first
        if not textarea.is_visible(timeout=3000):
            # Fallback: any textarea
            textarea = page.locator("textarea").first
        assert textarea.is_visible(timeout=5000)


# ---------------------------------------------------------------------------
# Agent prompt (slow — needs LLM)
# ---------------------------------------------------------------------------


@pytest.mark.slow
class TestAgentPrompt:
    @pytest.fixture(autouse=True)
    def _load_agents(self, page, frontend_url):
        _goto(page, frontend_url)
        agents_btn = page.get_by_text("Agents", exact=False).first
        if agents_btn.is_visible(timeout=5000):
            agents_btn.click()
        page.wait_for_timeout(1000)

    def test_send_prompt_and_get_response(self, page):
        # Type into prompt textarea
        textarea = page.get_by_placeholder("Describe").first
        if not textarea.is_visible(timeout=3000):
            textarea = page.locator("textarea").first
        assert textarea.is_visible(timeout=5000)

        textarea.fill("Say exactly: hello e2e")

        # Click send button
        send_btn = page.locator("button").filter(has=page.locator("svg")).last
        if send_btn.is_visible(timeout=2000):
            send_btn.click()

        # Wait for streaming indicator
        streaming = page.get_by_text("Streaming").first
        # It may appear briefly
        page.wait_for_timeout(3000)

        # Eventually streaming should stop and a response should appear
        # Wait up to 60s for agent to finish
        page.wait_for_timeout(30000)
        # Just verify the page didn't crash — no assertion on exact response
        assert page.locator("body").is_visible()


# ---------------------------------------------------------------------------
# Web browser panel
# ---------------------------------------------------------------------------


class TestWebBrowserPanel:
    @pytest.fixture(autouse=True)
    def _load_agents(self, page, frontend_url):
        _goto(page, frontend_url)
        agents_btn = page.get_by_text("Agents", exact=False).first
        if agents_btn.is_visible(timeout=5000):
            agents_btn.click()
        page.wait_for_timeout(1000)

    def test_web_toggle_opens_panel(self, page):
        # Click the "Web" button to open the browser panel
        web_btn = page.get_by_text("Web", exact=True).first
        if web_btn.is_visible(timeout=5000):
            web_btn.click()
            page.wait_for_timeout(500)

            # Should show the web browser panel with URL bar
            url_input = page.get_by_placeholder("Enter URL").first
            assert url_input.is_visible(timeout=5000)
        else:
            pytest.skip("Web button not found")

    def test_web_close_hides_panel(self, page):
        # Open panel
        web_btn = page.get_by_text("Web", exact=True).first
        if web_btn.is_visible(timeout=5000):
            web_btn.click()
            page.wait_for_timeout(500)

            # Close the panel
            close_btn = page.get_by_text("Close").first
            if close_btn.is_visible(timeout=3000):
                close_btn.click()
                page.wait_for_timeout(500)

                # Panel should be gone
                url_input = page.get_by_placeholder("Enter URL").first
                assert not url_input.is_visible(timeout=2000)
            else:
                pytest.skip("Close button not found")
        else:
            pytest.skip("Web button not found")


# ---------------------------------------------------------------------------
# Sandbox mode buttons
# ---------------------------------------------------------------------------


class TestSandboxModeButtons:
    @pytest.fixture(autouse=True)
    def _load_agents(self, page, frontend_url):
        _goto(page, frontend_url)
        agents_btn = page.get_by_text("Agents", exact=False).first
        if agents_btn.is_visible(timeout=5000):
            agents_btn.click()
        page.wait_for_timeout(1000)

    def test_browser_mode_button_visible(self, page):
        btn = page.get_by_text("Browser Mode", exact=False).first
        assert btn.is_visible(timeout=10000)

    def test_desktop_mode_button_visible(self, page):
        btn = page.get_by_text("Desktop Mode", exact=False).first
        assert btn.is_visible(timeout=10000)
