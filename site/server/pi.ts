// Process-singleton PiClient. Drives Pi's AgentSession SDK in-process behind
// the supervisor that @assistant-ui/react-pi/node owns.
//
// workspacePath: where the agent runs shell commands + reads/writes files.
//               Defaults to the repo root (this project's parent), but each
//               thread creation can pass its own workspacePath.
//
// We do NOT seed a model — Pi resolves it the same way `pi` does on the CLI:
//   1. PI_PROVIDER + PI_MODEL_ID env vars
//   2. ~/.pi/agent/settings.json's defaultProvider + defaultModel
//   3. (none) → readiness reports "missing-model" and the UI prompts
//
// That means: if you've already run `pi` once and picked a default model, this
// site just works. No additional configuration.

import { createPiNodeClient } from "@assistant-ui/react-pi/node";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
// site/ → repo root
const repoRoot = resolve(here, "..", "..");

export const piClient = createPiNodeClient({
  workspacePath: process.env.PUX_SITE_WORKSPACE ?? repoRoot,
});
