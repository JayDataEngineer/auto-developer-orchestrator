// Terminal BFF: WebSocket upgrade that spawns a host PTY at the workspace
// root and bidirectionally pipes bytes between the browser xterm.js and the
// kernel-assigned pseudo-terminal.
//
// Wire protocol (matches the legacy pux convention so we don't have to invent
// a new one):
//   - Browser→Server text frames: PTY stdin bytes (UTF-8).
//   - Browser→Server control frames (start with "\x01"): resize, e.g.
//       "\x01RESIZE:rows:cols"
//   - Server→Browser text/binary frames: PTY stdout+stderr (combined).

import { WebSocketServer, type WebSocket } from "ws";
import * as pty from "node-pty";
import { type Server } from "node:http";
import { existsSync, statSync } from "node:fs";
import { dirname, resolve as resolvePath } from "node:path";
import { fileURLToPath } from "node:url";
import { env } from "node:process";

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolvePath(here, "..", "..");
const workspaceRoot = resolvePath(env.PUX_SITE_WORKSPACE ?? repoRoot);

const SHELL = env.PUX_SITE_SHELL ?? env.SHELL ?? "/bin/bash";

// Sentinel bytes that prefix control frames. 0x01 = START OF HEADING — never
// appears in legitimate UTF-8 stdin from a human typing.
const CTRL = 0x01;

interface TermSession {
  pty: pty.IPty;
  ws: WebSocket;
}

const sessions = new Set<TermSession>();

function spawnPty(cwd: string, cols: number, rows: number): pty.IPty {
  // node-pty will throw if the shell binary is missing — let it propagate so
  // the WebSocket closes with a clear error.
  return pty.spawn(SHELL, ["-l"], {
    name: "xterm-256color",
    cols,
    rows,
    cwd,
    env: {
      ...process.env,
      TERM: "xterm-256color",
      // Belt-and-suspenders for shells that read these on startup.
      COLORTERM: "truecolor",
    },
  });
}

export function attachTerminalUpgrade(server: Server): void {
  const wss = new WebSocketServer({ noServer: true });

  server.on("upgrade", (req, socket, head) => {
    const url = new URL(req.url ?? "", "http://x");
    if (url.pathname !== "/api/terminal/ws") return;

    // Refuse if the workspace doesn't exist (we'd spawn in / otherwise).
    if (!existsSync(workspaceRoot) || !statSync(workspaceRoot).isDirectory()) {
      socket.write("HTTP/1.1 412 Precondition Failed\r\n\r\nworkspace gone\r\n");
      socket.destroy();
      return;
    }

    wss.handleUpgrade(req, socket, head, (ws) => {
      wss.emit("connection", ws, req);
    });
  });

  wss.on("connection", (ws, req) => {
    const url = new URL(req.url ?? "/api/terminal/ws", "http://x");
    const cols = clampInt(url.searchParams.get("cols"), 20, 400, 80);
    const rows = clampInt(url.searchParams.get("rows"), 1, 200, 24);

    let ptyHandle: pty.IPty;
    try {
      ptyHandle = spawnPty(workspaceRoot, cols, rows);
    } catch (err) {
      ws.send(
        JSON.stringify({
          type: "error",
          message: err instanceof Error ? err.message : String(err),
        }),
      );
      ws.close(1011, "pty spawn failed");
      return;
    }

    const session: TermSession = { pty: ptyHandle, ws };
    sessions.add(session);

    // PTY → WS
    const onData = (data: string) => {
      if (ws.readyState === ws.OPEN) ws.send(data);
    };
    const onExit = ({ exitCode }: { exitCode: number; signal?: number }) => {
      if (ws.readyState === ws.OPEN) {
        try {
          ws.send(`\r\n[process exited code ${exitCode}]\r\n`);
        } catch {
          // already closed
        }
        ws.close(1000, "pty exited");
      }
      sessions.delete(session);
    };
    ptyHandle.onData(onData);
    ptyHandle.onExit(onExit);

    // WS → PTY
    ws.on("message", (msg, isBinary) => {
      const buf = Buffer.isBuffer(msg) ? msg : Buffer.from(msg as ArrayBuffer);
      if (isBinary) {
        ptyHandle.write(buf.toString("utf-8"));
        return;
      }
      if (buf.length > 0 && buf[0] === CTRL) {
        const cmd = buf.subarray(1).toString("utf-8").trim();
        const m = /^RESIZE:(\d+):(\d+)$/.exec(cmd);
        if (m) {
          const c = clampInt(m[1], 20, 400, 80);
          const r = clampInt(m[2], 1, 200, 24);
          try {
            ptyHandle.resize(c, r);
          } catch {
            // pty may have just exited — ignore
          }
        }
        return;
      }
      ptyHandle.write(buf.toString("utf-8"));
    });

    ws.on("close", () => {
      try {
        ptyHandle.kill();
      } catch {
        // already dead
      }
      sessions.delete(session);
    });

    ws.on("error", () => {
      try {
        ptyHandle.kill();
      } catch {
        // already dead
      }
      sessions.delete(session);
    });
  });
}

function clampInt(v: string | null, min: number, max: number, dflt: number): number {
  if (v == null || v === "") return dflt;
  const n = Number.parseInt(v, 10);
  if (!Number.isFinite(n)) return dflt;
  return Math.max(min, Math.min(max, n));
}

