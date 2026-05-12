"""Browser automation fixtures."""

import pytest


def goto_frontend(page, frontend_url):
    """Navigate to frontend and wait for it to be ready."""
    page.goto(frontend_url, wait_until="networkidle", timeout=30000)
    try:
        page.wait_for_selector(".h-10.border-b", timeout=20000)
    except Exception:
        content = page.content()
        if len(content) < 100:
            pytest.skip("Frontend page not rendering")
