/**
 * TUI test setup — runs before every test file in the tui project.
 *
 * DELIBERATELY MINIMAL: no vi.mock() calls here. vitest's hoist pass only
 * transforms the test file's own source — vi.mock in a setupFile does NOT
 * propagate into test files' module graphs. Each test file declares its own
 * vi.mock blocks at the top (small duplication, correct behavior).
 *
 * What does belong here: global hooks, fake timers config, vi.setConfig.
 */
import { afterEach, vi } from "vitest";

afterEach(() => {
  vi.restoreAllMocks();
});
