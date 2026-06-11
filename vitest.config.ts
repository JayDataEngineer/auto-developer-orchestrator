import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    include: [
      "frontend/web/src/**/*.test.{ts,tsx}",
      "frontend/shared/src/__tests__/**/*.test.{ts,tsx}",
      "frontend/tui/**/*.test.{ts,tsx}",
    ],
    exclude: [
      "**/node_modules/**",
      // shared/ tests using bun:test — run with `bun test` instead
      "frontend/shared/src/format-tool-result.test.ts",
      "frontend/shared/src/relative-time.test.ts",
      "frontend/shared/src/tool-arg-preview.test.ts",
    ],
  },
});
