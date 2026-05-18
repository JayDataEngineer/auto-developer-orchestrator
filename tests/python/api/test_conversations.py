"""
Conversation management tests: history, rename, delete.

These tests verify the full round-trip:
  1. Backend DB operations (conversation_titles table, conversation_messages)
  2. HTTP API endpoints (GET /history, DELETE /conversation, PUT /conversation/rename)
  3. Title persistence across requests

Requires: running Go backend with database initialized.
"""

import pytest

pytestmark = [pytest.mark.api]


class TestConversationHistory:
    """GET /api/pux/history — list conversation messages; GET /api/pux/conversations — summaries."""

    def test_history_returns_list(self, api_url, api_session):
        resp = api_session.get(f"{api_url}/api/pux/history", params={"project": "default"})
        assert resp.status_code == 200
        data = resp.json()
        # GetHistory returns a raw list of messages
        assert isinstance(data, list)

    def test_conversations_returns_list(self, api_url, api_session):
        resp = api_session.get(f"{api_url}/api/pux/conversations")
        assert resp.status_code == 200
        data = resp.json()
        # GetConversations returns a raw list of ConversationSummary objects
        assert isinstance(data, list)

    def test_conversation_summary_has_required_fields(self, api_url, api_session, test_project):
        """Every conversation summary must have project, agentId, lastMessage, lastAt, messageCount, title."""
        # First, ensure there's at least one conversation by sending a prompt
        from utils.sse import stream_prompt
        agent_id = f"test-conv-{__name__}"
        try:
            events = stream_prompt(api_url, api_session, test_project, "say hello", agent_id=agent_id)
            # We don't care about the response, just that messages were saved
        except Exception:
            pass  # Agent might fail but messages should still be saved

        resp = api_session.get(f"{api_url}/api/pux/conversations")
        assert resp.status_code == 200
        conversations = resp.json()

        if not conversations:
            pytest.skip("No conversations exist yet — send a prompt first")

        # Check structure of first conversation
        conv = conversations[0]
        assert "project" in conv, "Missing 'project' field"
        assert "agentId" in conv, "Missing 'agentId' field"
        assert "lastMessage" in conv, "Missing 'lastMessage' field"
        assert "lastAt" in conv, "Missing 'lastAt' field"
        assert "messageCount" in conv, "Missing 'messageCount' field"
        assert "title" in conv, "Missing 'title' field"
        assert isinstance(conv["messageCount"], int)
        assert conv["messageCount"] >= 0

        # Cleanup
        api_session.delete(
            f"{api_url}/api/pux/conversation?project={test_project}&agentId={agent_id}"
        )


class TestConversationRename:
    """PUT /api/pux/conversation/rename — set custom title."""

    def test_rename_sets_title(self, api_url, api_session, test_project):
        """Rename a conversation and verify the title appears in conversations."""
        # Create a conversation by sending a message
        from utils.sse import stream_prompt
        agent_id = f"test-rename-{__name__}"
        try:
            stream_prompt(api_url, api_session, test_project, "say hello", agent_id=agent_id)
        except Exception:
            pass

        # Rename it — project, agentId, and title go in the JSON body
        new_title = "My Custom Title 123"
        resp = api_session.put(
            f"{api_url}/api/pux/conversation/rename",
            json={"project": test_project, "agentId": agent_id, "title": new_title},
        )
        assert resp.status_code == 200, f"Rename failed: {resp.text}"
        data = resp.json()
        assert data.get("success") is True

        # Verify title appears in conversations list
        conv_resp = api_session.get(f"{api_url}/api/pux/conversations")
        assert conv_resp.status_code == 200
        conversations = conv_resp.json()
        match = [c for c in conversations if c["project"] == test_project and c["agentId"] == agent_id]
        assert len(match) == 1, f"Expected 1 conversation, found {len(match)}"
        assert match[0]["title"] == new_title, f"Title mismatch: expected {new_title!r}, got {match[0]['title']!r}"

        # Cleanup
        api_session.delete(
            f"{api_url}/api/pux/conversation?project={test_project}&agentId={agent_id}"
        )

    def test_rename_overwrites_previous_title(self, api_url, api_session, test_project):
        """Second rename replaces the first title."""
        from utils.sse import stream_prompt
        agent_id = f"test-rename-overwrite-{__name__}"
        try:
            stream_prompt(api_url, api_session, test_project, "say hi", agent_id=agent_id)
        except Exception:
            pass

        # First rename
        api_session.put(
            f"{api_url}/api/pux/conversation/rename",
            json={"project": test_project, "agentId": agent_id, "title": "First Title"},
        )

        # Second rename
        resp = api_session.put(
            f"{api_url}/api/pux/conversation/rename",
            json={"project": test_project, "agentId": agent_id, "title": "Second Title"},
        )
        assert resp.status_code == 200

        # Verify second title won
        conv_resp = api_session.get(f"{api_url}/api/pux/conversations")
        conversations = conv_resp.json()
        match = [c for c in conversations if c["project"] == test_project and c["agentId"] == agent_id]
        assert len(match) == 1
        assert match[0]["title"] == "Second Title"

        # Cleanup
        api_session.delete(
            f"{api_url}/api/pux/conversation?project={test_project}&agentId={agent_id}"
        )

    def test_rename_missing_project_returns_error(self, api_url, api_session):
        resp = api_session.put(
            f"{api_url}/api/pux/conversation/rename",
            json={"project": "", "agentId": "default", "title": "test"},
        )
        assert resp.status_code == 400

    def test_rename_missing_title_returns_error(self, api_url, api_session):
        resp = api_session.put(
            f"{api_url}/api/pux/conversation/rename",
            json={"project": "test", "agentId": "default"},
        )
        # The handler requires both project and title
        assert resp.status_code == 400

    def test_rename_nonexistent_conversation_succeeds(self, api_url, api_session):
        """Renaming a conversation that has no messages should still succeed (upsert)."""
        resp = api_session.put(
            f"{api_url}/api/pux/conversation/rename",
            json={"project": "ghost-project", "agentId": "ghost-agent", "title": "Ghost Title"},
        )
        assert resp.status_code == 200
        assert resp.json().get("success") is True


class TestConversationDelete:
    """DELETE /api/pux/conversation — delete conversation + title."""

    def test_delete_removes_conversation(self, api_url, api_session, test_project):
        """Create, rename, then delete a conversation — verify it's gone."""
        from utils.sse import stream_prompt
        agent_id = f"test-delete-{__name__}"
        try:
            stream_prompt(api_url, api_session, test_project, "say hello", agent_id=agent_id)
        except Exception:
            pass

        # Rename so we can verify both title and messages are deleted
        api_session.put(
            f"{api_url}/api/pux/conversation/rename",
            json={"project": test_project, "agentId": agent_id, "title": "To Be Deleted"},
        )

        # Confirm it exists
        conv_resp = api_session.get(f"{api_url}/api/pux/conversations")
        conversations = conv_resp.json()
        match = [c for c in conversations if c["project"] == test_project and c["agentId"] == agent_id]
        assert len(match) == 1

        # Delete — uses query params for project and agentId
        resp = api_session.delete(
            f"{api_url}/api/pux/conversation?project={test_project}&agentId={agent_id}"
        )
        assert resp.status_code == 200
        assert resp.json().get("success") is True

        # Verify it's gone from conversations
        conv_resp2 = api_session.get(f"{api_url}/api/pux/conversations")
        conversations2 = conv_resp2.json()
        match2 = [c for c in conversations2 if c["project"] == test_project and c["agentId"] == agent_id]
        assert len(match2) == 0, f"Conversation still in conversations after delete: {match2}"

    def test_delete_nonexistent_conversation_succeeds(self, api_url, api_session):
        """Deleting a conversation that doesn't exist should return success."""
        resp = api_session.delete(
            f"{api_url}/api/pux/conversation?project=nope&agentId=nope"
        )
        assert resp.status_code == 200
        assert resp.json().get("success") is True

    def test_delete_missing_params_returns_error(self, api_url, api_session):
        resp = api_session.delete(f"{api_url}/api/pux/conversation")
        # No project query param — requireProjectName returns 400
        assert resp.status_code == 400

    def test_delete_also_removes_title(self, api_url, api_session, test_project):
        """Verify that deleting a conversation also removes its custom title."""
        from utils.sse import stream_prompt
        agent_id = f"test-delete-title-{__name__}"
        try:
            stream_prompt(api_url, api_session, test_project, "say hello", agent_id=agent_id)
        except Exception:
            pass

        # Rename
        api_session.put(
            f"{api_url}/api/pux/conversation/rename",
            json={"project": test_project, "agentId": agent_id, "title": "Title To Be Nuked"},
        )

        # Delete
        resp = api_session.delete(
            f"{api_url}/api/pux/conversation?project={test_project}&agentId={agent_id}"
        )
        assert resp.status_code == 200

        # Re-create conversation with same ID — title should be auto-set from new first message
        try:
            stream_prompt(api_url, api_session, test_project, "hello again", agent_id=agent_id)
        except Exception:
            pass

        conv_resp = api_session.get(f"{api_url}/api/pux/conversations")
        conversations = conv_resp.json()
        match = [c for c in conversations if c["project"] == test_project and c["agentId"] == agent_id]
        if match:
            # Title should no longer be the old custom title
            assert match[0]["title"] != "Title To Be Nuked", f"Old custom title should be gone, got: {match[0]['title']!r}"

        # Cleanup
        api_session.delete(
            f"{api_url}/api/pux/conversation?project={test_project}&agentId={agent_id}"
        )


class TestConversationRoundTrip:
    """Full end-to-end: create -> rename -> verify -> delete -> verify gone."""

    def test_full_lifecycle(self, api_url, api_session, test_project):
        from utils.sse import stream_prompt
        agent_id = f"test-lifecycle-{__name__}"

        # Step 1: Create conversation by sending a prompt
        try:
            stream_prompt(api_url, api_session, test_project, "hello world", agent_id=agent_id)
        except Exception:
            pass

        # Step 2: Verify it appears in conversations
        conv_resp = api_session.get(f"{api_url}/api/pux/conversations")
        conversations = conv_resp.json()
        match = [c for c in conversations if c["project"] == test_project and c["agentId"] == agent_id]
        assert len(match) == 1, "Step 2: Conversation not in conversations"
        conv = match[0]
        assert conv["messageCount"] >= 1, f"Step 2: Expected messages, got {conv['messageCount']}"
        # Title is auto-set from the first user message
        assert conv["title"] != "", "Step 2: Title should be auto-set from first message"

        # Step 3: Rename it
        resp = api_session.put(
            f"{api_url}/api/pux/conversation/rename",
            json={"project": test_project, "agentId": agent_id, "title": "Integration Test Chat"},
        )
        assert resp.status_code == 200

        # Step 4: Verify new title in conversations
        conv_resp2 = api_session.get(f"{api_url}/api/pux/conversations")
        conversations2 = conv_resp2.json()
        match2 = [c for c in conversations2 if c["project"] == test_project and c["agentId"] == agent_id]
        assert len(match2) == 1
        assert match2[0]["title"] == "Integration Test Chat"

        # Step 5: Get messages via history — verify they exist
        msgs_resp = api_session.get(
            f"{api_url}/api/pux/history", params={"project": test_project, "agentId": agent_id}
        )
        assert msgs_resp.status_code == 200
        messages = msgs_resp.json()
        assert len(messages) >= 1, "Should have at least 1 message"

        # Step 6: Delete
        del_resp = api_session.delete(
            f"{api_url}/api/pux/conversation?project={test_project}&agentId={agent_id}"
        )
        assert del_resp.status_code == 200
        assert del_resp.json()["success"] is True

        # Step 7: Verify gone from conversations
        conv_resp3 = api_session.get(f"{api_url}/api/pux/conversations")
        conversations3 = conv_resp3.json()
        match3 = [c for c in conversations3 if c["project"] == test_project and c["agentId"] == agent_id]
        assert len(match3) == 0, "Step 7: Conversation should be gone after delete"

        # Step 8: Verify messages are gone
        msgs2_resp = api_session.get(
            f"{api_url}/api/pux/history", params={"project": test_project, "agentId": agent_id}
        )
        assert msgs2_resp.status_code == 200
        assert len(msgs2_resp.json()) == 0, "Step 8: Messages should be empty after delete"
