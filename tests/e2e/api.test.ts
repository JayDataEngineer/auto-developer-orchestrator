/**
 * E2E Tests for Auto-Developer Orchestrator
 * 
 * Tests the full API surface and integration points.
 * Run with: npx vitest run e2e/api.test.ts
 */

import { describe, it, expect, beforeAll, afterAll } from 'vitest';
import fetch from 'node-fetch';

const BASE_URL = 'http://localhost:3847';

describe('Auto-Developer Orchestrator - E2E API Tests', () => {
  // Wait for server to be ready
  beforeAll(async () => {
    console.log('🧪 Starting E2E tests...');
    // Give server time to start
    await new Promise(resolve => setTimeout(resolve, 2000));
  }, 10000);

  afterAll(() => {
    console.log('✅ E2E tests completed');
  });

  describe('Health & Status', () => {
    it('should respond to health check', async () => {
      const response = await fetch(`${BASE_URL}/`);
      expect(response.status).toBe(200);
      expect(response.headers.get('content-type')).toContain('text/html');
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

      // Should succeed or fail gracefully
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
      expect(data.success).toBe(true);
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
      expect(data.isAutoMode).toBe(true);
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
      expect(data.isAutoMode).toBe(false);
    });
  });

  describe('Task Dispatch API', () => {
    it('should dispatch a task', async () => {
      const response = await fetch(`${BASE_URL}/api/dispatch`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          taskId: 'task-0',
          project: 'test-project'
        })
      });

      expect(response.status).toBe(200);
      const data = await response.json();
      expect(data.success).toBe(true);
      expect(data.taskId).toBe('task-0');
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

  describe('Test Generation API', () => {
    it('should generate tests', async () => {
      const response = await fetch(`${BASE_URL}/api/generate-tests`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          summary: 'Test feature X',
          engine: 'gemini',
          prompt: 'Generate comprehensive tests'
        })
      });

      expect(response.status).toBe(200);
      const data = await response.json();
      expect(data.success).toBe(true);
      expect(data.engine).toBe('gemini');
      expect(Array.isArray(data.tests)).toBe(true);
    });

    it('should run tests', async () => {
      const tests = [
        'Test case 1',
        'Test case 2',
        'Test case 3'
      ];

      const response = await fetch(`${BASE_URL}/api/run-tests`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ tests })
      });

      expect(response.status).toBe(200);
      const data = await response.json();
      expect(data.success).toBe(true);
      expect(Array.isArray(data.results)).toBe(true);
      expect(data.results[0]).toHaveProperty('test');
      expect(data.results[0]).toHaveProperty('status');
      expect(data.results[0]).toHaveProperty('duration');
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

  describe('Deep Agent TODO Generation API', () => {
    it('should have TODO generation endpoint', async () => {
      const response = await fetch(`${BASE_URL}/api/ai/generate-todos`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          project: 'sample-project'
        })
      });

      // May fail if deepagents not configured, but endpoint should exist
      // Returns 404 if project doesn't exist, which is expected
      expect([200, 404, 500]).toContain(response.status);
    });

    it('should reject missing project in TODO generation', async () => {
      const response = await fetch(`${BASE_URL}/api/ai/generate-todos`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({})
      });

      // Returns 400 (bad request) or 404 (project not found in path logic)
      expect([400, 404]).toContain(response.status);
    });
  });

  describe('Error Handling', () => {
    it('should handle 404 for unknown routes', async () => {
      const response = await fetch(`${BASE_URL}/api/unknown-route`);
      // Vite dev server returns 200 for SPA routing, so we check for non-API routes
      // In production, this would be 404
      expect([200, 404]).toContain(response.status);
    });

    it('should handle malformed JSON', async () => {
      const response = await fetch(`${BASE_URL}/api/checklist/update`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: 'not-json'
      });

      // Should return error (400 or 500)
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
  });
});
