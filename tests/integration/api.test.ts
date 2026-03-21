import { describe, it, expect, beforeAll } from 'vitest';
import fetch from 'node-fetch';

const BASE_URL = 'http://localhost:3847';

describe('Integration Tests - API', () => {
  beforeAll(async () => {
    // Ensure server is up
    await new Promise(resolve => setTimeout(resolve, 2000));
  });

  describe('GET /api/status', () => {
    it('should return 200 OK and valid status object', async () => {
      const response = await fetch(`${BASE_URL}/api/status?project=test`);
      const data = await response.json();
      expect(response.status).toBe(200);
      expect(data).toHaveProperty('gitState');
      expect(data).toHaveProperty('isAutoMode');
      expect(data).toHaveProperty('agentStatus');
    });
  });

  describe('GET /api/projects', () => {
    it('should return 200 OK and list of projects', async () => {
      const response = await fetch(`${BASE_URL}/api/projects`);
      const data = await response.json();
      expect(response.status).toBe(200);
      expect(data).toHaveProperty('projects');
      expect(Array.isArray((data as any).projects)).toBe(true);
    });
  });
});
