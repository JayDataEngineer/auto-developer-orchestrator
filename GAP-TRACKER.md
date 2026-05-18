# Gap Tracker — Outstanding Test & Code Issues

Last updated: 2026-05-18

## 1. Flaky LLM-Dependent Tests (4 failures)

These tests pass code-wise but fail due to LLM quality or backend instability:

| Test | File | Root Cause |
|------|------|------------|
| `test_text_delta_events_present` | `sse/test_pi_agent.py:119` | Local LLM sometimes produces no text content (error events instead of text_delta) |
| `test_agent_end_has_usage` | `sse/test_pi_agent.py:133` | LLM returns zero token usage when it fails to generate |
| `test_respond_no_pending_approval` | `agent/test_approval_flow.py:220` | Backend crashes during long test runs (connection refused) |
| `test_respond_deny_no_pending` | `agent/test_approval_flow.py:233` | Same — backend connection lost |

**Fix needed**: Add retry/skip logic for LLM-dependent tests. Check `agent_end` usage only when no error events present. Increase backend resilience for long test runs.

## 2. Slow LLM Integration Tests

Tests in `agent/test_goals.py`, `sse/test_tool_lifecycle.py`, `sse/test_agent_lifecycle.py::TestMultiTurnConversation` each take 60-180 seconds because they wait for LLM streaming to complete. The full suite takes 4+ minutes.

**Fix needed**: Add a `@pytest.mark.llm` marker and a `--skip-llm` CLI flag. Tests that require LLM generation should be skippable in CI.

## 3. Frontend React Mount Failure

The frontend has an intermittent "Invalid hook call" React error that prevents the app from mounting. The `#root` div stays empty. This causes:
- `test_frontend_ui.py` tests to skip (19/19 SKIPPED)
- Some browser/desktop tests to skip

The 3 `test_history.py` tests pass because the app somehow renders in that state.

**Fix needed**: Investigate the "Invalid hook call" error in the React app. Likely a hook ordering issue in one of the component render paths.

## 4. No Project Test Files (vitest)

`src/`, `shared/`, and `ts-tui-ink/` have zero test files. The `vitest.config.ts` is ready but there's nothing to run.

**Fix needed**: Add unit tests for:
- `shared/src/pux-chat-adapter.ts` (SSE parsing, state management)
- `shared/src/pux-store.ts` (Zustand store actions)
- `ts-tui-ink/src/components/` (Ink component rendering)

## 5. Unstaged Pre-existing Changes

These files were dirty before the test-fix session and remain unstaged:

| File | What changed |
|------|-------------|
| `go-backend/internal/hooks/permission.go` | Added `file_read`, `file_edit`, `delegate_to`, `delegate_async` to tool arg formatters |
| `go-backend/internal/perms/tool_permissions.go` | Replaced old tool names (`write`, `edit`, `delete`) with new ones (`file_read`, `file_write`, `file_edit`, etc.), added persistence |
| `shared/src/pux-chat-adapter.ts` | Fixed race: close tool calls that never got `tool_execution_end` before `subagent_end` |
| `src/web/src/components/assistant-ui/thread.tsx` | UI changes |
| `src/web/src/components/workbench/prompt-panel.tsx` | UI changes (prompt panel redesign) |
| `memos/review-config-report.md` | Doc update |

**Action needed**: Review and commit these separately — they're feature changes, not test fixes.

## 6. Untracked Files

| Path | Description |
|------|-------------|
| `example.png` | Screenshot — should be gitignored |
| `go-backend/go-backend/` | Looks like accidental binary output — should be gitignored |
| `memos/dune-themes.txt` | Notes file |
| `memos/meaning-of-dune.md` | Notes file |
| `src/web/next-app/` | Experimental Next.js app? |
| `tests/python/frontend/screenshots/` | Test artifacts — should be gitignored |
| `tool-ui/` | Reference UI code — should be gitignored or documented |

**Action needed**: Add to `.gitignore` or commit if intentional.

## 7. Todo Test Rewrite (Already Committed)

The `go-backend/internal/tools/todo/todo_test.go` was completely rewritten to match the new full-state-replacement API (`todos` array) instead of the old action-based API (`add`/`update`/`delete`). This was committed separately by the Playwright agent.

## 8. Backend Stability

The Go backend crashes during long Python test runs (4+ minutes). Tests after the crash get `ConnectionError`. This affects:
- `agent/test_approval_flow.py` — backend dies mid-suite
- `sse/test_agent_lifecycle.py` — backend dies during multi-turn tests

**Fix needed**: Investigate backend memory leaks or goroutine leaks during extended SSE streaming.
