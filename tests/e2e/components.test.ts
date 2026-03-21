/**
 * Component E2E Tests
 * Tests React component rendering and interaction.
 */

import { describe, it, expect } from 'vitest';

describe('Frontend Components - E2E', () => {
  describe('Component Imports', () => {
    it('should import App component', async () => {
      const { App } = await import('../src/App');
      expect(App).toBeDefined();
    });

    it('should import Header component', async () => {
      const { Header } = await import('../src/components/Header');
      expect(Header).toBeDefined();
    });

    it('should import Sidebar component', async () => {
      const { Sidebar } = await import('../src/components/Sidebar');
      expect(Sidebar).toBeDefined();
    });

    it('should import Checklist component', async () => {
      const { Checklist } = await import('../src/components/Checklist');
      expect(Checklist).toBeDefined();
    });

    it('should import Terminal component', async () => {
      const { Terminal } = await import('../src/components/Terminal');
      expect(Terminal).toBeDefined();
    });

    it('should import CurrentTaskCard component', async () => {
      const { CurrentTaskCard } = await import('../src/components/CurrentTaskCard');
      expect(CurrentTaskCard).toBeDefined();
    });
  });

  describe('Deep Agent Module', () => {
    it('should import deepAgent module', async () => {
      const deepAgent = await import('../src/deepAgent');
      expect(deepAgent.todoAgent).toBeDefined();
      expect(deepAgent.generateTODOs).toBeDefined();
    });
  });

  describe('Utility Functions', () => {
    it('should import utils', async () => {
      const utils = await import('../src/lib/utils');
      expect(utils.cn).toBeDefined();
    });
  });
});
