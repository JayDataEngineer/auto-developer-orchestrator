# Folder Browser — Implementation Notes

## What Changed

### Backend (Go)

**`go-backend/internal/handlers/fs_browse.go`** — Added `Mkdir` handler
- `POST /api/fs/mkdir` — creates directory under allowed roots
- Validates path is under allowlist, sanitizes name (no path separators, no ..)

**`go-backend/internal/handlers/ssh_browse.go`** — Added `SshMkdir` handler
- `POST /api/pux/ssh/mkdir` — creates directory on remote via SFTP
- Same sanitization rules as local mkdir

**`go-backend/internal/handlers/tailscale.go`** — New file
- `GET /api/tailscale/devices` — runs `tailscale status --json`, returns online peers
- Gracefully returns `{available: false}` when tailscale not installed

**`go-backend/cmd/server/app.go`** — Wired new routes
- `r.Post("/fs/mkdir", fsBrowseHandler.Mkdir)`
- `r.Get("/tailscale/devices", tailscaleHandler.Devices)`
- `r.Post("/ssh/mkdir", h.sshBrowse.SshMkdir)` via pux routes

### Frontend (React)

**`src/web/src/components/add-project-dialog.tsx`** — Complete rewrite

All 10 gaps fixed:

1. **Selection bug fixed** — Single click = select (highlight), double click = navigate into. "Use this folder" option at top of every listing lets you select the current directory itself.

2. **Search/filter** — Search input in toolbar. Client-side case-insensitive filtering. Escape clears.

3. **Keyboard navigation** — Arrow up/down moves focus, Enter opens/navigates, Space selects, Backspace goes up, Escape clears search.

4. **Hidden files toggle** — Eye icon in toolbar. Client-side toggle.

5. **Create folder** — FolderPlus button in toolbar. Inline input with Enter/Escape. Works for local + SSH. Backend mkdir endpoints.

6. **SSH host key trust** — Detects host key errors, shows yellow trust prompt with fingerprint, "Trust & Connect" button retries.

7. **Saved SSH connections** — Last 5 connections stored in `localStorage` under `pux:ssh-recent`. Shown as quick-pick in SSH tab.

8. **Tailscale tab** — Fourth source tab. Discovers online devices via `GET /api/tailscale/devices`. Click device → auto-connects SSH. Falls back to SSH form if auth fails.

9. **File type icons** — Maps extensions to lucide icons: code → FileCode, images → FileImage, JSON/YAML → FileJson, archives → FileArchive, text → FileText.

10. **Better breadcrumbs** — Widened to `max-w-32`, home button in toolbar.

## Interaction Model

```
Click folder        → Select it (highlight, enable Add)
Double-click folder → Navigate into it
"Use this folder"   → Select the current directory
Search box          → Filter entries in current view
Arrow keys          → Move keyboard focus
Enter               → Open/navigate focused directory
Space               → Select focused directory
Backspace           → Go up one level
```
