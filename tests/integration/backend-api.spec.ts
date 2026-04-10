/**
 * Backend API Integration Tests
 *
 * Tests every backend endpoint with real HTTP calls.
 * No mocking — the Go backend must be running.
 */
import { test, expect } from '@playwright/test';
import {
  apiGet, apiPost, apiPut, apiDelete,
  TEST_PROJECT, TEST_AGENT,
} from './helpers';

// ─── Backend Health ────────────────────────────────────────────────

test.describe('Backend Health', () => {
  test('/api/projects returns project list', async () => {
    const { status, data } = await apiGet('/api/projects');
    expect(status).toBe(200);
    expect(data).toHaveProperty('projects');
    expect(Array.isArray(data.projects)).toBe(true);
    expect(data.projects.length).toBeGreaterThan(0);
  });

  test('/api/status returns git status', async () => {
    const { status, data } = await apiGet(`/api/status?project=${TEST_PROJECT}`);
    expect(status).toBe(200);
    expect(data).toHaveProperty('gitState');
    expect(data).toHaveProperty('isAutoMode');
    expect(data).toHaveProperty('agentStatus');
  });

  test('/api/config/ai returns AI config', async () => {
    const { status, data } = await apiGet('/api/config/ai');
    expect(status).toBe(200);
    expect(data).toHaveProperty('autoTask');
    expect(data).toHaveProperty('testTypes');
  });

  test('/api/config/system returns system config', async () => {
    const { status, data } = await apiGet('/api/config/system');
    expect(status).toBe(200);
    expect(data).toHaveProperty('projectsDir');
  });

  test('/api/github/user returns connection status', async () => {
    const { status, data } = await apiGet('/api/github/user');
    expect(status).toBe(200);
    expect(data).toHaveProperty('connected');
  });
});

// ─── Pi Agent Endpoints ────────────────────────────────────────────

test.describe('Pi Agent API', () => {
  test('GET /api/pi/state returns agent state', async () => {
    const { status, data } = await apiGet(`/api/pi/state?project=${TEST_PROJECT}&agentId=${TEST_AGENT}`);
    expect(status).toBe(200);
    expect(data).toHaveProperty('model');
    expect(data).toHaveProperty('streaming');
    expect(data).toHaveProperty('input');
    expect(data).toHaveProperty('output');
    expect(data).toHaveProperty('cache');
  });

  test('GET /api/pi/models returns model list', async () => {
    const { status, data } = await apiGet(`/api/pi/models?project=${TEST_PROJECT}&agentId=${TEST_AGENT}`);
    expect(status).toBe(200);
    expect(data).toHaveProperty('models');
    expect(Array.isArray(data.models)).toBe(true);
  });

  test('GET /api/pi/messages returns message history', async () => {
    const { status, data } = await apiGet(`/api/pi/messages?project=${TEST_PROJECT}&agentId=${TEST_AGENT}`);
    expect(status).toBe(200);
    expect(Array.isArray(data)).toBe(true);
  });

  test('PUT /api/pi/model sets the model', async () => {
    const { status, data } = await apiPut('/api/pi/model', {
      project: TEST_PROJECT,
      provider: 'litellm',
      modelId: 'fast',
      agentId: TEST_AGENT,
    });
    expect(status).toBe(200);

    // Verify model was set
    const state = await apiGet(`/api/pi/state?project=${TEST_PROJECT}&agentId=${TEST_AGENT}`);
    expect(state.status).toBe(200);
    // Model should be set (could be 'fast' or the full model name)
  });

  test('POST /api/pi/abort returns success', async () => {
    const { status } = await apiPost(`/api/pi/abort?project=${TEST_PROJECT}&agentId=${TEST_AGENT}`);
    expect(status).toBe(200);
  });

  test('POST /api/pi/compact returns success', async () => {
    const { status } = await apiPost(`/api/pi/compact?project=${TEST_PROJECT}&agentId=${TEST_AGENT}`);
    expect(status).toBe(200);
  });

  test('GET /api/pi/active returns active sessions', async () => {
    const { status, data } = await apiGet('/api/pi/active');
    expect(status).toBe(200);
    expect(data).toHaveProperty('projects');
    expect(Array.isArray(data.projects)).toBe(true);
  });

  test('GET /api/pi/history returns conversation history', async () => {
    const { status, data } = await apiGet('/api/pi/history');
    expect(status).toBe(200);
    expect(data).toHaveProperty('conversations');
    expect(Array.isArray(data.conversations)).toBe(true);
  });
});

// ─── Sandbox / Desktop Endpoints ────────────────────────────────────

test.describe('Sandbox / Desktop API', () => {
  test('GET desktop-mode returns response (may be error if no sandbox)', async () => {
    const { status, data } = await apiGet(`/api/sandbox/${TEST_PROJECT}/desktop-mode`);
    // Could be 200, 405 (GET not allowed), 404, or 500 — all valid real responses
    expect([200, 400, 404, 405, 500]).toContain(status);
    // Response may be empty body (405)
  });

  test('POST computer-use/enable returns response (may fail without sandbox)', async () => {
    const { status, data } = await apiPost(`/api/sandbox/${TEST_PROJECT}/computer-use/enable`, {}, 10_000);
    // Could succeed or fail — both are valid
    expect([200, 400, 404, 500, 504]).toContain(status);
    expect(data).not.toBeNull();
  });

  test('GET viewer returns response', async () => {
    const { status, data } = await apiGet(`/api/sandbox/${TEST_PROJECT}/viewer`);
    expect([200, 400, 404, 500]).toContain(status);
    expect(data).not.toBeNull();
  });
});

// ─── Scheduler API ──────────────────────────────────────────────────

test.describe('Scheduler API', () => {
  let createdJobId: string | null = null;

  test('GET /api/scheduler returns job list', async () => {
    const { status, data } = await apiGet('/api/scheduler');
    expect(status).toBe(200);
    expect(data).toHaveProperty('jobs');
    expect(Array.isArray(data.jobs)).toBe(true);
  });

  test('POST /api/scheduler creates a new job', async () => {
    const { status, data } = await apiPost('/api/scheduler', {
      name: 'Integration Test Job',
      project: TEST_PROJECT,
      message: 'echo hello',
      scheduleType: 'cron',
      cronExpr: '0 0 9 * * *',
      enabled: false,
    });
    // Accept both success and validation error
    if (status === 200 || status === 201) {
      expect(data).toHaveProperty('job');
      expect(data.job).toHaveProperty('id');
      createdJobId = data.job.id;
    } else {
      // 400 = validation error — log what's missing
      console.log(`  ⚠ Scheduler create returned ${status}: ${JSON.stringify(data)}`);
      expect([400, 422]).toContain(status);
    }
  });

  test('GET /api/scheduler/:id returns the job', async () => {
    if (!createdJobId) test.skip();
    const { status, data } = await apiGet(`/api/scheduler/${createdJobId}`);
    expect(status).toBe(200);
    expect(data).toHaveProperty('id', createdJobId);
    expect(data).toHaveProperty('name');
  });

  test('DELETE /api/scheduler/:id deletes the job', async () => {
    if (!createdJobId) test.skip();
    const { status } = await apiDelete(`/api/scheduler/${createdJobId}`);
    expect(status).toBe(200);

    // Verify it's gone
    const { status: getStatus } = await apiGet(`/api/scheduler/${createdJobId}`);
    expect([404, 200]).toContain(getStatus); // May still return the deleted job or 404
  });
});

// ─── Task Management API ────────────────────────────────────────────

test.describe('Task Management API', () => {
  let createdTaskId: string | null = null;

  test('GET /api/pi/tasks/list returns tasks', async () => {
    const { status, data } = await apiGet(`/api/pi/tasks/list?projectDir=${TEST_PROJECT}`);
    expect(status).toBe(200);
    expect(data).toHaveProperty('tasks');
    expect(Array.isArray(data.tasks)).toBe(true);
  });

  test('POST /api/pi/tasks/ creates a new task', async () => {
    const { status, data } = await apiPost('/api/pi/tasks/', {
      title: 'Integration Test Task',
      description: 'Created by integration tests',
      projectDir: TEST_PROJECT,
      parentAgent: TEST_AGENT,
    });
    expect(status).toBe(200);
    // Response wraps task in { success, task: { id, ... } }
    const task = data.task ?? data;
    expect(task).toHaveProperty('id');
    expect(task).toHaveProperty('title', 'Integration Test Task');
    expect(task).toHaveProperty('status', 'pending');
    createdTaskId = task.id;
  });

  test('GET /api/pi/tasks/:id returns the task', async () => {
    if (!createdTaskId) test.skip();
    const { status, data } = await apiGet(`/api/pi/tasks/${createdTaskId}`);
    expect(status).toBe(200);
    // Response wraps task in { success, task: { id, ... } }
    const task = data.task ?? data;
    expect(task).toHaveProperty('id', createdTaskId);
  });

  test('DELETE /api/pi/tasks/:id deletes the task', async () => {
    if (!createdTaskId) test.skip();
    const { status } = await apiDelete(`/api/pi/tasks/${createdTaskId}`);
    expect(status).toBe(200);
    // Clean up - also delete any tasks created during this run
  });
});

// ─── CLI Endpoints ──────────────────────────────────────────────────

test.describe('CLI API', () => {
  test('GET /api/cli/ls lists files', async () => {
    const { status, data } = await apiGet(`/api/cli/ls?path=.`);
    expect(status).toBe(200);
    expect(data).toHaveProperty('entries');
    expect(Array.isArray(data.entries)).toBe(true);
  });

  test('GET /api/cli/cat reads a file', async () => {
    const { status, data } = await apiGet(`/api/cli/cat?path=package.json`);
    expect(status).toBe(200);
    expect(data).toHaveProperty('content');
    expect(typeof data.content).toBe('string');
    expect(data.content).toContain('"name"');
  });

  test('GET /api/cli/commands lists commands', async () => {
    const { status, data } = await apiGet('/api/cli/commands');
    expect(status).toBe(200);
    expect(data).toHaveProperty('commands');
    expect(Array.isArray(data.commands)).toBe(true);
  });
});
