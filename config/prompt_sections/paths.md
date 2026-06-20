# Paths

You run with a **project directory** — the path the user passed via `-p` / `--project` (or the org's configured project root). That directory is your working directory; treat it as the root for all relative paths.

## Two layouts (check which one you're in)

The sandbox-vs-host layout depends on how the kernel was invoked. **Verify with a single `pwd` or by trying the path** before assuming:

- **Host execution** (default for `orch agent prompt -p /abs/path`): the project directory IS the working directory. Scripts at `<project>/sandbox/X.py`, data at `<project>/data/`, configs at `<project>/config/`. Run as `python3 sandbox/X.py` from the project root.
- **Docker sandbox execution** (sandboxOnly mode): the project directory is mounted at `/sandbox/workspace/`. Scripts at `/sandbox/workspace/sandbox/X.py`. Run as `python3 /sandbox/workspace/sandbox/X.py`.

If a `file_read` or `cd` fails with "no such file or directory", you are using the wrong layout — switch to the other one. Do NOT retry the same path with cosmetic variations.

## Temporary files

Host: `$TMPDIR` or `/tmp/`. Docker sandbox: `/sandbox/tmp/`.

## Artifacts

Artifacts via `yield_artifact` land in the project's `workspace/memos/` directory (or `/sandbox/workspace/memos/` in Docker mode). The tool handles the path resolution — you just provide the filename.

## Org-specific overrides

If the org's MANIFESTO specifies a different layout (e.g., a project with no `sandbox/` subdir, or scripts at a non-standard path), follow the MANIFESTO. The MANIFESTO is authoritative for path conventions; this section is the default.
