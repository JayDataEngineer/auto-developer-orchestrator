import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    include: [
      "src/**/*.test.{ts,tsx}",
      "shared/src/__tests__/**/*.test.{ts,tsx}",
      "ts-tui-ink/**/*.test.{ts,tsx}",
    ],
    exclude: [
      "**/node_modules/**",
      // shared/ tests using bun:test — run with `bun test` instead
      "shared/src/format-tool-result.test.ts",
      "shared/src/relative-time.test.ts",
      "shared/src/tool-arg-preview.test.ts",
    ],
  },
});
