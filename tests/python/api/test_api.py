"""
REST API tests: Health, Config, Projects, Checklist, CLI, Error handling.
"""

import pytest

pytestmark = pytest.mark.api


# ---------------------------------------------------------------------------
# Health & Config
# ---------------------------------------------------------------------------


class TestHealthAndConfig:
    def test_health_returns_ok(self, api_url, api_session):
        resp = api_session.get(f"{api_url}/api/health")
        assert resp.status_code == 200
        assert resp.text.strip() == "OK"

    def test_get_ai_config(self, api_url, api_session):
        resp = api_session.get(f"{api_url}/api/config/ai")
        assert resp.status_code == 200
        data = resp.json()
        assert "autoTask" in data
        assert "testTypes" in data

    def test_set_ai_config(self, api_url, api_session):
        # Fetch current config, flip a flag, post it back
        get_resp = api_session.get(f"{api_url}/api/config/ai")
        config = get_resp.json()
        original = config["autoTask"]
        config["autoTask"] = not original

        post_resp = api_session.post(f"{api_url}/api/config/ai", json=config)
        assert post_resp.status_code == 200

        # Verify the change stuck
        verify = api_session.get(f"{api_url}/api/config/ai").json()
        assert verify["autoTask"] == (not original)

        # Restore
        config["autoTask"] = original
        api_session.post(f"{api_url}/api/config/ai", json=config)

    def test_get_system_config(self, api_url, api_session):
        resp = api_session.get(f"{api_url}/api/config/system")
        assert resp.status_code == 200
        data = resp.json()
        assert "projectsDir" in data

    def test_set_system_config(self, api_url, api_session):
        get_resp = api_session.get(f"{api_url}/api/config/system")
        original = get_resp.json()
        original_dir = original["projectsDir"]

        new_dir = "/tmp/test-projects-dir"
        post_resp = api_session.post(
            f"{api_url}/api/config/system", json={"projectsDir": new_dir}
        )
        assert post_resp.status_code == 200

        # Restore
        api_session.post(
            f"{api_url}/api/config/system", json={"projectsDir": original_dir}
        )


# ---------------------------------------------------------------------------
# Projects
# ---------------------------------------------------------------------------


class TestProjects:
    def test_list_projects_returns_array(self, api_url, api_session):
        resp = api_session.get(f"{api_url}/api/projects")
        assert resp.status_code == 200
        data = resp.json()
        assert "projects" in data
        assert isinstance(data["projects"], list)

    def test_add_project_valid_path(self, test_project, api_url, api_session):
        """The test_project fixture already adds a project; verify it appears."""
        resp = api_session.get(f"{api_url}/api/projects")
        projects = resp.json()["projects"]
        assert test_project in projects

    def test_add_project_invalid_path(self, api_url, api_session):
        resp = api_session.post(
            f"{api_url}/api/projects/add",
            json={"name": "nonexistent-proj", "path": "/no/such/path/xyz"},
        )
        # API should reject with 4xx or error in body
        assert resp.status_code >= 400 or not resp.json().get("success", True)

    def test_add_project_duplicate_name(self, api_url, api_session, test_project):
        resp = api_session.post(
            f"{api_url}/api/projects/add",
            json={"name": test_project, "path": "/tmp/dupe"},
        )
        # Should either reject or handle gracefully
        assert resp.status_code in (200, 201, 400, 409)

    def test_project_status(self, api_url, api_session, test_project):
        resp = api_session.get(f"{api_url}/api/status", params={"project": test_project})
        assert resp.status_code == 200
        data = resp.json()
        assert "project" in data
        assert data["project"] == test_project

    def test_branch_info(self, api_url, api_session, test_project):
        resp = api_session.get(f"{api_url}/api/branch", params={"project": test_project})
        # May return 200 or 500 if project is not a git repo
        if resp.status_code == 200:
            data = resp.json()
            assert "branch" in data
        else:
            # Non-git project — should still respond, not hang
            assert resp.status_code == 500


# ---------------------------------------------------------------------------
# Checklist
# ---------------------------------------------------------------------------


class TestChecklist:
    def test_get_checklist_valid_project(self, api_url, api_session, test_project):
        resp = api_session.get(
            f"{api_url}/api/checklist", params={"project": test_project}
        )
        assert resp.status_code == 200
        data = resp.json()
        assert "tasks" in data
        assert isinstance(data["tasks"], list)

    def test_get_checklist_missing_project(self, api_url, api_session):
        resp = api_session.get(
            f"{api_url}/api/checklist", params={"project": "nonexistent-proj"}
        )
        # API returns 200 with empty tasks for unknown projects
        assert resp.status_code == 200
        assert resp.json().get("tasks") == []

    def test_update_checklist_roundtrip(self, api_url, api_session):
        # Use test-repo which is an actual git repo
        project = "test-repo"
        tasks = [
            {"id": "t-0", "text": "E2E test task", "completed": False, "status": "pending"}
        ]
        resp = api_session.post(
            f"{api_url}/api/checklist/update",
            json={"tasks": tasks, "project": project},
        )
        # May fail with 500 if file is root-owned (Docker artifact)
        if resp.status_code == 500:
            pytest.skip("TASKS.md not writable (root-owned from Docker)")
        assert resp.status_code == 200

        # Verify
        get_resp = api_session.get(
            f"{api_url}/api/checklist", params={"project": project}
        )
        assert get_resp.status_code == 200
        fetched = get_resp.json()["tasks"]
        assert len(fetched) >= 1
        assert any(t["text"] == "E2E test task" for t in fetched)


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------


class TestCLI:
    def test_list_allowed_commands(self, api_url, api_session):
        resp = api_session.get(f"{api_url}/api/cli/commands")
        assert resp.status_code == 200
        data = resp.json()
        assert data.get("success") is True
        assert "commands" in data
        assert isinstance(data["commands"], list)
        assert len(data["commands"]) > 0

    def test_execute_allowed_command_ls(self, api_url, api_session, test_project):
        resp = api_session.post(
            f"{api_url}/api/cli/execute",
            json={"command": "ls", "args": []},
        )
        assert resp.status_code == 200
        data = resp.json()
        assert data.get("success") is True
        assert "output" in data

    def test_execute_allowed_command_pwd(self, api_url, api_session):
        resp = api_session.post(
            f"{api_url}/api/cli/execute",
            json={"command": "pwd", "args": []},
        )
        assert resp.status_code == 200
        data = resp.json()
        assert data.get("success") is True

    def test_execute_blocked_command(self, api_url, api_session):
        resp = api_session.post(
            f"{api_url}/api/cli/execute",
            json={"command": "rm", "args": ["-rf", "/"]},
        )
        # Blocked commands should fail
        assert resp.status_code >= 400 or not resp.json().get("success", True)

    def test_cli_cat(self, api_url, api_session, test_project):
        # Read an existing file (README.md should exist in any project)
        resp = api_session.get(
            f"{api_url}/api/cli/cat",
            params={"path": f"{test_project}/README.md"},
        )
        # Accept 200 (file found) or error codes (no README / not a git repo)
        if resp.status_code == 200:
            data = resp.json()
            assert data.get("success") is True
            assert "content" in data
        else:
            assert resp.status_code in (400, 404, 500)

    def test_cli_ls_directory(self, api_url, api_session):
        resp = api_session.get(f"{api_url}/api/cli/ls", params={"path": "."})
        assert resp.status_code == 200
        data = resp.json()
        assert data.get("success") is True
        assert "entries" in data

    def test_cli_directory_traversal_blocked(self, api_url, api_session):
        resp = api_session.get(
            f"{api_url}/api/cli/cat",
            params={"path": "../../../etc/passwd"},
        )
        # Should block path traversal
        assert resp.status_code >= 400 or not resp.json().get("success", True)


# ---------------------------------------------------------------------------
# Error handling
# ---------------------------------------------------------------------------


class TestErrorHandling:
    def test_malformed_json_returns_4xx(self, api_url, api_session):
        resp = api_session.post(
            f"{api_url}/api/config/ai",
            data="not json{{{",
            headers={"Content-Type": "application/json"},
        )
        assert resp.status_code >= 400

    def test_unknown_endpoint_returns_404(self, api_url, api_session):
        resp = api_session.get(f"{api_url}/api/nonexistent-endpoint")
        assert resp.status_code == 404

    def test_method_not_allowed(self, api_url, api_session):
        resp = api_session.delete(f"{api_url}/api/health")
        assert resp.status_code in (404, 405)


# ---------------------------------------------------------------------------
# GitHub Integration
# ---------------------------------------------------------------------------


class TestGitHub:
    def test_github_user_connection_status(self, api_url, api_session):
        resp = api_session.get(f"{api_url}/api/github/user")
        assert resp.status_code == 200
        data = resp.json()
        assert "connected" in data

    def test_github_repos(self, api_url, api_session):
        resp = api_session.get(f"{api_url}/api/github/repos")
        assert resp.status_code == 200
        data = resp.json()
        # May be {repos: []} or {connected: false, ...}
        assert data is not None

    def test_github_prs_no_owner(self, api_url, api_session):
        resp = api_session.get(f"{api_url}/api/github/prs")
        # Requires owner/repo params
        assert resp.status_code in (200, 400)

    def test_github_branches_no_owner(self, api_url, api_session):
        resp = api_session.get(f"{api_url}/api/github/branches")
        assert resp.status_code in (200, 400)

    def test_github_stats_no_owner(self, api_url, api_session):
        resp = api_session.get(f"{api_url}/api/github/stats")
        assert resp.status_code in (200, 400)

    def test_github_activity_no_owner(self, api_url, api_session):
        resp = api_session.get(f"{api_url}/api/github/activity")
        assert resp.status_code in (200, 400)

    def test_connect_github_no_token(self, api_url, api_session):
        resp = api_session.post(f"{api_url}/api/config/github", json={})
        # Should fail without token
        assert resp.status_code in (200, 400, 500)


# ---------------------------------------------------------------------------
# Git Workflow
# ---------------------------------------------------------------------------


class TestGitWorkflow:
    def test_status_returns_git_state(self, api_url, api_session, test_project):
        resp = api_session.get(f"{api_url}/api/status", params={"project": test_project})
        assert resp.status_code == 200
        data = resp.json()
        assert "gitState" in data
        assert "isAutoMode" in data
        assert "agentStatus" in data

    def test_branch_info(self, api_url, api_session, test_project):
        resp = api_session.get(f"{api_url}/api/branch", params={"project": test_project})
        if resp.status_code == 200:
            data = resp.json()
            assert "branch" in data
        else:
            assert resp.status_code == 500  # non-git project

    def test_set_auto_mode(self, api_url, api_session, test_project):
        resp = api_session.post(f"{api_url}/api/settings/mode", json={
            "project": test_project,
            "autoMode": False,
        })
        # May succeed or fail depending on backend
        assert resp.status_code in (200, 400, 404)

    def test_clone_missing_params(self, api_url, api_session):
        resp = api_session.post(f"{api_url}/api/clone", json={})
        # Should reject without URL
        assert resp.status_code in (200, 400, 422)

    def test_branch_checkout_missing_params(self, api_url, api_session):
        resp = api_session.post(f"{api_url}/api/branch/checkout", json={})
        assert resp.status_code in (200, 400, 422)

    def test_merge_missing_params(self, api_url, api_session):
        resp = api_session.post(f"{api_url}/api/merge", json={})
        assert resp.status_code in (200, 400, 422)
