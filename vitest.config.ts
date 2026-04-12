import { defineConfig } from 'vitest/config';

export default defineConfig({
  test: {
    globals: true,
    include: ['tests/unit/**/*.test.ts', 'tests/integration/**/*.test.ts', 'tests/react/**/*.test.tsx'],
    exclude: [
      '**/node_modules/**',
      '**/dist/**',
      '**/reference/**',
      '**/ref_docs/**',
      '**/projects/**',
      '**/docs/**',
      '**/openshell/**',
      '**/go-backend/**',
    ],
    environment: 'jsdom',
    testTimeout: 10000,
    setupFiles: ['tests/react/setup.ts'],
  },
});
