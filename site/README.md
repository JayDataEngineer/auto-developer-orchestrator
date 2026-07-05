# pux site

Standalone React/Vite/CopilotKit frontend — a browser workbench for the Pux
harness (chat sidebar + editor / terminal / sandbox / VNC panels), backed by
[CopilotKit](https://docs.copilotkit.ai/) + the harness's AG-UI endpoint.

Self-contained: `site/` has its own `package.json` / `package-lock.json` /
`tsconfig.json` (the repo root is a uv workspace, not an npm workspace), so
`rm -rf site/` leaves the rest of the project untouched.

## Layout

```
site/
├── package.json          # standalone npm project (not a root workspace member)
├── package-lock.json
├── tsconfig.json
├── vite.config.ts        # dev port 5176 (strictPort); /api → 127.0.0.1:3001
├── index.html
├── public/favicon.svg
├── server/               # Node BFF (PUX_SITE_PORT, default 3001)
│   ├── index.ts          # HTTP route layer + production SPA fallback
│   ├── copilotkit.ts     # /api/copilotkit → harness AG-UI (:9988/agui/<org>)
│   ├── agent-protocol.ts # server-side Agent Protocol client (:9988)
│   ├── files.ts          # /api/files/**
│   ├── sandbox.ts        # /api/sandbox/**
│   ├── terminal.ts       # /api/terminal/ws (node-pty PTY)
│   └── vnc-proxy.ts      # /api/sandbox/vnc/** (HTTP + WebSocket reverse-proxy)
└── src/
    ├── main.tsx
    ├── App.tsx           # CopilotSidebar + workbench shell
    ├── index.css         # tailwind 4
    ├── lib/              # api.ts (→ harness REST), runtime.tsx, store.ts,
    │                     # use-agents.ts, utils.ts
    └── components/
        ├── workbench/    # editor / file-tree / terminal / sandbox / settings / vnc panels
        └── ui/           # button / dialog / separator / skeleton / tooltip
```

## Run

The harness must be running first — it serves both the Agent Protocol REST
API and the AG-UI endpoint the chat sidebar uses:

```bash
# from the repo root
pux serve                          # FastAPI on http://127.0.0.1:9988

# then, from this folder
npm install
npm run dev                        # vite (5176) + Node BFF (3001) concurrently
```

Open http://127.0.0.1:5176 — pick an org, send a message, see the streaming
reply in the CopilotKit sidebar; the workbench panels drive the sandbox.

## How it wires

```
Browser (5176)
  ├── /api/copilotkit ──vite proxy──> Node BFF (3001) ──copilotkit.ts──> harness AG-UI (:9988/agui/<org>)
  ├── /api/{files,sandbox,vnc,terminal/ws} ──vite proxy──> Node BFF (3001)
  └── Agent Protocol CRUD (threads/runs/agents) ───────────────────────> harness REST (:9988)
```

The Node BFF only owns the routes that need Node.js (file ops over the
bind-mount, sandbox lifecycle, a `node-pty` terminal, a VNC reverse-proxy, and
the CopilotKit↔AG-UI translation). Everything else — thread/run/agent CRUD —
goes browser-direct to the harness Agent Protocol. In production, `GET *` on
the BFF falls through to the built React assets in `dist/`.

## Production

```bash
npm run build   # vite → dist/, tsup → dist-server/
npm start       # NODE_ENV=production node --import tsx server/index.ts (serves SPA + API)
```

## Env

- `PUX_SITE_PORT` — Node BFF port (default `3001`)
- `PUX_SITE_HOST` — Node BFF host (default `127.0.0.1`)
- `PUX_HARNESS_URL` — harness URL for the CopilotKit↔AG-UI route (default `http://127.0.0.1:9988`)
- `PUX_API_URL` — harness URL for the server-side Agent Protocol client (default `http://127.0.0.1:9988`)
- `VITE_PUX_HARNESS_URL` — harness URL for the browser-direct Agent Protocol calls (default `http://127.0.0.1:9988`; set at dev/build time, baked into the bundle)

## Delete-proof

`rm -rf site/` from the repo root leaves the rest of the project untouched —
no root `package.json` workspace entry, no shared tsconfig, no path aliases
that escape this folder.
