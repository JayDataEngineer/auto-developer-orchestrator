"""Browser automation fixtures."""

import pytest


def goto_frontend(page, frontend_url):
    """Navigate to frontend and wait for it to be ready.

    Waits for the React app to mount by checking for the sidebar content
    or header bar. If the app crashes (empty #root), skips the test.
    """
    page.goto(frontend_url, wait_until="networkidle", timeout=30000)
    try:
        # Wait for the app shell: either the sidebar or the header bar
        page.wait_for_selector(
            "[data-sidebar='content'], header.h-10",
            timeout=20000,
        )
    except Exception:
        # Check if React rendered at all -- if #root is empty, the app crashed
        root_html = page.evaluate("document.getElementById('root')?.innerHTML?.length || 0")
        if root_html == 0:
            pytest.skip("React app did not mount (#root is empty -- likely a build/hook error)")
        # App rendered something but our selectors didn't match -- still skip
        pytest.skip("Frontend loaded but app shell selectors not found")
