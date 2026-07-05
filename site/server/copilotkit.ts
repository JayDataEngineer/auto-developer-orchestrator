// CopilotKit runtime route — proxies /api/copilotkit requests to the
// harness's AG-UI endpoint via LangGraphHttpAgent.
//
// The harness runs AG-UI at http://127.0.0.1:9988/agui/<org_name>.
// This route handles the CopilotKit protocol ↔ AG-UI translation.

import type { IncomingMessage, ServerResponse } from "node:http";

const HARNESS_URL = process.env.PUX_HARNESS_URL ?? "http://127.0.0.1:9988";

export async function handleCopilotKitRoute(
  req: IncomingMessage,
  res: ServerResponse,
  urlPath: string,
): Promise<boolean> {
  // Only handle POST /api/copilotkit and POST /api/copilotkit/**
  const method = (req.method ?? "GET").toUpperCase();
  if (method !== "POST") return false;

  // Read the full request body
  const chunks: Buffer[] = [];
  for await (const c of req) chunks.push(c as Buffer);
  const body = Buffer.concat(chunks);

  // Parse to find which agent/graph is being targeted
  let targetOrg = "general";
  try {
    const parsed = JSON.parse(body.toString("utf-8"));
    // CopilotKit sends thread_id and run_id — extract org from metadata or default
    if (parsed.thread_id) {
      // Try to resolve org from thread metadata via the harness
      try {
        const threadResp = await fetch(
          `${HARNESS_URL}/threads/${parsed.thread_id}`,
        );
        if (threadResp.ok) {
          const thread = await threadResp.json() as { agent_id?: string };
          if (thread.agent_id) targetOrg = thread.agent_id;
        }
      } catch {
        // fall through to default
      }
    }
  } catch {
    // not JSON — forward as-is to default org
  }

  // Forward to the harness AG-UI endpoint
  const aguiUrl = `${HARNESS_URL}/agui/${targetOrg}`;
  try {
    const upstream = await fetch(aguiUrl, {
      method: "POST",
      headers: {
        "content-type": req.headers["content-type"] ?? "application/json",
        "accept": req.headers["accept"] ?? "*/*",
      },
      body,
    });

    // Stream the response back
    res.writeHead(upstream.status, {
      "content-type": upstream.headers.get("content-type") ?? "text/event-stream",
      "cache-control": "no-store",
      "x-accel-buffering": "no",
    });

    if (upstream.body) {
      const reader = upstream.body.getReader();
      const decoder = new TextDecoder();
      try {
        while (true) {
          const { done, value } = await reader.read();
          if (done) break;
          const chunk = typeof value === "string" ? value : decoder.decode(value, { stream: true });
          res.write(chunk);
        }
      } catch {
        // stream ended
      }
    }
    res.end();
    return true;
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    console.error(`[copilotkit] proxy to ${aguiUrl} failed:`, msg);
    res.writeHead(502, { "content-type": "application/json" });
    res.end(JSON.stringify({ error: `harness unavailable: ${msg}` }));
    return true;
  }
}
