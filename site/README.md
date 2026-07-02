# pux site

Standalone web UI driving **pi-mono** in-process via the official
**`@assistant-ui/react-pi`** adapter.

Isolated by design: not a workspace member of the root `package.json`.
Delete this folder and the rest of the repo is untouched.

## Layout

```
site/
├── package.json          # standalone (no workspace membership)
├── tsconfig.json
├── vite.config.ts        # dev port 5176, /api → 127.0.0.1:3001
├── index.html
├── public/favicon.svg
├── server/
│   ├── pi.ts             # createPiNodeClient({ workspacePath: repoRoot })
│   └── index.ts          # HTTP + SSE backend implementing /api/pi/**
└── src/
    ├── main.tsx
    ├── App.tsx
    ├── index.css         # tailwind 4 + dark theme tokens
    ├── lib/runtime.tsx   # createPiHttpClient("/api/pi") + usePiRuntime
    └── components/thread.tsx  # assistant-ui primitives, dark theme
```

## Run

From this folder:

```bash
npm install
npm run dev   # vite (5176) + tsx server (3001) concurrently
```

Open http://127.0.0.1:5176 — send a message, see streaming reply.

## How it wires

```
Browser ──HTTP/SSE──> vite proxy (/api/**) ──> Node server (3001)
                                                       │
                                            createPiNodeClient()
                                                       │
                                              pi-mono SDK in-process
                                                       │
                                          pux-mcpserver (Go, 9987)
                                                       │
                                              Docker sandbox
```

The Node server implements the PiClient wire contract (`/api/pi/**`) by
delegating to `createPiNodeClient()` from `@assistant-ui/react-pi/node`.
The browser uses `createPiHttpClient()` from `@assistant-ui/react-pi`.

## Production

```bash
npm run build   # vite → dist/, tsup → dist-server/
npm start       # node dist-server/index.js (serves SPA + API)
```

## Env

- `PUX_SITE_WORKSPACE` — workspace path for pi (defaults to repo root,
  two levels up from `server/pi.ts`)
- `PUX_SITE_PORT` — backend port (default 3001)
- `PI_PROVIDER` / `PI_MODEL_ID` — override model (else `~/.pi/agent/settings.json`)

## Delete-proof

`rm -rf site/` from the repo root leaves the rest of the project untouched.
No root `package.json` workspace entry, no shared tsconfig, no path aliases
that escape this folder.
