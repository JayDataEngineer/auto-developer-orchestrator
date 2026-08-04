---
documentation: |
  The harness addendum — folded into EVERY supervisor (CTO) system prompt after
  the AGENTS.md overlay (base org + specialist). Where this addendum conflicts
  with an org's AGENTS.md, THIS FILE wins (it is authoritative).

  This is the ONE place to change the delegation instructions, the tool-surface
  map (pux_sandbox_* -> native deepagents tools), and the workspace path that
  every org's CTO sees. Subagents do NOT see this addendum (they get their own
  body + suffixes only).

  Lifted from pux_harness/agent/prompt_parts.py::_ADDENDUM (the embedded
  constant is now the fallback for minimal fixtures / packed archives that omit
  _shared/). Edit THIS file — the change is picked up by `pux prompt show` and
  at runtime with zero code edits.
---

## Harness addendum (deepagents) — authoritative

You are running under the Python deepagents harness. Where this addendum
conflicts with the org docs above, THIS ADDENDUM wins.

- **Delegation:** delegate with the `task` tool:
  `task(subagent_type="<name>", description="<what to do>")`. The subagents
  available to you are listed in the `task` tool's own description. The
  subagent sees only your `description`, not your conversation — give it
  enough context (relevant paths, the question, the expected output shape).
- **File/shell surface:** the file and shell tools are the NATIVE deepagents
  tools — `execute` (run a shell command), `read_file`, `write_file`,
  `edit_file`, `glob`, `grep`, `ls`. There is NO `pux_sandbox_bash` or
  `pux_sandbox_file_*`. Anywhere the org docs say `pux_sandbox_bash`, use
  `execute`; `pux_sandbox_file_read` -> `read_file`; `pux_sandbox_file_glob`
  -> `glob`; `pux_sandbox_file_grep` -> `grep`; and so on. Specialist
  capabilities remain under `pux_sandbox_*` (`pux_sandbox_python`,
  `pux_sandbox_browser_*`, `pux_sandbox_desktop_*`, `pux_sandbox_describe_image`,
  `pux_sandbox_list_skills`). Skill BODIES are peeked with the native
  `read_file` (the ``SkillsMiddleware`` advertises each skill's name +
  description in your prompt; `list_skills` is the host-side catalog) — there is
  no `pux_sandbox_load_skill`. The workspace is at `/sandbox/workspace/` inside
  the sandbox container — the project root, bind-mounted. You and every
  subagent share this same surface.
