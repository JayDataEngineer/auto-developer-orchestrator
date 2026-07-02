// Sandbox lifecycle BFF: docker CLI wrapper for a single global sandbox per
// workspace.
//
// Routes (mounted under /api/sandbox):
//   GET    /api/sandbox                  → SandboxInfo | null
//   POST   /api/sandbox                  → SandboxInfo   { image?, resolution? }
//   DELETE /api/sandbox                  → 204
//   GET    /api/sandbox/screenshot       → image/png
//   POST   /api/sandbox/desktop-mode     → 204 (no-op; always desktop)
//
// Label contract (we adopt our own — distinct from the orgsandbox labels so
// we don't accidentally pick up compose-managed containers):
//   pux.site.workspace=<workspaceRoot>
//   pux.site.sandbox-id=default

import type { IncomingMessage, ServerResponse } from "node:http";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { dirname, resolve as resolvePath } from "node:path";
import { fileURLToPath } from "node:url";
import { env } from "node:process";

const execFileP = promisify(execFile);
const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolvePath(here, "..", "..");
const workspaceRoot = resolvePath(env.PUX_SITE_WORKSPACE ?? repoRoot);

const SANDBOX_ID = "default";
const LABEL_WORKSPACE = "pux.site.workspace";
const LABEL_SANDBOX_ID = "pux.site.sandbox-id";
const DEFAULT_IMAGE = env.PUX_SITE_SANDBOX_IMAGE ?? "pux-sandbox:latest";
const DEFAULT_RESOLUTION = env.PUX_SITE_SANDBOX_RESOLUTION ?? "1280x720x24";
// Host port override — defaults match the container's internal ports. Set
// PUX_SITE_VNC_PORT / PUX_SITE_CDP_PORT to e.g. 26080 / 29222 if 6080/9222
// are already taken on the host.
const HOST_VNC_PORT = Number(env.PUX_SITE_VNC_PORT ?? 6080);
const HOST_CDP_PORT = Number(env.PUX_SITE_CDP_PORT ?? 9222);

export interface SandboxInfo {
  id: string;
  containerId: string;
  image: string;
  status: string;
  running: boolean;
  createdAt: string;
  ports: { host: number; container: number; protocol: string }[];
  vncUrl: string | null;
}

interface DockerPsRow {
  ID: string;
  Image: string;
  Status: string;
  State: string; // "running", "created", "exited", etc.
  CreatedAt: string;
  Ports: string;
}

async function docker(args: string[]): Promise<string> {
  const { stdout } = await execFileP("docker", args, { maxBuffer: 64 * 1024 * 1024 });
  return stdout;
}

async function dockerJson<T>(args: string[]): Promise<T> {
  const out = await docker(args);
  return JSON.parse(out) as T;
}

function parsePorts(s: string): { host: number; container: number; protocol: string }[] {
  // e.g. "0.0.0.0:6080->6080/tcp, 0.0.0.0:9222->9222/tcp"
  if (!s) return [];
  const ports: { host: number; container: number; protocol: string }[] = [];
  for (const part of s.split(",")) {
    const m = /(\d+):(\d+)->(\d+)\/(\w+)/.exec(part);
    if (m) {
      ports.push({ host: Number(m[2]), container: Number(m[3]), protocol: m[4] });
    }
  }
  return ports;
}

async function findSandbox(): Promise<DockerPsRow | null> {
  // Use docker ps with --filter on our labels. JSON output for easy parsing.
  const args = [
    "ps",
    "-a",
    "--filter", `label=${LABEL_WORKSPACE}=${workspaceRoot}`,
    "--filter", `label=${LABEL_SANDBOX_ID}=${SANDBOX_ID}`,
    "--format", "{{json .}}",
  ];
  const out = await docker(args);
  const lines = out.split("\n").map(s => s.trim()).filter(Boolean);
  if (lines.length === 0) return null;
  // Pick the first; there should only be one.
  return JSON.parse(lines[0]) as DockerPsRow;
}

function toInfo(row: DockerPsRow): SandboxInfo {
  const ports = parsePorts(row.Ports);
  // Relative URL — same-origin through the BFF reverse proxy so remote
  // operators (SSH tunnel, tailscale) don't need direct access to the
  // container's published port. We always expose this when the sandbox
  // is running; the proxy 503s if the container is gone.
  return {
    id: SANDBOX_ID,
    containerId: row.ID,
    image: row.Image,
    status: row.Status,
    running: row.State === "running",
    createdAt: row.CreatedAt,
    ports,
    vncUrl: row.State === "running"
      ? `/api/sandbox/vnc/vnc.html?autoconnect=true&resize=remote&path=api/sandbox/vnc/websockify&reconnect=true`
      : null,
  };
}

async function createSandbox(opts: { image?: string; resolution?: string }): Promise<SandboxInfo> {
  const existing = await findSandbox();
  if (existing) return toInfo(existing);

  const image = opts.image ?? DEFAULT_IMAGE;
  const resolution = opts.resolution ?? DEFAULT_RESOLUTION;
  const args = [
    "run", "-d",
    "--label", `${LABEL_WORKSPACE}=${workspaceRoot}`,
    "--label", `${LABEL_SANDBOX_ID}=${SANDBOX_ID}`,
    "-e", `DISPLAY=:99`,
    "-e", `RESOLUTION=${resolution}`,
    "-e", `NOVNC_PORT=6080`,
    "-p", `${HOST_VNC_PORT}:6080`,
    "-p", `${HOST_CDP_PORT}:9222`,
    "-v", `${workspaceRoot}:/workspace:rw`,
    "--shm-size", "2g",
    "--restart", "unless-stopped",
    image,
  ];
  const containerId = (await docker(args)).trim();
  // Inspect to populate info.
  const inspect = await dockerJson<Array<{ State: { Running: boolean; Status: string }; Created: string; Config: { Image: string } }>>(["inspect", "--format", "{{json .}}", containerId]);
  const i = inspect[0];
  const row: DockerPsRow = {
    ID: containerId.slice(0, 12),
    Image: i.Config.Image,
    Status: i.State.Status,
    State: i.State.Running ? "running" : i.State.Status,
    CreatedAt: i.Created,
    Ports: "",
  };
  return toInfo(row);
}

async function deleteSandbox(): Promise<void> {
  const sb = await findSandbox();
  if (!sb) return;
  await docker(["rm", "-f", sb.ID]);
}

async function screenshot(): Promise<Buffer> {
  const sb = await findSandbox();
  if (!sb) throw new Error("no sandbox");
  if (sb.State !== "running") throw new Error("sandbox not running");
  // scrot writes a PNG to stdout via /dev/stdout. DISPLAY is explicit because
  // docker exec doesn't always inherit the env we set on `run`.
  const { stdout } = await execFileP("docker", [
    "exec", "-e", "DISPLAY=:99",
    sb.ID,
    "scrot", "-o", "/dev/stdout",
  ], { maxBuffer: 32 * 1024 * 1024, encoding: "buffer" });
  if (!stdout.length) throw new Error("screenshot empty");
  return stdout;
}

async function readJsonBody(req: IncomingMessage): Promise<unknown> {
  const chunks: Buffer[] = [];
  for await (const c of req) chunks.push(c as Buffer);
  if (chunks.length === 0) return {};
  return JSON.parse(Buffer.concat(chunks).toString("utf-8"));
}

function sendJson(res: ServerResponse, status: number, body: unknown): void {
  const payload = JSON.stringify(body);
  res.writeHead(status, {
    "content-type": "application/json; charset=utf-8",
    "cache-control": "no-store",
  });
  res.end(payload);
}

function sendNoContent(res: ServerResponse, status = 204): void {
  res.writeHead(status, { "content-length": "0" });
  res.end();
}

function sendError(res: ServerResponse, status: number, message: string): void {
  sendJson(res, status, { error: message });
}

export async function handleSandboxRoute(
  req: IncomingMessage,
  res: ServerResponse,
  urlPath: string,
): Promise<boolean> {
  const method = (req.method ?? "GET").toUpperCase();

  if (method === "GET" && urlPath === "/api/sandbox") {
    const sb = await findSandbox();
    sendJson(res, 200, sb ? toInfo(sb) : null);
    return true;
  }

  if (method === "POST" && urlPath === "/api/sandbox") {
    const body = (await readJsonBody(req)) as { image?: string; resolution?: string };
    try {
      const info = await createSandbox(body);
      sendJson(res, 201, info);
    } catch (err) {
      sendError(res, 500, err instanceof Error ? err.message : String(err));
    }
    return true;
  }

  if (method === "DELETE" && urlPath === "/api/sandbox") {
    try {
      await deleteSandbox();
      sendNoContent(res);
    } catch (err) {
      sendError(res, 500, err instanceof Error ? err.message : String(err));
    }
    return true;
  }

  if (method === "GET" && urlPath === "/api/sandbox/screenshot") {
    try {
      const png = await screenshot();
      res.writeHead(200, {
        "content-type": "image/png",
        "cache-control": "no-store",
        "content-length": String(png.length),
      });
      res.end(png);
    } catch (err) {
      sendError(res, 400, err instanceof Error ? err.message : String(err));
    }
    return true;
  }

  if (method === "POST" && urlPath === "/api/sandbox/desktop-mode") {
    // Always desktop in our image — no-op.
    sendNoContent(res);
    return true;
  }

  return false;
}
