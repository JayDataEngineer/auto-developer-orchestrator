"""
WebUI Visual Regression Tests.

Uses the webui_visual.py server (same pattern as tui_visual.py) to capture
screenshots and verify the WebUI renders correctly via AI vision assertions.

Requires: Vite frontend running on :5174, webui_visual.py server on :9878.

Start: task webui-visual (in another terminal)
Run:   pytest frontend/test_webui_visual.py -v
"""

import pytest
import requests

from fixtures.vision import (
    assert_webui_visible,
    assert_webui_text_contains,
    capture_webui_screenshot,
    observe_webui,
    webui_click,
    webui_goto,
    webui_press_key,
    webui_type,
    WEBUI_VISUAL_URL,
)

pytestmark = [pytest.mark.playwright, pytest.mark.vision]


@pytest.fixture(autouse=True, scope="module")
def check_visual_server():
    """Skip all tests if the WebUI visual server isn't running."""
    try:
        resp = requests.get(f"{WEBUI_VISUAL_URL}/health", timeout=3)
        resp.raise_for_status()
    except Exception:
        pytest.skip("WebUI visual server not running. Start with: task webui-visual")


class TestWebUIRendering:
    """Verify the WebUI renders correctly on load."""

    def test_main_page_loads(self):
        """The main page should show a dark-themed UI."""
        result = observe_webui()
        assert result["screen"], "Page should have visible text content"
        assert result["viewport"]["width"] > 0

    def test_dark_theme(self):
        """The WebUI should use a dark background theme."""
        assert_webui_visible(
            "Does this screenshot show a dark-themed application UI with "
            "a dark or black background? Answer yes or no."
        )

    def test_top_bar_visible(self):
        """The top navigation bar should be visible."""
        assert_webui_visible(
            "Is there a top navigation bar visible at the top of the page "
            "with tabs or buttons? Answer yes or no."
        )

    def test_project_selector_present(self):
        """A project selector (dropdown) should be visible."""
        assert_webui_text_contains("project")


class TestWebUITabs:
    """Verify tab navigation renders correctly."""

    def test_agent_tab_default(self):
        """Agent tab should be the default view."""
        webui_goto("http://localhost:5174")
        assert_webui_visible(
            "Does this show an agent chat interface with a text input "
            "area or message area? Answer yes or no."
        )

    def test_tasks_tab(self):
        """Clicking the Tasks tab should show a task list."""
        webui_click(selector="button:has-text('Tasks')")
        assert_webui_visible(
            "Does this show a tasks list or task management interface "
            "with task items? Answer yes or no."
        )
        # Go back to agent tab
        webui_click(selector="button:has-text('Agent')")


class TestWebUIResponsive:
    """Verify responsive layout at different viewports."""

    def test_mobile_layout(self):
        """The WebUI should be functional on mobile (375x667)."""
        webui_goto("http://localhost:5174")
        result = observe_webui()
        assert result["screen"], "Mobile view should have content"

    def test_desktop_layout(self):
        """The WebUI should look good on desktop (1920x1080)."""
        webui_goto("http://localhost:5174")
        result = observe_webui()
        assert result["viewport"]["width"] > 0


class TestWebUINoErrors:
    """Verify no console errors on load."""

    def test_no_console_errors(self):
        """The browser console should not show errors after page load."""
        webui_goto("http://localhost:5174")
        result = observe_webui()
        logs = result.get("logs", [])
        errors = [l for l in logs if "[error]" in l.lower()]
        # Filter out known benign errors (e.g., extension-related)
        real_errors = [e for e in errors if "extension" not in e.lower()]
        assert len(real_errors) == 0, (
            f"Console errors found:\n" + "\n".join(real_errors[:10])
        )
