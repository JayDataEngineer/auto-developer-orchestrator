#!/usr/bin/env node
// pux launcher.
//
// Spawns the bundled pi runtime with our AGENTS.md, .mcp.json (sandbox MCP
// backend), and the pux extensions wired in. Mirrors little-coder's launcher
// pattern: pi is a plain npm dep, extensions live under .pi/extensions/, and
// pi-mcp-adapter bridges our Go MCP server's tools into pi's tool registry.
//
// Usage:
//   pux                       # interactive TUI against the cwd
//   pux --org _demo           # interactive TUI with a CTO overlay (Phase 3)
//   pux dispatch --org X "…"  # one-shot headless dispatch (Phase 4)
//   pux --help                # pi's own help

import { spawn } from "node:child_process";
import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

// ---- 1. Node version preflight (>= 22.19.0, matching pi-coding-agent) ----
const MIN_NODE = [22, 19, 0];
const cur = process.versions.node.split(".").map((n) => parseInt(n, 10));
const tooOld =
  cur[0] < MIN_NODE[0] ||
  (cur[0] === MIN_NODE[0] && cur[1] < MIN_NODE[1]) ||
  (cur[0] === MIN_NODE[0] && cur[1] === MIN_NODE[1] && cur[2] < MIN_NODE[2]);
if (tooOld) {
  console.error(
    `pux requires Node.js >= ${MIN_NODE.join(".")} (you have ${process.versions.node}).\n` +
      `Install a newer Node from https://nodejs.org or via nvm: 'nvm install 22'.`,
  );
  process.exit(1);
}

// ---- 2. Resolve package install root ----
const here = dirname(fileURLToPath(import.meta.url));
const pkgRoot = resolve(here, "..");

// ---- 3. Resolve the bundled pi CLI entry point ----
// Invoke pi's JS entry directly under the current Node binary (skips .bin/pi
// shim — works identically on Linux/macOS/Windows, no cmd.exe quoting traps).
// Try npm nested layout first, then bun/flat sibling layout.
const piPkgCandidates = [
  join(pkgRoot, "node_modules", "@earendil-works", "pi-coding-agent"),
  join(dirname(pkgRoot), "@earendil-works", "pi-coding-agent"),
];
let piEntry;
let piResolveErr;
for (const piPkgRoot of piPkgCandidates) {
  try {
    const piPkgJson = JSON.parse(readFileSync(join(piPkgRoot, "package.json"), "utf-8"));
    const binRel = typeof piPkgJson?.bin === "string" ? piPkgJson.bin : piPkgJson?.bin?.pi;
    if (typeof binRel !== "string") throw new Error("pi package.json has no bin.pi entry");
    const candidate = resolve(piPkgRoot, binRel);
    if (existsSync(candidate)) {
      piEntry = candidate;
      break;
    }
    piResolveErr = new Error(`resolved bin ${candidate} does not exist`);
  } catch (err) {
    piResolveErr = err;
  }
}
if (!piEntry) {
  console.error(
    `pux: cannot resolve the bundled pi cli. Looked in:\n` +
      piPkgCandidates.map((p) => `  - ${p}`).join("\n") +
      `\nUnderlying error: ${piResolveErr?.message ?? piResolveErr}\n` +
      `Try reinstalling: npm install`,
  );
  process.exit(1);
}

// ---- 4. Auto-discover bundled extensions under .pi/extensions/ ----
// Each subdir with an index.ts becomes a `--extension <path>` arg. Mirrors
// little-coder's pattern — keeps the bundled set explicit, ignores pi's own
// auto-discovery from cwd (--no-extensions below).
const extDir = join(pkgRoot, ".pi", "extensions");
const extArgs = [];
if (existsSync(extDir)) {
  for (const name of readdirSync(extDir).sort()) {
    const subdir = join(extDir, name);
    const idx = join(subdir, "index.ts");
    try {
      if (statSync(subdir).isDirectory() && existsSync(idx)) {
        extArgs.push("--extension", idx);
      }
    } catch {
      // skip unreadable entries
    }
  }
}

// pi-mcp-adapter is a regular npm dep — load it as an extension so its proxy
// tool is available without requiring the operator to run `pi install`. Path
// is stable per the pi-mcp-adapter package.json `pi.extensions` manifest.
const mcpAdapterEntry = join(pkgRoot, "node_modules", "pi-mcp-adapter", "index.ts");
if (existsSync(mcpAdapterEntry)) {
  extArgs.push("--extension", mcpAdapterEntry);
} else {
  console.error(
    `pux: pi-mcp-adapter not found at ${mcpAdapterEntry}\n` +
      `Run 'npm install' to bring down the dep.`,
  );
  process.exit(1);
}

// pi-subagents brings the `subagent` tool + agent-file discovery. Agents live
// under .pi/agents/*.md (project) and are delegated to via frontmatter-driven
// spawning. Path is stable per pi-subagents package.json `pi.extensions`.
const subagentsEntry = join(pkgRoot, "node_modules", "pi-subagents", "src", "extension", "index.ts");
if (existsSync(subagentsEntry)) {
  extArgs.push("--extension", subagentsEntry);
} else {
  console.error(
    `pux: pi-subagents not found at ${subagentsEntry}\n` +
      `Run 'npm install' to bring down the dep.`,
  );
  process.exit(1);
}

// ---- 5. Compose pi argv ----
// --no-context-files : ignore user's AGENTS.md so OURS wins
// --no-extensions    : skip pi's auto-discovery from cwd; explicit -e flags still load
// --system-prompt    : load <pkgRoot>/AGENTS.md regardless of cwd
const userArgs = process.argv.slice(2);
const agentsMd = join(pkgRoot, "AGENTS.md");

const piArgs = [
  "--no-context-files",
  "--no-extensions",
  ...(existsSync(agentsMd) ? ["--system-prompt", agentsMd] : []),
  ...extArgs,
  ...userArgs,
];

// ---- 6. Suppress pi's version banner by default ----
// Pi is an internal dep here; users install `pux` and shouldn't see in-session
// nags about updating the underlying pi-coding-agent package.
if (process.env.PI_SKIP_VERSION_CHECK === undefined) {
  process.env.PI_SKIP_VERSION_CHECK = "1";
}

// ---- 7. Spawn pi in the user's cwd ----
// process.execPath = same Node binary that passed our preflight. piEntry as
// argv element (not a shell string) avoids space-in-path / shell-injection
// classes on every platform.
const child = spawn(process.execPath, [piEntry, ...piArgs], {
  stdio: "inherit",
  cwd: process.cwd(),
  env: process.env,
});

const forward = (sig) => () => {
  try {
    child.kill(sig);
  } catch {
    // child already gone
  }
};
process.on("SIGINT", forward("SIGINT"));
process.on("SIGTERM", forward("SIGTERM"));
process.on("SIGHUP", forward("SIGHUP"));

child.on("error", (err) => {
  console.error("pux: failed to start pi:", err.message);
  process.exit(1);
});

child.on("exit", (code, signal) => {
  if (signal) {
    process.kill(process.pid, signal);
  } else {
    process.exit(code ?? 0);
  }
});
