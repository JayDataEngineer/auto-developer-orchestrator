import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';
import path from 'path';

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, '.'),
    },
  },
  test: {
    globals: true,
    environment: 'jsdom',
    include: ['tests/react/**/*.test.{ts,tsx}'],
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
    setupFiles: ['./tests/react/setup.ts'],
    testTimeout: 10000,
  },
});
