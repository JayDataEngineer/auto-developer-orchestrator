import { defineConfig } from 'vitest/config';

export default defineConfig({
  test: {
    globals: true,
    environment: 'node',
    include: ['tests/unit/**/*.test.ts', 'tests/integration/**/*.test.ts'],
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
    testTimeout: 10000,
  },
});
