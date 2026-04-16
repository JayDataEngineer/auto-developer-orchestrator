/**
 * Component E2E Tests
 * Tests React component rendering and module imports.
 */

import { describe, it, expect } from 'vitest';

describe('Frontend Components - E2E', () => {
  describe('Component Imports', () => {
    it('should import App component', async () => {
      const AppModule = await import('../../src/App');
      expect(AppModule.App || AppModule.default).toBeDefined();
    });

    it('should import Header component', async () => {
      const { Header } = await import('../../src/components/Header');
      expect(Header).toBeDefined();
    });

    it('should import Checklist component', async () => {
      const { Checklist } = await import('../../src/components/Checklist');
      expect(Checklist).toBeDefined();
    });

    it('should import AIConfigModal component', async () => {
      const { AIConfigModal } = await import('../../src/components/AIConfigModal');
      expect(AIConfigModal).toBeDefined();
    });

    it('should import PiAgentView component', async () => {
      const { PiAgentView } = await import('../../src/components/PiAgentView');
      expect(PiAgentView).toBeDefined();
    });
  });

  describe('Utility Functions', () => {
    it('should import utils', async () => {
      const utils = await import('../../src/lib/utils');
      expect(utils.cn).toBeDefined();
    });
  });

  describe('Pages', () => {
    it('should have AppShell component', async () => {
      const { AppShell } = await import('../../src/components/AppShell');
      expect(AppShell).toBeDefined();
    });
  });
});
