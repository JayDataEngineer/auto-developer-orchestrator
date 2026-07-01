#!/usr/bin/env node
// pux launcher.
//
// Spawns the bundled pi runtime with our AGENTS.md, .mcp.json (sandbox MCP
// backend), and the pux extensions wired in. Mirrors little-coder's launcher
// pattern: pi is a plain npm dep, extensions live under .pi/extensions/, and
// pi-mcp-adapter bridges our Go MCP server's tools into pi's tool registry.
//
// Subcommands:
//   pux                       # interactive TUI against the cwd
//   pux --org _demo           # interactive TUI with a CTO overlay
//   pux dispatch --org X "…"  # one-shot headless dispatch (alias for -p)
//   pux history list          # list pi session files
//   pux history resume        # pi --resume (TUI session picker)
//   pux history continue      # pi --continue (most recent session)
//   pux setup                 # verify Docker image + build + start MCP server
//   pux teardown              # stop the MCP server
//   pux --help                # pi's own help

import { spawn, spawnSync } from "node:child_process";
import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import { homedir } from "node:os";
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

// ---- 2a. Prepend node_modules/.bin to PATH ----
// pi-subagents spawns child sessions via the bare command "pi" (resolved
// through PATH). Without this prepend, a globally-installed pi (e.g.
// @mariozechner/pi-coding-agent via Homebrew) wins and subagents inherit a
// different model registry than the parent — current models like
// deepseek-v4-flash go "not found" inside child sessions even though the
// parent sees them fine. node_modules/.bin/pi is the symlink npm writes to
// our bundled @earendil-works/pi-coding-agent, so putting it first keeps
// parent + child on the same version.
const localBin = join(pkgRoot, "node_modules", ".bin");
if (existsSync(localBin)) {
  process.env.PATH = `${localBin}:${process.env.PATH ?? ""}`;
}

// ---- 3. Subcommand intercept ----
// `dispatch` is a pure alias: rewrite argv to add `-p` then fall through to
// the normal pi spawn. The others are terminal — handle and exit.
const subcommand = process.argv[2];

if (subcommand === "dispatch") {
  // pux dispatch [...args] → pux -p [...args]
  process.argv = [process.argv[0], process.argv[1], "--print", ...process.argv.slice(3)];
} else if (subcommand === "history") {
  process.exit(runHistory(process.argv.slice(3)));
} else if (subcommand === "setup") {
  process.exit(runSetup(process.argv.slice(3)));
} else if (subcommand === "teardown") {
  process.exit(runTeardown(process.argv.slice(3)));
}

// ---- 4. Resolve the bundled pi CLI entry point ----
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

// ---- 5. Auto-discover bundled extensions under .pi/extensions/ ----
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

// ---- 6. Compose pi argv ----
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

// ---- 7. Suppress pi's version banner by default ----
// Pi is an internal dep here; users install `pux` and shouldn't see in-session
// nags about updating the underlying pi-coding-agent package.
if (process.env.PI_SKIP_VERSION_CHECK === undefined) {
  process.env.PI_SKIP_VERSION_CHECK = "1";
}

// ---- 8. Spawn pi in the user's cwd ----
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

// ---- Subcommand implementations ----

function runHistory(args) {
  // pi-mono stores sessions as JSONL at
  // ~/.pi/agent/sessions/<encoded-cwd>/<timestamp>_<uuid>.jsonl
  // The encoding makes the path ugly to grep, so we just list everything and
  // let the operator pipe to fzf / grep / etc.
  const sessionsDir = join(homedir(), ".pi", "agent", "sessions");
  const sub = args[0] ?? "list";

  if (sub === "list") {
    if (!existsSync(sessionsDir)) {
      console.log(`No sessions directory at ${sessionsDir}`);
      console.log(`(Run 'pux' to start your first session.)`);
      return 0;
    }
    // Walk one level deep — each subdir is an encoded project path.
    let projectDirs;
    try {
      projectDirs = readdirSync(sessionsDir).filter((n) => {
        try {
          return statSync(join(sessionsDir, n)).isDirectory();
        } catch {
          return false;
        }
      });
    } catch (err) {
      console.error(`pux history: cannot read ${sessionsDir}: ${err.message}`);
      return 1;
    }
    const rows = [];
    for (const proj of projectDirs) {
      const projDir = join(sessionsDir, proj);
      let files;
      try {
        files = readdirSync(projDir).filter((n) => n.endsWith(".jsonl"));
      } catch {
        continue;
      }
      for (const f of files) {
        const full = join(projDir, f);
        try {
          const st = statSync(full);
          // Strip the `.jsonl` and split timestamp+uuid for readability.
          rows.push({
            project: proj.replace(/^--|--$/g, "").replace(/--/g, "/"),
            session: f.replace(/\.jsonl$/, ""),
            mtime: st.mtime.toISOString(),
            size: st.size,
          });
        } catch {
          // skip unreadable
        }
      }
    }
    if (rows.length === 0) {
      console.log("No sessions found.");
      return 0;
    }
    rows.sort((a, b) => (a.mtime < b.mtime ? 1 : -1));
    console.log("Most recent sessions (newest first):");
    for (const r of rows) {
      console.log(`  ${r.mtime}  ${r.session}  ${r.project}`);
    }
    console.log(`\nResume:  pux --resume ${rows[0].session}`);
    console.log(`Continue: pux --continue`);
    return 0;
  }

  if (sub === "resume" || sub === "-r") {
    console.error(
      `pux history resume: use 'pux --resume' directly — pi opens an interactive session picker.`,
    );
    return 1;
  }

  if (sub === "continue" || sub === "-c") {
    console.error(`pux history continue: use 'pux --continue' directly.`);
    return 1;
  }

  console.error(
    `pux history: unknown subcommand '${sub}'.\n` +
      `Available: list (default), resume, continue.\n` +
      `For full session control, use pi flags directly: pux --resume, pux --continue, pux --session <id>, pux --fork <id>.`,
  );
  return 1;
}

function runSetup(_args) {
  // 1. Verify task binary is on PATH (canonical entry to Go server lifecycle).
  const taskOnPath = spawnSync("task", ["--version"], { stdio: "pipe" }).status === 0;
  if (!taskOnPath) {
    console.error(
      `pux setup: 'task' (Taskfile) not found on PATH.\n` +
        `Install: https://taskfile.dev/installation/ — e.g. 'sh -c "$(curl --location https://taskfile.dev/install.sh)"'`,
    );
    return 1;
  }

  // 2. Verify Docker is up.
  const dockerOk = spawnSync("docker", ["version", "--format", "{{.Server.Version}}"], {
    stdio: "pipe",
  }).status === 0;
  if (!dockerOk) {
    console.error(
      `pux setup: docker daemon not reachable. Start Docker Desktop or the dockerd service.`,
    );
    return 1;
  }

  // 3. Verify the pux-sandbox image exists.
  const imgCheck = spawnSync(
    "docker",
    ["image", "inspect", "pux-sandbox:latest", "--format", "{{.Id}}"],
    { stdio: "pipe" },
  );
  if (imgCheck.status !== 0) {
    console.error(
      `pux setup: 'pux-sandbox:latest' image not found.\n` +
        `Build it first: cd sandbox && docker build -t pux-sandbox:latest .`,
    );
    return 1;
  }

  // 4. Build + start the MCP server in background.
  console.log("Building MCP server binary...");
  const build = spawnSync("task", ["build"], { stdio: "inherit", cwd: pkgRoot });
  if (build.status !== 0) {
    console.error(`pux setup: task build failed (exit ${build.status}).`);
    return build.status ?? 1;
  }

  console.log("Starting MCP server in background...");
  const start = spawnSync("task", ["start"], { stdio: "inherit", cwd: pkgRoot });
  if (start.status !== 0) {
    console.error(`pux setup: task start failed (exit ${start.status}).`);
    return start.status ?? 1;
  }

  // 5. Print connection info.
  console.log("\nMCP server is up at http://127.0.0.1:9987");
  console.log("Connect from any MCP client using:");
  console.log('  {"mcpServers":{"pux-sandbox":{"url":"http://127.0.0.1:9987"}}}');
  console.log("\nOr drive it via the pux CLI:");
  console.log("  pux                       # interactive TUI");
  console.log("  pux --org _demo           # interactive TUI with CTO overlay");
  console.log("  pux dispatch --org _demo \"task\"  # one-shot headless");
  return 0;
}

function runTeardown(_args) {
  const res = spawnSync("task", ["stop"], { stdio: "inherit", cwd: pkgRoot });
  return res.status ?? 0;
}
