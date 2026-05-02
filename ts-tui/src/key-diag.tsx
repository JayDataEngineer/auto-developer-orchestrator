#!/usr/bin/env bun
/**
 * Shift+Enter diagnostic: run this, press Shift+Enter, then Ctrl+C.
 * Shows raw bytes, env vars, and whether any sequence matches.
 *
 *   bun run src/key-diag.tsx
 */
import { readFileSync } from "node:fs";

const info = {
  term: process.env.TERM ?? "(unset)",
  termProgram: process.env.TERM_PROGRAM ?? "(unset)",
  kittyWindowId: process.env.KITTY_WINDOW_ID ?? "(unset)",
  colorterm: process.env.COLORTERM ?? "(unset)",
  terminalEmulator: process.env.TERMINAL_EMULATOR ?? "(unset)",
  weztermPane: process.env.WEZTERM_PANE ?? "(unset)",
};

console.log("=== Terminal Environment ===");
for (const [k, v] of Object.entries(info)) {
  console.log(`  ${k}: ${v}`);
}

// Check if Kitty protocol is likely active
const isKitty = (info.term || "").includes("kitty") ||
                (info.termProgram || "").toLowerCase() === "ghostty" ||
                info.kittyWindowId !== "(unset)" ||
                (info.terminalEmulator || "").toLowerCase().includes("kitty") ||
                info.weztermPane !== "(unset)";

console.log(`\nKitty protocol likely: ${isKitty}`);

// Read /proc/self/attr/current to check SELinux context (irrelevant but useful)
try {
  const tty = readFileSync("/proc/self/stat", "utf-8").split(" ")[6];
  console.log(`  TTY: ${tty}`);
} catch {}

// List any env vars related to terminal
const termVars = Object.entries(process.env as Record<string, string>)
  .filter(([k]) => /term|tty|shell|kitty|ghost|wez|alac/i.test(k));
if (termVars.length) {
  console.log("\n  All terminal-related env vars:");
  for (const [k, v] of termVars) {
    console.log(`    ${k}=${v}`);
  }
}

console.log("\n=== Press Shift+Enter now! ===");
console.log("  (Ctrl+C to exit)");
console.log("");

// Stdin event handler — captures raw bytes
const { stdin } = process;

const shiftEnterTests: [string, string][] = [
  ["\x1b[13;2u", "CSI-u Shift+Enter"],
  ["\x1b[57414;2u", "CSI-u KP Shift+Enter"],
  ["\x1b[13;3u", "CSI-u Alt+Enter"],
  ["\x1b[13;4u", "CSI-u Shift+Alt+Enter"],
  ["\x1b[13;5u", "CSI-u Ctrl+Enter"],
  ["\x1b[13;6u", "CSI-u Ctrl+Shift+Enter"],
  ["\x1b[27;2;13~", "xterm modOtherKeys Shift+Enter"],
  ["\x1b[13;2~", "xterm format2 Shift+Enter"],
  ["\x1b\r", "Kitty/Ghostty custom Shift+Enter"],
  ["\x1bOM", "SS3 M (numpad Enter)"],
  ["\r", "CR (plain Enter)"],
  ["\n", "LF (plain Enter / Shift+Enter)"],
];

function checkMatches(s: string) {
  const matched: string[] = [];
  // CSI-u regex from app.tsx
  const m = s.match(/^\x1b\[(13|57414);([\d]+)(u|~)/);
  if (m && m[2]) {
    const mod = parseInt(m[2], 10);
    const isShift = ((mod - 1) & 1) === 1;
    matched.push(`CSI-u match: mod=${mod}, shift=${isShift}`);
  }
  // xterm 3-number format
  const m3 = s.match(/^\x1b\[27;(\d+);(\d+)~/);
  if (m3 && m3[1] && m3[2]) {
    const mod = parseInt(m3[1], 10);
    const code = parseInt(m3[2], 10);
    const isShift = ((mod - 1) & 1) === 1;
    matched.push(`xterm-3 match: mod=${mod}, code=${code}, shift=${isShift}`);
  }
  // xterm 2-number format
  const m2 = s.match(/^\x1b\[(\d+);(\d+)~/);
  if (m2 && m2[1] && m2[2]) {
    const code = parseInt(m2[1], 10);
    const mod = parseInt(m2[2], 10);
    const isShift = ((mod - 1) & 1) === 1;
    matched.push(`xterm-2 match: code=${code}, mod=${mod}, shift=${isShift}`);
  }
  for (const [seq, label] of shiftEnterTests) {
    if (s === seq) {
      matched.push(`exact match: "${label}"`);
    }
  }
  return matched;
}

let keyCount = 0;
stdin.setRawMode(true);
stdin.resume();

stdin.on("data", (data: Buffer) => {
  keyCount++;
  const bytes = Array.from(data).map(b => "0x" + b.toString(16).padStart(2, "0")).join(" ");
  const json = JSON.stringify(data.toString());
  const len = data.length;

  console.log(`\n--- Key #${keyCount} (len=${len}) ---`);
  console.log(`  hex:  ${bytes}`);
  console.log(`  json: ${json}`);

  const matches = checkMatches(data.toString());
  if (matches.length > 0) {
    console.log("  matches:");
    for (const m of matches) console.log(`    ✓ ${m}`);
  } else {
    console.log("  matches: (none)");
  }

  // If this was \r or \n, we also show what would happen
  const s = data.toString();
  if (s === "\r") console.log("  → Ink useInput: key.return = true");
  if (s === "\n") {
    if (isKitty) console.log("  → Raw handler: Shift+Enter (newline)");
    else console.log("  → Ink useInput: key.return = true (probably)");
  }
});

console.log("\nListening for keypresses...\n");
