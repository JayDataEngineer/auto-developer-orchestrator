#!/usr/bin/env bun
import { render, Text } from "ink";
import React from "react";
import { parseArgs } from "node:util";

const { values } = parseArgs({
  args: process.argv.slice(2),
  options: {
    server: { type: "string", default: "http://localhost:3847" },
    project: { type: "string", default: "." },
    agent: { type: "string", default: "default" },
    help: { type: "boolean", default: false },
  },
});

if (values.help) {
  process.stdout.write(
    `orch-tui — TypeScript TUI for the Orchestrator

Usage: bun run src/index.tsx [options]

Options:
  --server <url>   Backend server URL (default: http://localhost:3847)
  --project <name> Project name (default: .)
  --agent <id>     Agent ID (default: default)
  --help           Show this help

Keyboard shortcuts (in chat):
  Enter           Send message (add trailing \\ for newline)
  Shift+Enter     Insert newline
  Ctrl+T          Toggle thinking display
  Ctrl+H          Toggle help overlay
  Ctrl+J          Scheduler mode
  Ctrl+C          Quit (stop streaming if active)
`
  );
  process.exit(0);
}

import App from "./app";

const { waitUntilExit } = render(
  React.createElement(App, {
    serverUrl: values.server!,
    project: values.project!,
    agentId: values.agent!,
  })
);

waitUntilExit();
