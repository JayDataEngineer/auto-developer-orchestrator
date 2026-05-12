"""
Web browser sub-agent tests: session, navigate, click, type, screenshot, state.
"""

import pytest

pytestmark = pytest.mark.browser

# Track session IDs for cleanup
_session_ids: list[str] = []


@pytest.fixture(autouse=True, scope="module")
def cleanup_sessions(api_url, api_session):
    """Close any test sessions after the module finishes."""
    yield
    for sid in _session_ids:
        try:
            api_session.delete(
                f"{api_url}/api/pi/web/session",
                json={"sessionId": sid},
            )
        except Exception:
            pass
    _session_ids.clear()


class TestWebSession:
    def test_create_session(self, api_url, api_session):
        resp = api_session.post(
            f"{api_url}/api/pi/web/session",
            json={"sessionId": "e2e-test-session"},
        )
        if resp.status_code in (500, 502, 503):
            pytest.skip("Browserless service unavailable")
        assert resp.status_code == 200
        data = resp.json()
        assert "sessionId" in data
        _session_ids.append(data["sessionId"])

    def test_close_session(self, api_url, api_session):
        # Create then close
        create_resp = api_session.post(
            f"{api_url}/api/pi/web/session",
            json={},
        )
        if create_resp.status_code in (500, 502, 503):
            pytest.skip("Browserless service unavailable")
        assert create_resp.status_code == 200
        sid = create_resp.json()["sessionId"]

        close_resp = api_session.delete(
            f"{api_url}/api/pi/web/session",
            json={"sessionId": sid},
        )
        assert close_resp.status_code == 200


class TestWebNavigate:
    @pytest.fixture(autouse=True)
    def _session(self, api_url, api_session):
        """Create a session for navigation tests."""
        resp = api_session.post(f"{api_url}/api/pi/web/session", json={})
        if resp.status_code in (500, 502, 503):
            pytest.skip("Browserless service unavailable")
        assert resp.status_code == 200
        self.session_id = resp.json()["sessionId"]
        _session_ids.append(self.session_id)
        yield
        try:
            api_session.delete(
                f"{api_url}/api/pi/web/session",
                json={"sessionId": self.session_id},
            )
        except Exception:
            pass

    def test_navigate_example_com(self, api_url, api_session):
        resp = api_session.post(
            f"{api_url}/api/pi/web/navigate",
            json={"url": "https://example.com", "sessionId": self.session_id},
        )
        if resp.status_code == 500:
            pytest.skip("Browserless service error")
        assert resp.status_code == 200
        data = resp.json()
        # Should have page info
        assert "url" in data or "title" in data or "elements" in data or "screenshot" in data

    def test_navigate_invalid_url(self, api_url, api_session):
        resp = api_session.post(
            f"{api_url}/api/pi/web/navigate",
            json={"url": "not-a-valid-url", "sessionId": self.session_id},
        )
        # Should handle gracefully
        if resp.status_code == 500:
            pytest.skip("Browserless service error")
        assert resp.status_code in (200, 400)


class TestWebInteraction:
    @pytest.fixture(autouse=True)
    def _session(self, api_url, api_session):
        # Create session and navigate first
        resp = api_session.post(f"{api_url}/api/pi/web/session", json={})
        if resp.status_code in (500, 502, 503):
            pytest.skip("Browserless service unavailable")
        assert resp.status_code == 200
        self.session_id = resp.json()["sessionId"]
        _session_ids.append(self.session_id)

        # Navigate to a page with interactive elements
        api_session.post(
            f"{api_url}/api/pi/web/navigate",
            json={"url": "https://example.com", "sessionId": self.session_id},
        )
        yield
        try:
            api_session.delete(
                f"{api_url}/api/pi/web/session",
                json={"sessionId": self.session_id},
            )
        except Exception:
            pass

    def test_click_element(self, api_url, api_session):
        resp = api_session.post(
            f"{api_url}/api/pi/web/click",
            json={"elementId": 0, "sessionId": self.session_id},
        )
        if resp.status_code == 500:
            pytest.skip("Browserless service error")
        # Click may succeed or fail depending on element; just verify no server error
        assert resp.status_code in (200, 400, 404)

    def test_type_text(self, api_url, api_session):
        resp = api_session.post(
            f"{api_url}/api/pi/web/type",
            json={
                "elementId": 0,
                "text": "e2e test input",
                "submit": False,
                "sessionId": self.session_id,
            },
        )
        if resp.status_code == 500:
            pytest.skip("Browserless service error")
        assert resp.status_code in (200, 400, 404)


class TestWebScreenshot:
    @pytest.fixture(autouse=True)
    def _session(self, api_url, api_session):
        resp = api_session.post(f"{api_url}/api/pi/web/session", json={})
        if resp.status_code in (500, 502, 503):
            pytest.skip("Browserless service unavailable")
        assert resp.status_code == 200
        self.session_id = resp.json()["sessionId"]
        _session_ids.append(self.session_id)

        api_session.post(
            f"{api_url}/api/pi/web/navigate",
            json={"url": "https://example.com", "sessionId": self.session_id},
        )
        yield
        try:
            api_session.delete(
                f"{api_url}/api/pi/web/session",
                json={"sessionId": self.session_id},
            )
        except Exception:
            pass

    def test_screenshot_returns_image(self, api_url, api_session):
        resp = api_session.get(
            f"{api_url}/api/pi/web/screenshot",
            params={"sessionId": self.session_id},
        )
        if resp.status_code == 404:
            pytest.skip("Browserless screenshot endpoint unavailable")
        assert resp.status_code == 200
        assert "image" in resp.headers.get("Content-Type", "") or len(resp.content) > 0

    def test_screenshot_missing_session_400(self, api_url, api_session):
        resp = api_session.get(
            f"{api_url}/api/pi/web/screenshot",
            params={"sessionId": "nonexistent-session"},
        )
        assert resp.status_code in (400, 404)


class TestWebState:
    @pytest.fixture(autouse=True)
    def _session(self, api_url, api_session):
        resp = api_session.post(f"{api_url}/api/pi/web/session", json={})
        if resp.status_code in (500, 502, 503):
            pytest.skip("Browserless service unavailable")
        assert resp.status_code == 200
        self.session_id = resp.json()["sessionId"]
        _session_ids.append(self.session_id)

        api_session.post(
            f"{api_url}/api/pi/web/navigate",
            json={"url": "https://example.com", "sessionId": self.session_id},
        )
        yield
        try:
            api_session.delete(
                f"{api_url}/api/pi/web/session",
                json={"sessionId": self.session_id},
            )
        except Exception:
            pass

    def test_get_state_returns_page_info(self, api_url, api_session):
        resp = api_session.get(
            f"{api_url}/api/pi/web/state",
            params={"sessionId": self.session_id},
        )
        assert resp.status_code == 200
        data = resp.json()
        # Should have some page info
        assert data is not None and len(data) > 0

    def test_describe_page(self, api_url, api_session):
        resp = api_session.post(
            f"{api_url}/api/pi/web/describe",
            json={"sessionId": self.session_id},
        )
        if resp.status_code == 404:
            pytest.skip("Describe endpoint unavailable")
        assert resp.status_code == 200
        data = resp.json()
        # Should return a description string
        assert "description" in data or "text" in data or isinstance(data, str) or len(data) > 0
