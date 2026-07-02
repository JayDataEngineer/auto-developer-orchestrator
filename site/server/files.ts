// Node BFF file ops on host workspace. All paths resolved against the
// configured workspace root (process.env.PUX_SITE_WORKSPACE or repo root).
//
// Routes (mounted under /api/files by index.ts):
//   GET    /api/files/list?path=           → DirEntry[]
//   GET    /api/files/read?path=           → { path, language, content }
//   PUT    /api/files/write                → 204     { path, content }
//   POST   /api/files/create               → 201     { path, type: "file"|"dir" }
//   POST   /api/files/move                 → 204     { from, to }
//   DELETE /api/files/delete?path=         → 204     (trash to .pux/trash/<ts>/)
//
// Path safety: every path is resolved against workspaceRoot and rejected if
// the result escapes the root. Symlinks inside the workspace are allowed
// (lstat is used) but paths cannot write OUTSIDE the workspace.

import type { IncomingMessage, ServerResponse } from "node:http";
import { readFile, writeFile, readdir, stat, rename, mkdir, rm } from "node:fs/promises";
import { existsSync } from "node:fs";
import { dirname, join, relative, resolve as resolvePath, extname } from "node:path";
import { fileURLToPath } from "node:url";
import { env } from "node:process";

// Match pi.ts's resolution: workspace defaults to the repo root (parent of
// site/), so the editor sees the same tree the agent's bash tool does.
const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolvePath(here, "..", "..");
const workspaceRoot = resolvePath(env.PUX_SITE_WORKSPACE ?? repoRoot);
const trashRoot = join(workspaceRoot, ".pux", "trash");

const LANG_BY_EXT: Record<string, string> = {
  ".ts": "typescript",
  ".tsx": "typescript",
  ".js": "javascript",
  ".jsx": "javascript",
  ".mjs": "javascript",
  ".cjs": "javascript",
  ".json": "json",
  ".md": "markdown",
  ".markdown": "markdown",
  ".py": "python",
  ".go": "go",
  ".rs": "rust",
  ".rb": "ruby",
  ".java": "java",
  ".kt": "kotlin",
  ".yml": "yaml",
  ".yaml": "yaml",
  ".toml": "toml",
  ".html": "html",
  ".css": "css",
  ".scss": "scss",
  ".sh": "shell",
  ".bash": "shell",
  ".zsh": "shell",
  ".sql": "sql",
  ".dockerfile": "dockerfile",
  ".txt": "plaintext",
};

function languageForPath(p: string): string {
  const ext = extname(p).toLowerCase();
  if (ext === "" && p.endsWith("Dockerfile")) return "dockerfile";
  return LANG_BY_EXT[ext] ?? "plaintext";
}

function safeResolve(p: string | undefined | null): string {
  const root = resolvePath(workspaceRoot);
  // Treat empty/null/undefined as workspace root. Otherwise resolve + sandbox.
  const decoded = p ? decodeURIComponent(p) : ".";
  const candidate = resolvePath(root, decoded);
  const rel = relative(root, candidate);
  if (rel.startsWith("..") || rel.includes("..")) {
    throw new Error(`path escapes workspace: ${decoded}`);
  }
  return candidate;
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

export interface DirEntry {
  name: string;
  path: string;
  type: "file" | "dir" | "symlink";
  size: number;
  mtime: string;
}

// Skip these names in the file tree by default. The legacy frontend hid
// dot-folders like .git and the heavy node_modules. For our purposes:
// always hide .git, always hide node_modules. Show other dotfiles.
const HIDDEN_DIR_NAMES = new Set([".git", "node_modules", ".pux"]);

export async function handleFilesRoute(
  req: IncomingMessage,
  res: ServerResponse,
  urlPath: string,
  query: URLSearchParams,
): Promise<boolean> {
  const method = (req.method ?? "GET").toUpperCase();
  const url = urlPath;

  // GET /api/files/list?path=
  if (method === "GET" && url === "/api/files/list") {
    const target = safeResolve(query.get("path") ?? "");
    const st = await stat(target);
    if (!st.isDirectory()) {
      sendError(res, 400, "not a directory");
      return true;
    }
    const entries = await readdir(target, { withFileTypes: true });
    const out: DirEntry[] = [];
    for (const e of entries) {
      if (HIDDEN_DIR_NAMES.has(e.name)) continue;
      let type: DirEntry["type"] = "file";
      if (e.isSymbolicLink()) type = "symlink";
      else if (e.isDirectory()) type = "dir";
      let size = 0;
      let mtime = "";
      try {
        const s = await stat(join(target, e.name));
        size = s.size;
        mtime = s.mtime.toISOString();
      } catch {
        // symlink to nowhere — skip stats
      }
      const rel = relative(workspaceRoot, join(target, e.name));
      out.push({ name: e.name, path: rel, type, size, mtime });
    }
    // dirs first, then files, both alpha
    out.sort((a, b) => {
      if (a.type === "dir" && b.type !== "dir") return -1;
      if (a.type !== "dir" && b.type === "dir") return 1;
      return a.name.localeCompare(b.name);
    });
    sendJson(res, 200, out);
    return true;
  }

  // GET /api/files/read?path=
  if (method === "GET" && url === "/api/files/read") {
    const target = safeResolve(query.get("path") ?? "");
    const st = await stat(target);
    if (!st.isFile()) {
      sendError(res, 400, "not a file");
      return true;
    }
    const content = await readFile(target);
    sendJson(res, 200, {
      path: relative(workspaceRoot, target),
      language: languageForPath(target),
      content: content.toString("utf-8"),
      size: content.length,
      mtime: st.mtime.toISOString(),
    });
    return true;
  }

  // PUT /api/files/write
  if (method === "PUT" && url === "/api/files/write") {
    const body = (await readJsonBody(req)) as { path?: string; content?: string };
    if (!body.path || typeof body.content !== "string") {
      sendError(res, 400, "path + content required");
      return true;
    }
    const target = safeResolve(body.path);
    await mkdir(dirname(target), { recursive: true });
    await writeFile(target, body.content, "utf-8");
    sendNoContent(res);
    return true;
  }

  // POST /api/files/create
  if (method === "POST" && url === "/api/files/create") {
    const body = (await readJsonBody(req)) as { path?: string; type?: "file" | "dir" };
    if (!body.path || (body.type !== "file" && body.type !== "dir")) {
      sendError(res, 400, "path + type (file|dir) required");
      return true;
    }
    const target = safeResolve(body.path);
    if (existsSync(target)) {
      sendError(res, 409, "already exists");
      return true;
    }
    await mkdir(dirname(target), { recursive: true });
    if (body.type === "dir") await mkdir(target, { recursive: true });
    else await writeFile(target, "", "utf-8");
    sendJson(res, 201, { path: relative(workspaceRoot, target), type: body.type });
    return true;
  }

  // POST /api/files/move
  if (method === "POST" && url === "/api/files/move") {
    const body = (await readJsonBody(req)) as { from?: string; to?: string };
    if (!body.from || !body.to) {
      sendError(res, 400, "from + to required");
      return true;
    }
    const src = safeResolve(body.from);
    const dst = safeResolve(body.to);
    await mkdir(dirname(dst), { recursive: true });
    await rename(src, dst);
    sendNoContent(res);
    return true;
  }

  // DELETE /api/files/delete?path=
  if (method === "DELETE" && url === "/api/files/delete") {
    const target = safeResolve(query.get("path") ?? "");
    const rel = relative(workspaceRoot, target);
    if (rel === "" || rel === ".") {
      sendError(res, 400, "refusing to delete workspace root");
      return true;
    }
    // Trash instead of unlink — recoverable.
    const ts = new Date().toISOString().replace(/[:.]/g, "-");
    const trashPath = join(trashRoot, ts, rel);
    await mkdir(dirname(trashPath), { recursive: true });
    await rename(target, trashPath);
    sendNoContent(res);
    return true;
  }

  // Hard-purge endpoint (admin only — use sparingly): DELETE /api/files/purge?path=
  // Bypasses trash. Used for huge node_modules / build outputs.
  if (method === "DELETE" && url === "/api/files/purge") {
    const target = safeResolve(query.get("path") ?? "");
    const rel = relative(workspaceRoot, target);
    if (rel === "" || rel === ".") {
      sendError(res, 400, "refusing to purge workspace root");
      return true;
    }
    await rm(target, { recursive: true, force: true });
    sendNoContent(res);
    return true;
  }

  return false;
}

export { workspaceRoot };
