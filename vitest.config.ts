import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    include: [
      "frontend/web/src/**/*.test.{ts,tsx}",
      "frontend/shared/src/**/*.test.{ts,tsx}",
    ],
    exclude: [
      "**/node_modules/**",
      "frontend/tui/**",  // TUI tests use bun:test — run with `bun test`
    ],
  },
});
