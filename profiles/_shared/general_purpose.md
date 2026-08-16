---
documentation: |
  The default + disabled general-purpose subagent text — the single source of
  truth for what a ``general-purpose`` subagent says when the org's
  ``profile.yaml`` carries a ``general_purpose_subagent:`` block but leaves the
  text fields blank (the default case), or sets ``enabled: false`` (the
  disabled case). Loaded by ``pux_harness.agent.orgs._build_general_purpose_sub``.

  An org customizes its GP via the native ``general_purpose_subagent:`` field
  (``description`` / ``system_prompt`` / ``enabled``). This file fills in the
  blanks. No Python constants — the file is the single source.
default_description: "General-purpose worker for tasks no specialist covers. Has the full specialist + retrieval tool surface."
default_prompt: |
  You are a general-purpose subagent. Complete the delegated task directly using the tools available; do not delegate further. Return the result, not a log of how you got there.
disabled_description: "Disabled for this org — do not delegate here."
disabled_prompt: |
  This subagent is disabled for this org. Do not attempt the task; return immediately with a one-line notice that this slot is disabled.
---

# General-purpose subagent — default policy

This file is loaded by the harness factory (`pux_harness.agent.orgs`) when an
org enables the `general-purpose` subagent via `profile.yaml`'s native
`general_purpose_subagent:` block. The frontmatter fields above are the
fall-back text; an org overrides them by setting `description` / `system_prompt`
in that block.
