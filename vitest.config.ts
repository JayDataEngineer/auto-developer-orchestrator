import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';
import path from 'path';

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, 'src'),
    },
  },
  test: {
    globals: true,
    include: ['tests/unit/**/*.test.ts', 'tests/react/**/*.test.{ts,tsx}'],
    exclude: [
      '**/node_modules/**',
      '**/dist/**',
      '**/reference/**',
      '**/ref_docs/**',
      '**/projects/**',
      '**/docs/**',
      '**/openshell/**',
      '**/go-backend/**',
      '**/tests/e2e/**',
    ],
    environment: 'jsdom',
    testTimeout: 10000,
    setupFiles: ['tests/react/setup.ts'],
  },
});
