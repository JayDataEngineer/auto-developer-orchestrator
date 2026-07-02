// pux-org-loader — turns a pi session into the CTO of a configured org.
//
// Activation: `--org <name>` CLI flag. When set:
//   1. The contents of `orgs/<name>/AGENTS.md` are appended to the base
//      system prompt (CTO overlay).
//   2. PUX_ORG=<name> is exported to the process environment so the Go
//      MCP server (which inherits this env via `task start`) can load
//      `orgs/<name>/policy.yaml` and apply declarative policy enforcement
//      (egress ACLs, env-only credentials, workspace mounts) at sandbox
//      create time. See backend/internal/policy/ for the schema.
//
// Everything else (subagent delegation, per-role tool whitelists, output
// files, thinking levels) is handled by pi-subagents natively — see
// .pi/agents/*.md for the agent-file format with rich frontmatter. The
// "CTO" is the main pi session; subagents are spawned via the `subagent`
// tool that pi-subagents registers.

import { existsSync } from "node:fs";
import { readFile } from "node:fs/promises";
import { join } from "node:path";
import process from "node:process";
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";

export default function (pi: ExtensionAPI) {
  pi.registerFlag("org", {
    description: "Activate org mode: append orgs/<name>/AGENTS.md to the system prompt.",
    type: "string",
    default: "",
  });

  pi.on("before_agent_start", async (event, _ctx) => {
    const orgName = pi.getFlag("org") as string | undefined;
    if (!orgName) return event;

    const orgAgentsMd = join(process.cwd(), "orgs", orgName, "AGENTS.md");
    if (!existsSync(orgAgentsMd)) {
      console.error(
        `pux-org-loader: ${orgAgentsMd} not found. ` +
          `Run without --org, or create orgs/${orgName}/AGENTS.md.`,
      );
      return event;
    }

    // Export PUX_ORG so the Go MCP server (a child process via task start)
    // reads it at sandbox create time and loads policy.yaml for this org.
    // Empty policy.yaml = no enforcement; missing file = no enforcement.
    // Opt-in is purely per-org via file presence.
    process.env.PUX_ORG = orgName;

    const body = await readFile(orgAgentsMd, "utf-8");
    return {
      ...event,
      systemPrompt: (event.systemPrompt ?? "") + `\n\n## Org: ${orgName}\n\n${body}`,
    };
  });
}
