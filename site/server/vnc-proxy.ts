// noVNC reverse-proxy: serves the static assets from the running sandbox
// container's port 6080 so the browser can load same-origin from
// /api/sandbox/vnc/. Also bridges the WebSocket upgrade at
// /api/sandbox/vnc/websockify to the container's WS endpoint.
//
// Why same-origin: iframes pointing at 127.0.0.1:<port> work fine locally
// but break in remote-operator deployments (SSH tunnel, tailscale, etc.)
// where only the BFF port is exposed. Routing through the BFF means one
// hostname/one port gets the operator the whole UI.

import type { IncomingMessage, ServerResponse, Server } from "node:http";
import { request as httpRequest } from "node:http";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { WebSocketServer, WebSocket } from "ws";
import { dirname, resolve as resolvePath } from "node:path";
import { fileURLToPath } from "node:url";
import { env } from "node:process";

const execFileP = promisify(execFile);
const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolvePath(here, "..", "..");
const workspaceRoot = resolvePath(env.PUX_SITE_WORKSPACE ?? repoRoot);

const LABEL_WORKSPACE = "pux.site.workspace";
const LABEL_SANDBOX_ID = "pux.site.sandbox-id";
const SANDBOX_ID = "default";
const HOST_VNC_PORT = Number(env.PUX_SITE_VNC_PORT ?? 6080);

interface DockerPsRow {
  ID: string;
  State: string;
}

async function findSandboxContainer(): Promise<DockerPsRow | null> {
  const args = [
    "ps",
    "-a",
    "--filter", `label=${LABEL_WORKSPACE}=${workspaceRoot}`,
    "--filter", `label=${LABEL_SANDBOX_ID}=${SANDBOX_ID}`,
    "--format", "{{json .}}",
  ];
  const { stdout } = await execFileP("docker", args, { maxBuffer: 64 * 1024 * 1024 });
  const lines = stdout.split("\n").map(s => s.trim()).filter(Boolean);
  if (lines.length === 0) return null;
  return JSON.parse(lines[0]) as DockerPsRow;
}

// Prefix that the BFF serves noVNC under.
const VNC_HTTP_PREFIX = "/api/sandbox/vnc";
// Path on the container's HTTP server (websockify's --web dir is /usr/share/novnc).
// Container's port 6080 serves vnc.html at /vnc.html, /core/rfb.js, etc.
// We strip /api/sandbox/vnc and forward the rest to the container.
const VNC_WS_PATH = "/api/sandbox/vnc/websockify";

function forwardHttp(req: IncomingMessage, res: ServerResponse, containerPath: string): void {
  // Build headers minus the connection-specific ones.
  const headers: Record<string, string> = {};
  for (const [k, v] of Object.entries(req.headers)) {
    if (!v) continue;
    if (["host", "connection", "content-length", "transfer-encoding"].includes(k.toLowerCase())) continue;
    headers[k] = Array.isArray(v) ? v.join(", ") : v;
  }

  const upstream = httpRequest(
    {
      hostname: "127.0.0.1",
      port: HOST_VNC_PORT,
      path: containerPath,
      method: req.method ?? "GET",
      headers,
    },
    (up) => {
      res.writeHead(up.statusCode ?? 200, up.headers);
      up.pipe(res);
    },
  );
  upstream.on("error", (err) => {
    if (!res.headersSent) {
      res.writeHead(502, { "content-type": "text/plain" });
      res.end(`VNC upstream error: ${err.message}`);
    } else {
      try { res.end(); } catch {}
    }
  });
  req.pipe(upstream);
}

export async function handleVncHttpRoute(
  req: IncomingMessage,
  res: ServerResponse,
  urlPath: string,
): Promise<boolean> {
  if (!urlPath.startsWith(VNC_HTTP_PREFIX + "/") && urlPath !== VNC_HTTP_PREFIX) return false;

  // Refuse if there's no sandbox running.
  const sb = await findSandboxContainer();
  if (!sb || sb.State !== "running") {
    res.writeHead(503, { "content-type": "text/plain" });
    res.end("no sandbox running");
    return true;
  }

  // Strip our prefix; container's websockify serves everything from "/".
  let containerPath = urlPath.slice(VNC_HTTP_PREFIX.length);
  if (containerPath === "" || containerPath === "/") containerPath = "/vnc.html";
  if (!containerPath.startsWith("/")) containerPath = "/" + containerPath;

  // Append the query string if any.
  const qi = (req.url ?? "").indexOf("?");
  if (qi >= 0) containerPath += (req.url ?? "").slice(qi);

  forwardHttp(req, res, containerPath);
  return true;
}

// WS bridge:Browser ↔ BFF ↔ container:6080/websockify
//
// noVNC requests the `binary` Subprotocol. websockify handles both `binary`
// and `base64`. We forward whatever the client sent to the container so the
// negotiation is end-to-end.
function bridgeWs(clientWs: WebSocket, upstreamWs: WebSocket): void {
  const cleanup = () => {
    try { clientWs.close(); } catch {}
    try { upstreamWs.close(); } catch {}
  };

  clientWs.on("message", (data) => {
    if (upstreamWs.readyState === WebSocket.OPEN) upstreamWs.send(data);
  });
  upstreamWs.on("message", (data) => {
    if (clientWs.readyState === WebSocket.OPEN) clientWs.send(data);
  });
  clientWs.on("close", cleanup);
  clientWs.on("error", cleanup);
  upstreamWs.on("close", cleanup);
  upstreamWs.on("error", cleanup);
}

export function attachVncUpgrade(server: Server): void {
  const wss = new WebSocketServer({ noServer: true });

  server.on("upgrade", (req, socket, head) => {
    const urlPath = (req.url ?? "").split("?")[0];
    if (urlPath !== VNC_WS_PATH) return;

    // Sandbox must be running.
    findSandboxContainer()
      .then((sb) => {
        if (!sb || sb.State !== "running") {
          socket.write("HTTP/1.1 503 Service Unavailable\r\n\r\n");
          socket.destroy();
          return;
        }
        wss.handleUpgrade(req, socket, head, (clientWs) => {
          // Open the upstream WS to the container's websockify endpoint,
          // forwarding the subprotocols the browser requested.
          const subprotocols =
            (req.headers["sec-websocket-protocol"] ?? "")
              .toString()
              .split(",")
              .map(s => s.trim())
              .filter(Boolean);

          const upstream = new WebSocket(
            `ws://127.0.0.1:${HOST_VNC_PORT}/websockify`,
            subprotocols.length > 0 ? subprotocols : undefined,
          );

          upstream.on("open", () => {
            bridgeWs(clientWs as unknown as WebSocket, upstream);
          });
          upstream.on("error", () => {
            try { clientWs.close(); } catch {}
          });
          // If the browser gives up before upstream opens, drop upstream.
          clientWs.on("close", () => {
            try { upstream.close(); } catch {}
          });
        });
      })
      .catch(() => {
        try {
          socket.write("HTTP/1.1 500 Internal Server Error\r\n\r\n");
          socket.destroy();
        } catch {}
      });
  });
}
