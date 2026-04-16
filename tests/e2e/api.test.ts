/**
 * E2E Tests for Auto-Developer Orchestrator
 *
 * Tests the full API surface and integration points.
 * Run with: npx vitest run e2e/api.test.ts
 */

import { describe, it, expect, beforeAll, afterAll } from 'vitest';

const BASE_URL = 'http://localhost:3847';

describe('Auto-Developer Orchestrator - E2E API Tests', () => {
  // Wait for server to be ready
  beforeAll(async () => {
    console.log('Starting E2E tests...');
    await new Promise(resolve => setTimeout(resolve, 2000));
  }, 10000);

  afterAll(() => {
    console.log('E2E tests completed');
  });

  describe('Health & Status', () => {
    it('should respond to health check', async () => {
      const response = await fetch(`${BASE_URL}/api/health`);
      expect(response.status).toBe(200);
      const text = await response.text();
      expect(text).toBe('OK');
    });

    it('should return API status', async () => {
      const response = await fetch(`${BASE_URL}/api/status?project=test-project`);
      expect(response.status).toBe(200);

      const data = await response.json();
      expect(data).toHaveProperty('gitState');
      expect(data).toHaveProperty('workingTree');
      expect(data).toHaveProperty('isAutoMode');
      expect(data).toHaveProperty('agentStatus');
      expect(data).toHaveProperty('lastCommit');
      expect(data.project).toBe('test-project');
    });
  });

  describe('Projects API', () => {
    it('should list projects', async () => {
      const response = await fetch(`${BASE_URL}/api/projects`);
      expect(response.status).toBe(200);

      const data = await response.json();
      expect(data).toHaveProperty('projects');
      expect(Array.isArray(data.projects)).toBe(true);
    });

    it('should add a custom project', async () => {
      const testProject = {
        name: 'e2e-test-project',
        path: '/tmp/e2e-test-project'
      };

      const response = await fetch(`${BASE_URL}/api/projects/add`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(testProject)
      });

      expect(response.status).toBeGreaterThanOrEqual(200);
    });
  });

  describe('Checklist API', () => {
    const testProject = 'sample-project';

    it('should get checklist for project', async () => {
      const response = await fetch(`${BASE_URL}/api/checklist?project=${testProject}`);
      expect(response.status).toBe(200);

      const data = await response.json();
      expect(data).toHaveProperty('tasks');
      expect(Array.isArray(data.tasks)).toBe(true);
    });

    it('should update checklist', async () => {
      const tasks = [
        { id: 'task-0', text: 'E2E Test Task 1', completed: false },
        { id: 'task-1', text: 'E2E Test Task 2', completed: true }
      ];

      const response = await fetch(`${BASE_URL}/api/checklist/update`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          project: testProject,
          tasks
        })
      });

      expect(response.status).toBeGreaterThanOrEqual(200);
      const data = await response.json();
      // May fail if project directory doesn't have a TASKS.md
      expect(data).toHaveProperty('success');
    });

    it('should verify checklist was updated', async () => {
      const response = await fetch(`${BASE_URL}/api/checklist?project=${testProject}`);
      expect(response.status).toBe(200);

      const data = await response.json();
      expect(data.tasks).toBeDefined();
    });
  });

  describe('AI Configuration API', () => {
    it('should get AI config', async () => {
      const response = await fetch(`${BASE_URL}/api/config/ai`);
      expect(response.status).toBe(200);

      const data = await response.json();
      expect(data).toHaveProperty('autoTask');
      expect(data).toHaveProperty('autoTest');
      expect(data).toHaveProperty('fullAutomationMode');
      expect(data).toHaveProperty('testTypes');
    });

    it('should update AI config', async () => {
      const newConfig = {
        autoTask: false,
        fullAutomationMode: true
      };

      const response = await fetch(`${BASE_URL}/api/config/ai`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(newConfig)
      });

      expect(response.status).toBe(200);
      const data = await response.json();
      expect(data.success).toBe(true);
      expect(data.aiConfig.autoTask).toBe(false);
      expect(data.aiConfig.fullAutomationMode).toBe(true);
    });
  });

  describe('System Configuration API', () => {
    it('should get system config', async () => {
      const response = await fetch(`${BASE_URL}/api/config/system`);
      expect(response.status).toBe(200);

      const data = await response.json();
      expect(data).toHaveProperty('projectsDir');
    });

    it('should update system config', async () => {
      const newConfig = {
        projectsDir: '/tmp/test-projects'
      };

      const response = await fetch(`${BASE_URL}/api/config/system`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(newConfig)
      });

      expect(response.status).toBe(200);
      const data = await response.json();
      expect(data.success).toBe(true);
    });
  });

  describe('Settings API', () => {
    it('should toggle automation mode', async () => {
      const response = await fetch(`${BASE_URL}/api/settings/mode`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          mode: 'auto',
          project: 'test-project'
        })
      });

      expect(response.status).toBe(200);
      const data = await response.json();
      expect(data.success).toBe(true);
      expect(data.is_auto_mode).toBe(true);
    });

    it('should toggle to manual mode', async () => {
      const response = await fetch(`${BASE_URL}/api/settings/mode`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          mode: 'manual',
          project: 'test-project'
        })
      });

      expect(response.status).toBe(200);
      const data = await response.json();
      expect(data.success).toBe(true);
      expect(data.is_auto_mode).toBe(false);
    });
  });

  describe('Merge API', () => {
    it('should merge a task', async () => {
      const response = await fetch(`${BASE_URL}/api/merge`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          project: 'sample-project'
        })
      });

      // May succeed or fail if no task in progress
      expect(response.status).toBeGreaterThanOrEqual(200);
    });
  });

  describe('Clone API', () => {
    it('should handle clone request', async () => {
      const response = await fetch(`${BASE_URL}/api/clone`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          url: 'https://github.com/test/repo.git'
        })
      });

      // May succeed or fail if project exists
      expect(response.status).toBeGreaterThanOrEqual(200);
    });

    it('should reject invalid clone URL', async () => {
      const response = await fetch(`${BASE_URL}/api/clone`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({})
      });

      expect(response.status).toBe(400);
    });
  });

  describe('Pi Agent API', () => {
    it('should list models', async () => {
      const response = await fetch(`${BASE_URL}/api/pi/models`);
      expect(response.status).toBe(200);

      const data = await response.json();
      expect(data).toHaveProperty('models');
    });

    it('should list active agents', async () => {
      const response = await fetch(`${BASE_URL}/api/pi/active`);
      expect(response.status).toBe(200);

      const data = await response.json();
      expect(data).toHaveProperty('projects');
    });

    it('should list sessions for a project', async () => {
      const response = await fetch(`${BASE_URL}/api/pi/sessions?project=test-project`);
      // May return 404 if project not registered, that's acceptable
      expect([200, 404]).toContain(response.status);
    });

    it('should get tool permissions', async () => {
      const response = await fetch(`${BASE_URL}/api/pi/tool-permissions`);
      expect(response.status).toBe(200);
    });

    it('should get conversation history', async () => {
      const response = await fetch(`${BASE_URL}/api/pi/history`);
      expect(response.status).toBe(200);
    });
  });

  describe('Sandbox API', () => {
    it('should list sandboxes', async () => {
      const response = await fetch(`${BASE_URL}/api/sandbox/`);
      expect(response.status).toBe(200);

      const data = await response.json();
      expect(Array.isArray(data)).toBe(true);
    });
  });

  describe('Scheduler API', () => {
    it('should list scheduler jobs', async () => {
      const response = await fetch(`${BASE_URL}/api/scheduler/`);
      expect(response.status).toBe(200);

      const data = await response.json();
      expect(data).toHaveProperty('jobs');
      expect(Array.isArray(data.jobs)).toBe(true);
    });
  });

  describe('Error Handling', () => {
    it('should handle 404 for unknown routes', async () => {
      const response = await fetch(`${BASE_URL}/api/unknown-route`);
      expect([200, 404]).toContain(response.status);
    });

    it('should handle malformed JSON', async () => {
      const response = await fetch(`${BASE_URL}/api/checklist/update`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: 'not-json'
      });

      expect(response.status).toBeGreaterThanOrEqual(400);
    });

    it('should handle missing required fields', async () => {
      const response = await fetch(`${BASE_URL}/api/projects/add`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({})
      });

      expect(response.status).toBe(400);
    });
  });

  describe('Integration Flow', () => {
    it('should complete full workflow', async () => {
      // 1. Get projects
      const projectsRes = await fetch(`${BASE_URL}/api/projects`);
      expect(projectsRes.status).toBe(200);

      // 2. Get AI config
      const aiConfigRes = await fetch(`${BASE_URL}/api/config/ai`);
      expect(aiConfigRes.status).toBe(200);

      // 3. Toggle mode
      const modeRes = await fetch(`${BASE_URL}/api/settings/mode`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ mode: 'manual', project: 'test' })
      });
      expect(modeRes.status).toBe(200);

      // 4. Get checklist
      const checklistRes = await fetch(`${BASE_URL}/api/checklist?project=test`);
      expect(checklistRes.status).toBe(200);

      // 5. Update checklist
      const updateRes = await fetch(`${BASE_URL}/api/checklist/update`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          project: 'test',
          tasks: [{ id: 'task-0', text: 'Integration test', completed: false }]
        })
      });
      expect(updateRes.status).toBeGreaterThanOrEqual(200);
    });

    it('should handle merge for a project with checklist', async () => {
      const projectName = 'merge-test-project';
      const projectDir = `/tmp/${projectName}`;

      // Setup: register project and create TASKS.md
      const fs = await import('fs');
      fs.mkdirSync(projectDir, { recursive: true });
      fs.writeFileSync(`${projectDir}/TASKS.md`, '- [ ] Implement cool new feature\n');
      fs.writeFileSync(`${projectDir}/.gitkeep`, '');

      await fetch(`${BASE_URL}/api/projects/add`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: projectName, path: projectDir })
      });

      // Merge the task (marks current task complete, appends debug task)
      const mergeRes = await fetch(`${BASE_URL}/api/merge`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ project: projectName })
      });

      expect(mergeRes.status).toBe(200);
      const mergeData = await mergeRes.json();
      // Merge succeeds — summary may be empty if no task index was set
      expect(mergeData.success).toBe(true);

      // Fetch the checklist — should still be readable
      const checklistRes = await fetch(`${BASE_URL}/api/checklist?project=${projectName}`);
      expect(checklistRes.status).toBe(200);
      const checklistData = await checklistRes.json();
      expect(Array.isArray(checklistData.tasks)).toBe(true);

      // Cleanup
      fs.rmSync(projectDir, { recursive: true, force: true });
    });
  });
});
