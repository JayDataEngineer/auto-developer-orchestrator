import { defineConfig } from "vitest/config";
import { resolve } from "path";

const alias = {
  "@": resolve(__dirname, "frontend/web/src"),
  "@pux/shared": resolve(__dirname, "frontend/shared/src/index.ts"),
};

const commonExclude = [
  "**/node_modules/**",
  "tests/**",                  // Playwright E2E
  "reference/**",              // External reference code
  "**/*.spec.{ts,tsx,js}",     // Playwright specs
];

export default defineConfig({
  resolve: { alias },
  test: {
    exclude: commonExclude,
    projects: [
      {
        resolve: { alias },
        test: {
          name: "shared",
          include: ["frontend/shared/src/**/*.test.{ts,tsx}"],
          environment: "node",
          setupFiles: ["./vitest.setup.ts"],
        },
      },
      {
        resolve: { alias },
        test: {
          name: "web",
          include: ["frontend/web/src/**/*.test.{ts,tsx}"],
          environment: "node",
          setupFiles: ["./vitest.setup.ts"],
        },
      },
      {
        resolve: { alias },
        test: {
          name: "tui",
          include: ["frontend/tui/tests/**/*.test.{ts,tsx}"],
          environment: "node",
          setupFiles: ["./frontend/tui/vitest.setup.ts"],
        },
      },
    ],
  },
});
