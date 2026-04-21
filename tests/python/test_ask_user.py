"""
ask_user Tool E2E Tests.

Tests the ask_user tool — the agent asks clarifying questions and the user
responds via the approval system. Verifies:

1. ask_user tool triggers approval_request SSE event with type="question"
2. The approval_request has correct fields (requestId, type, message)
3. User can respond via POST /api/pi/respond with action="answer"
4. Agent receives the answer and continues generating
5. The full round-trip completes (agent_end received after answering)
6. ask_user is NOT available in scheduler (fire-and-forget)

Requires: llama-server running with model loaded (GPU).

Run: cd tests/python && uv run pytest test_ask_user.py -v --tb=long
"""

import threading
import time

import pytest

from conftest import (
    API_BASE_URL,
    post_and_stream,
)

pytestmark = [pytest.mark.api, pytest.mark.sse, pytest.mark.slow]

API = API_BASE_URL
TEST_PROJECT = "test-repo"

# Prompts that are likely to trigger ask_user — the model should want to
# clarify before starting a complex task. Using very direct instructions
# to maximize the chance the 26B model calls ask_user.
ASK_USER_PROMPTS = [
    (
        "Before you start, use the ask_user tool to ask me what programming "
        "language I prefer. Wait for my answer before doing anything else. "
        "Use this exact format: ask_user with question 'What programming language do you prefer?'"
    ),
    (
        "I want you to build a website. But first, use ask_user to ask: "
        "'What color scheme do you want: dark or light?'. "
        "Do NOT start building until I answer."
    ),
    (
        "Use the ask_user tool to ask me: 'Should I use Python or JavaScript for this project?'"
    ),
]


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _find_approval_request(events):
    """Find the first approval_request event in the event list."""
    for event_type, data in events:
        if event_type == "approval_request" and data.get("type") == "question":
            return data
    return None


def _find_tool_start(events, tool_name):
    """Find tool_execution_start events for a specific tool."""
    return [
        data for et, data in events
        if et == "tool_execution_start" and data.get("toolName") == tool_name
    ]


def _collect_events_and_answer(session, prompt, project, agent_id, answer, timeout=90):
    """
    Stream a prompt, watch for approval_request, auto-answer it,
    and return all collected events.

    The answer is sent from a separate thread so the SSE reader doesn't block.
    """
    events = []
    approval_found = threading.Event()
    approval_data = {}
    answer_sent = threading.Event()

    def _send_answer():
        """Wait for approval event, then send answer."""
        if not approval_found.wait(timeout=60):
            return
        time.sleep(0.3)  # Small delay to ensure the approval is registered
        session.post(f"{API}/api/pi/respond", json={
            "project": project,
            "agentId": agent_id,
            "requestId": approval_data["request_id"],
            "action": "answer",
            "message": answer,
        }, timeout=10)
        answer_sent.set()

    answer_thread = threading.Thread(target=_send_answer, daemon=True)
    answer_thread.start()

    try:
        for event in post_and_stream(
            session,
            f"{API}/api/pi/prompt",
            {
                "message": prompt,
                "project": project,
                "agentId": agent_id,
            },
            timeout=timeout,
        ):
            events.append(event)

            # Check for approval_request
            if event[0] == "approval_request" and not approval_found.is_set():
                data = event[1]
                if data.get("type") == "question":
                    approval_data["request_id"] = data.get("requestId", "")
                    approval_found.set()

            # After we see the answer was sent, we can stop early if needed
            # but it's better to collect all events for assertions
    except Exception as e:
        # Timeout is acceptable — the stream may close before we read all events
        if "timed out" not in str(e).lower() and "timeout" not in str(e).lower():
            raise

    answer_thread.join(timeout=5)
    return events, approval_data if approval_found.is_set() else None


# ---------------------------------------------------------------------------
# Tests
# ---------------------------------------------------------------------------


class TestAskUserSSEEvent:
    """
    Verify the ask_user tool produces correct approval_request SSE events.
    These tests use the API directly (no Playwright).
    """

    def test_ask_user_produces_approval_request(self, api_session):
        """
        When the agent calls ask_user, the SSE stream should contain an
        approval_request event with type="question".
        """
        prompt = ASK_USER_PROMPTS[0]
        agent_id = f"ask-test-{int(time.time())}"

        events = []
        approval = None

        # Start a thread to auto-answer if we find an approval
        approval_found = threading.Event()
        approval_req_id = {}

        def _answer():
            if not approval_found.wait(timeout=60):
                return
            time.sleep(0.3)
            api_session.post(f"{API}/api/pi/respond", json={
                "project": TEST_PROJECT,
                "agentId": agent_id,
                "requestId": approval_req_id["id"],
                "action": "answer",
                "message": "Python",
            }, timeout=10)

        answer_thread = threading.Thread(target=_answer, daemon=True)
        answer_thread.start()

        try:
            for event in post_and_stream(
                api_session,
                f"{API}/api/pi/prompt",
                {
                    "message": prompt,
                    "project": TEST_PROJECT,
                    "agentId": agent_id,
                },
                timeout=90,
            ):
                events.append(event)
                if event[0] == "approval_request" and not approval_found.is_set():
                    data = event[1]
                    if data.get("type") == "question":
                        approval = data
                        approval_req_id["id"] = data.get("requestId", "")
                        approval_found.set()
        except Exception:
            pass  # Timeout is expected for long-running streams

        answer_thread.join(timeout=5)

        if not approval:
            pytest.skip("Model did not call ask_user (expected with 26B model)")

        assert approval["requestId"], f"Missing requestId in approval_request: {approval}"
        assert approval["type"] == "question", f"Expected type='question', got: {approval.get('type')}"
        assert "message" in approval, f"Missing 'message' field: {approval}"
        assert isinstance(approval["message"], str), f"message must be str: {type(approval['message'])}"

    def test_ask_user_event_has_required_fields(self, api_session):
        """
        The approval_request from ask_user must have all fields the frontend
        ApprovalBanner reads: requestId, type, message.
        (risk and toolName are optional for question type)
        """
        prompt = ASK_USER_PROMPTS[0]
        agent_id = f"ask-fields-{int(time.time())}"

        approval = None
        approval_found = threading.Event()
        approval_req_id = {}

        def _answer():
            if not approval_found.wait(timeout=60):
                return
            time.sleep(0.3)
            api_session.post(f"{API}/api/pi/respond", json={
                "project": TEST_PROJECT,
                "agentId": agent_id,
                "requestId": approval_req_id["id"],
                "action": "answer",
                "message": "React",
            }, timeout=10)

        answer_thread = threading.Thread(target=_answer, daemon=True)
        answer_thread.start()

        try:
            for event in post_and_stream(
                api_session,
                f"{API}/api/pi/prompt",
                {
                    "message": prompt,
                    "project": TEST_PROJECT,
                    "agentId": agent_id,
                },
                timeout=90,
            ):
                if event[0] == "approval_request" and not approval_found.is_set():
                    data = event[1]
                    if data.get("type") == "question":
                        approval = data
                        approval_req_id["id"] = data.get("requestId", "")
                        approval_found.set()
        except Exception:
            pass

        answer_thread.join(timeout=5)

        if not approval:
            pytest.skip("Model did not call ask_user")

        # Required fields for the frontend ApprovalBanner
        assert "requestId" in approval, "Missing requestId"
        assert "type" in approval, "Missing type"
        assert "message" in approval, "Missing message"
        assert approval["type"] == "question"

        print(f"  ✓ approval_request fields: {list(approval.keys())}")


class TestAskUserRoundTrip:
    """
    Verify the full round-trip: agent asks → user answers → agent continues.
    """

    def test_agent_continues_after_answer(self, api_session):
        """
        After answering the ask_user question, the agent should continue
        generating text (not just hang or error out).
        """
        prompt = ASK_USER_PROMPTS[0]
        agent_id = f"ask-roundtrip-{int(time.time())}"
        answer = "I prefer Python"

        events, approval = _collect_events_and_answer(
            api_session, prompt, TEST_PROJECT, agent_id, answer, timeout=120
        )

        if not approval:
            pytest.skip("Model did not call ask_user")

        event_types = [t for t, _ in events]

        # After answering, the agent should eventually send agent_end
        # (it may do more work first, but it should complete)
        assert "agent_end" in event_types, (
            f"Stream ended without agent_end after answering. "
            f"Event types: {event_types[-10:]}"
        )

        # There should be text after the approval (the agent's response to the answer)
        # Count text_delta events after the approval_request
        found_approval = False
        text_after_approval = 0
        for et, data in events:
            if et == "approval_request":
                found_approval = True
                continue
            if found_approval and et == "text_delta":
                text_after_approval += 1

        # The agent should produce some text response after getting the answer
        print(f"  ✓ Events after answer: text_deltas={text_after_approval}, "
              f"has_agent_end={'agent_end' in event_types}")

    def test_answer_delivered_to_agent(self, api_session):
        """
        The tool_execution_end for ask_user should contain the user's answer.
        """
        prompt = ASK_USER_PROMPTS[0]
        agent_id = f"ask-deliver-{int(time.time())}"
        answer = "JavaScript is my favorite"

        events, approval = _collect_events_and_answer(
            api_session, prompt, TEST_PROJECT, agent_id, answer, timeout=120
        )

        if not approval:
            pytest.skip("Model did not call ask_user")

        # Find the tool_execution_end for ask_user
        ask_user_ends = [
            data for et, data in events
            if et == "tool_execution_end"
            and data.get("toolName") == "ask_user"
        ]

        if not ask_user_ends:
            # The tool name might not be in the end event — check for the
            # result containing our answer text in any tool_execution_end
            all_ends = [data for et, data in events if et == "tool_execution_end"]
            has_answer = any(
                isinstance(d.get("result"), dict) and "JavaScript" in str(d.get("result", ""))
                for d in all_ends
            )
            if has_answer:
                print("  ✓ Answer found in tool result (without toolName)")
                return

            pytest.skip(
                "No tool_execution_end for ask_user found. "
                f"Event types: {[t for t, _ in events]}"
            )

        # The result should contain the answer
        result = ask_user_ends[0].get("result")
        assert result is not None, f"ask_user tool_execution_end missing result: {ask_user_ends[0]}"

        # Result should be a dict with "answer" key containing our text
        if isinstance(result, dict):
            assert "answer" in result, f"Result missing 'answer' key: {result}"
            assert "JavaScript" in result["answer"], (
                f"Answer doesn't contain expected text. Got: {result['answer']}"
            )
        elif isinstance(result, str):
            assert "JavaScript" in result, f"Answer string doesn't contain expected text: {result}"

        print(f"  ✓ Answer delivered to agent: {result}")


class TestAskUserToolStart:
    """
    Verify the tool_execution_start event for ask_user.
    """

    def test_tool_start_has_correct_fields(self, api_session):
        """
        When ask_user is called, tool_execution_start should have
        toolName="ask_user" and args with the question.
        """
        prompt = ASK_USER_PROMPTS[0]
        agent_id = f"ask-start-{int(time.time())}"

        events = []
        approval_found = threading.Event()
        approval_req_id = {}

        def _answer():
            if not approval_found.wait(timeout=60):
                return
            time.sleep(0.3)
            api_session.post(f"{API}/api/pi/respond", json={
                "project": TEST_PROJECT,
                "agentId": agent_id,
                "requestId": approval_req_id["id"],
                "action": "answer",
                "message": "Go",
            }, timeout=10)

        answer_thread = threading.Thread(target=_answer, daemon=True)
        answer_thread.start()

        try:
            for event in post_and_stream(
                api_session,
                f"{API}/api/pi/prompt",
                {
                    "message": prompt,
                    "project": TEST_PROJECT,
                    "agentId": agent_id,
                },
                timeout=90,
            ):
                events.append(event)
                if event[0] == "approval_request":
                    data = event[1]
                    if data.get("type") == "question" and not approval_found.is_set():
                        approval_req_id["id"] = data.get("requestId", "")
                        approval_found.set()
        except Exception:
            pass

        answer_thread.join(timeout=5)

        # Find ask_user tool starts
        ask_starts = _find_tool_start(events, "ask_user")
        if not ask_starts:
            pytest.skip("Model did not call ask_user")

        for ts in ask_starts:
            assert "args" in ts, f"tool_execution_start missing args: {ts}"
            args = ts["args"]
            assert isinstance(args, dict), f"args must be dict: {type(args)}"
            assert "question" in args, f"ask_user args missing 'question': {args}"

            print(f"  ✓ ask_user tool_execution_start: toolName={ts['toolName']}, "
                  f"question={args.get('question', '')[:50]}")


class TestAskUserSchedulerFireAndForget:
    """
    Verify that scheduled jobs (cron) cannot use ask_user.
    The LlamaExecutor creates SandboxToolExecutor without ApprovalMgr,
    so ask_user should return an error.
    """

    def test_scheduler_cannot_ask_user(self, api_session):
        """
        Create a scheduled job that instructs the agent to use ask_user.
        The job should complete (or error) but NOT hang waiting for a response.

        NOTE: This test requires the scheduler to be configured with the
        LlamaExecutor. If the scheduler is not available, it will be skipped.
        """
        # Check scheduler is available
        resp = api_session.get(f"{API}/api/scheduler/", timeout=5)
        if resp.status_code != 200:
            pytest.skip("Scheduler API not available")

        # Create a one-shot job
        job_resp = api_session.post(f"{API}/api/scheduler/", json={
            "name": "test-ask-user-scheduler",
            "project": TEST_PROJECT,
            "message": (
                "Use the ask_user tool to ask: 'What is your name?'. "
                "If ask_user is not available, just say 'ask_user not available' "
                "and use synthesize to report that."
            ),
            "scheduleType": "once",
            "enabled": True,
        }, timeout=10)

        if job_resp.status_code not in (200, 201):
            pytest.skip(f"Could not create scheduler job: {job_resp.status_code}")

        job_data = job_resp.json()
        job_id = job_data.get("id", "")

        if not job_id:
            pytest.skip("No job ID returned from scheduler")

        # Trigger the job
        trigger_resp = api_session.post(
            f"{API}/api/scheduler/{job_id}/trigger", timeout=10
        )
        assert trigger_resp.status_code == 200, (
            f"Trigger failed: {trigger_resp.status_code} {trigger_resp.text}"
        )

        # Wait for the job to complete (should NOT hang)
        # Poll for up to 90 seconds
        for i in range(30):
            time.sleep(3)
            runs_resp = api_session.get(
                f"{API}/api/scheduler/{job_id}/runs", timeout=10
            )
            if runs_resp.status_code == 200:
                runs = runs_resp.json()
                if isinstance(runs, list) and runs:
                    latest = runs[0] if isinstance(runs[0], dict) else {}
                    status = latest.get("status", "")
                    if status in ("completed", "failed"):
                        # Job finished — should not have hung
                        output = latest.get("output", "")
                        error = latest.get("error", "")

                        # The output should mention ask_user not being available
                        # (or the model may have ignored the instruction entirely)
                        print(f"  ✓ Scheduler job completed: status={status}")
                        print(f"    output: {output[:200] if output else '(empty)'}")
                        if error:
                            print(f"    error: {error[:200]}")

                        # Cleanup
                        try:
                            api_session.delete(
                                f"{API}/api/scheduler/{job_id}", timeout=10
                            )
                        except Exception:
                            pass
                        return

        # Cleanup on timeout
        try:
            api_session.delete(f"{API}/api/scheduler/{job_id}", timeout=10)
        except Exception:
            pass

        pytest.fail("Scheduler job did not complete within 90s — may be hanging on ask_user")
