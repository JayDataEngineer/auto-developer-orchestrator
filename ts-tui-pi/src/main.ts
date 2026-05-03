#!/usr/bin/env bun
// pux-tui — pi-mono TUI powered by pux Go backend

import { TUI, ProcessTerminal, Text, Spacer } from "./tui/index.js";
import { parseArgs } from "node:util";

const { values: opts } = parseArgs({
  options: {
    server: { type: "string", default: "http://localhost:3847" },
    project: { type: "string", default: "default" },
  },
});

// Initialize TUI
const terminal = new ProcessTerminal();
const tui = new TUI(terminal, false);

// Build UI: simple vertical container
const header = new Text(`pux-tui  server: ${opts.server}  project: ${opts.project}`);
tui.addChild(header);
tui.addChild(new Spacer());
tui.addChild(new Text("SSE backend connected. Type to begin."));

tui.start();

// Handle input via process.stdin directly
process.stdin.on("data", (data: Buffer) => {
  const s = data.toString();
  if (s === "\x03" || s === "\x04") {
    tui.stop();
    process.exit(0);
  }
});

// Keep alive
setInterval(() => {}, 1000);
