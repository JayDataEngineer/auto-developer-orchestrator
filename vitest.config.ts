import { defineConfig } from "vitest/config";
import { resolve } from "path";

export default defineConfig({
  resolve: {
    alias: {
      "@": resolve(__dirname, "frontend/web/src"),
      "@pux/shared": resolve(__dirname, "frontend/shared/src/index.ts"),
    },
  },
  test: {
    include: [
      "frontend/web/src/**/*.test.{ts,tsx}",
      "frontend/shared/src/**/*.test.{ts,tsx}",
    ],
    exclude: [
      "**/node_modules/**",
      "frontend/tui/**",  // TUI tests use bun:test — run with `bun test`
    ],
    // Per-test override: tests needing a real DOM set `// @vitest-environment jsdom`
    // at the top of the file. Default stays node for the majority of unit tests.
    environment: "node",
    setupFiles: ["./vitest.setup.ts"],
  },
});
