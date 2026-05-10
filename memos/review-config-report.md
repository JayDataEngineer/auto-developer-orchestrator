# Configuration Files Review Report

**Date:** 2025-04-14  
**Scope:** 16 configuration files  
**Reviewer:** Config Auditor  

---

## 1. 🔴 CRITICAL (must-fix)

### 🔴 `.env` — Hardcoded Production Secrets (Security Breach)
- **File:** `.env`  
- **Line 1:** `GITHUB_TOKEN="REDACTED"` — Real GitHub PAT in plaintext. Rotate immediately.  
- **Line 2:** `JULES_API_KEY="REDACTED"` — Hardcoded Jules API key.  
- `.env` is tracked in `.gitignore` (wisely), but these should never have been committed. Remove from git history / rotate.

### 🔴 `.env.example` — Missing `JULES_API_KEY` Documentation
- `.env.example` documents `GITHUB_TOKEN` but omits `JULES_API_KEY`. Anyone cloning the repo won't know this var is required for CI failure-fixer workflows.

### 🔴 `docker-compose.langfuse.yml` — Plaintext Passwords Everywhere
| Variable | Value | Risk |
|---|---|---|
| `NEXTAUTH_SECRET` | `orch-langfuse-nextauth-2026` | JWT signing key — must be random |
| `ENCRYPTION_KEY` | `a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2` | Static hex — insufficient entropy |
| `LANGFUSE_INIT_USER_PASSWORD` | `orch-admin-2026` | Admin password in plaintext |
| `POSTGRES_PASSWORD`, `CLICKHOUSE_PASSWORD`, `REDIS_AUTH`, `MINIO_ROOT_PASSWORD` | All hardcoded | Data-at-rest exposure |
- **Fix:** Use environment variables from a `.env` file; reference with `${VAR}` syntax.

### 🔴 `tsconfig.json` — `skipLibCheck: true` Hides Type Errors
- **Line 14:** `"skipLibCheck": true` — suppresses type-checking of all `.d.ts` files. May hide real incompatibilities.  
- **`noUnusedLocals` / `noUnusedParameters` / `strictNullChecks` / `strict`** — none enabled. This is a relaxed config for what claims to be a production app.

### 🔴 `Taskfile.yml` — Hardcoded Langfuse API Keys in Command
- **Line 52 (around):** `go run cmd/server/main.go` with inline `LANGFUSE_PUBLIC_KEY=pk-orch-2026-lf-a1b2c3d4e5f6` and `LANGFUSE_SECRET_KEY=sk-orch-2026-lf-a1b2c3d4e5f6` — these are the **same keys** used by `docker-compose.langfuse.yml`'s `LANGFUSE_INIT_PROJECT_PUBLIC_KEY`. If committed, these keys are public.

### 🔴 `docker-compose.yml` — No `depends_on` Between Services
- Frontend proxies `/api` to the Go backend, but there is **no `depends_on`** on `go-backend`. Docker may start frontend before Go is ready, causing 502 errors.  
- Backend mounts `docker.sock` with **write access** (`/var/run/docker.sock`) — allows container breakout.

### 🔴 `docker-compose.dev.yml` — No `depends_on` + No Healthchecks
- Zero healthchecks on any service. The `go-backend` service mounts `docker.sock` as writable. Same container breakout risk.

### 🔴 `CI Workflow` — `go-version: "1.26"` Does Not Exist Yet
- **File:** `.github/workflows/ci.yml`, line 17: `go-version: "1.26"`  
- Latest stable Go release is **1.24.x**. `1.26` is a future/unreleased version. CI will **fail immediately** with "version not found".

### 🔴 `package.json` — Vite 6 + Vitest 2 Incompatibility
- `"vite": "^6.2.0"` and `"vitest": "^2.0.0"` — **Vitest 2.x requires Vite ^5.x**. `npm install` will likely produce peer dependency warnings or breakage.  
- **Fix:** Use `vitest@3.x` (compatible with Vite 6) or downgrade Vite to ^5.

---

## 2. 🟡 WARNING (should-fix)

### 🟡 `nginx.conf` — Zero Security Headers
- No `X-Frame-Options`, `X-Content-Type-Options`, `Content-Security-Policy`, `Strict-Transport-Security`, `Referrer-Policy`, or `Permissions-Policy`. Production deployment is vulnerable to clickjacking, MIME sniffing, and XSS.

### 🟡 `nginx.conf` — Hardcoded Backend IP
- **Line 22:** `set $backend http://172.17.0.1:3847;` — Hardcoded Docker bridge IP. Will break on Docker Desktop (macOS/Windows), different network setups, or Kubernetes. Should use service name.

### 🟡 `Dockerfile` — No `.dockerignore` Utilized
- The `.dockerignore` exists at root but the production `Dockerfile` copies everything (`.`) into the builder stage. The `.dockerignore` lists `docs`, `Dockerfile*`, `docker-compose*.yml`, etc., but these files still get sent to the Docker daemon as context. Fine for small repos, but bloated.

### 🟡 `Dockerfile` — Runs as Root
- No `USER` directive. Both the Go binary and nginx run as root in the final stage.  
- **Fix:** `USER nginx` for the nginx stage; create a non-root user for the Go binary.

### 🟡 `Dockerfile` — No HEALTHCHECK
- Neither the root `Dockerfile` (frontend) nor `go-backend/Dockerfile` define a `HEALTHCHECK` instruction. The compose file has a healthcheck on `go-backend` (via `wget`), but nothing for frontend.

### 🟡 `go-backend/Dockerfile` — Wrong Base Image
- **Line 12:** Uses `node:22-alpine` as the final runtime base image — but the binary is a **Go static binary** (`CGO_ENABLED=0`). Using `scratch`, `distroless`, or `alpine:3` would be smaller and more secure.  
- Similarly, installing `git`, `openssh-client`, `ca-certificates`, `sqlite`, **and** the Pi coding agent (`npm install -g @mariozechner/pi-coding-agent@latest`) adds unnecessary attack surface.

### 🟡 `Taskfile.yml` — `dotenv: [".env"]` Will Leak Secrets in CI
- The Taskfile loads `.env` which contains secrets. If `task` is run in CI with a `.env` file present, secrets leak into logs.

### 🟡 `docker-compose.dev.yml` — Frontend Port Not Exposed
- The `frontend-dev` service has **no `ports:` mapping**. While Traefik is configured, if someone needs to access Vite directly (e.g., for debugging), there's no host port.

### 🟡 `vitest.config.e2e.ts` — Pattern Mismatch
- The E2E vitest config includes `tests/e2e/**/*.test.ts` but the e2e test directory contains **both `.test.ts` and `.spec.ts`** files. Playwright uses `.spec.ts`; Vitest won't pick up `.spec.ts` files. Inconsistent.

### 🟡 `vite.config.ts` — Proxy `rewrite` is No-Op
- **Line 21:** `rewrite: (path) => path` — an identity function. Can be removed.

### 🟡 `.gitignore` — Overly Broad Patterns
- `*.log` — may miss important log files  
- `data/` — completely ignores the data directory. What if someone needs to commit a seed database?  
- `passwords.txt` — good intention, but overly specific  
- `.env*` (line 7) matches `.env.production`, `.env.local` — good, but note it's broad

### 🟡 `.gitignore` — Ignores `projects/` But This Is Core Data
- `projects/` is gitignored. If the project relies on starter project templates in `projects/`, they'd need to be committed or bootstrapped separately.

---

## 3. 🟢 INFO (nitpick / suggestions)

### 🟢 `package.json` — `test:e2e` Uses Vitest, But `test:playwright` Exists
- Two E2E test entries (`test:e2e` → vitest, `test:playwright` → playwright). Confusing. Consider unifying.

### 🟢 `playwright.config.ts` — `webServer` Port Conflict
- **Line 40:** Web server command `npm run dev` will start Vite on port 5174 (the `--port` in `package.json`), but the `baseURL` is also `localhost:5174`. This works, but if the Vite `server.port` in `vite.config.ts` is 5174, the `--port` flag in `package.json` `dev` script is redundant (set twice).

### 🟢 `vite.config.ts` — Logging in Production Build
- The `configure` event handlers (lines 24-29) `console.log` every request. In dev this is fine, but these callbacks run in the Vite dev server **and** on any client-side proxy usage. Minimal perf impact.

### 🟢 `tsconfig.json` — Uses `experimentalDecorators: true` but No Decorators in Dependencies
- Angular-style decorators are enabled. If not using them, disable to avoid confusion.

### 🟢 `docker-compose.langfuse.yml` — `name: orchestrator-langfuse`
- Docker Compose v2 `name` field is used. Ensure all developers have a recent Docker Compose version (v2.3+).

### 🟢 `Taskfile.yml` — CRLF Risk in Command Lines
- The file content appears clean (UTF-8), but some commands use `&` (HTML entities) in the yaml. The YAML parser handles this correctly, but it's unusual formatting.

### 🟢 `.dockerignore` — Inconsistent with Dockerfile
- `.dockerignore` ignores `Dockerfile*` and `docker-compose*.yml`, but both compose files use `context: .` which includes the Dockerfiles they reference. Build context still works (Docker daemon receives files but Dockerfile instructions can't reference them).

### 🟢 `nginx.conf` — Hardcoded DNS resolver
- **Line 1:** `resolver 127.0.0.11` — Docker's embedded DNS. This is fine for Docker but will break in Kubernetes. Consider parameterizing.

### 🟢 `CI Workflow` — Missing Frontend Tests
- Only Go tests and lint run. No `npm run test`, `npm run lint`, or `npm run build` for the frontend. The `playwright.config.ts` has `forbidOnly: !!process.env.CI` which is never triggered.

---

## Summary

| Severity | Count | Key Areas |
|---|---|---|
| 🔴 **CRITICAL** | 9 | Secrets in plaintext, Go 1.26 doesn't exist, Vite/Vitest incompatibility |
| 🟡 **WARNING** | 14 | Missing security headers, root containers, no healthchecks, CI gaps |
| 🟢 **INFO** | 9 | Minor improvements, deduplication, style |

### Top 3 Actions to Take
1. **Rotate all hardcoded secrets** (GitHub token, Jules API key, Langfuse keys) and move to environment variables
2. **Fix Go version** in CI to `1.24` or the actual latest stable
3. **Fix Vite/Vitest version mismatch** — upgrade vitest to 3.x or downgrade Vite

