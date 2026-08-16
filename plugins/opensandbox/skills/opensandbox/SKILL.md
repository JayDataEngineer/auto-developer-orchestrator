---
name: opensandbox
description: Run work in upstream OpenSandbox sandboxes — dcode --sandbox opensandbox for full isolation, the opensandbox MCP tools for ad-hoc sandbox_create/command_run/file_*, and the osb CLI for humans.
---

# OpenSandbox

The sandbox platform is upstream
[OpenSandbox](https://github.com/opensandbox-group/OpenSandbox) — never a
handrolled container. Three surfaces, one server (`make sandbox`, :8080):

| Surface | How | Use |
|---|---|---|
| dcode sandbox provider | `dcode --sandbox opensandbox` | dcode's own execute/read/write/glob/grep run inside the sandbox |
| MCP server (`opensandbox` in `.mcp.json`) | `sandbox_create` / `command_run` / `file_*` tools | ad-hoc sandboxes from any session |
| `osb` CLI | `osb sandbox create --image python:3.12` | human-driven inspection |

## Install the provider (once per dcode upgrade)

```bash
uv pip install --python "$(uv tool dir)/deepagents-code/bin/python" ./plugins/opensandbox
dcode --sandbox opensandbox
```

Env knobs (all optional): `OPENSANDBOX_DOMAIN` (localhost:8080),
`OPENSANDBOX_PROTOCOL` (http), `OPENSANDBOX_API_KEY`, `OPENSANDBOX_IMAGE`
(python:3.12), `OPENSANDBOX_TIMEOUT_HOURS` (8).

## Notes

- The provider bridges dcode's `SandboxProvider` seam to the OpenSandbox SDK;
  every file operation derives from `commands.run` + `files` service.
- Browser/desktop environments are upstream examples (chrome+VNC, playwright,
  desktop, vscode) — create a sandbox with such an image rather than building
  containers.
- Server lifecycle: `make sandbox` / `sandbox-status` / `sandbox-stop`.
