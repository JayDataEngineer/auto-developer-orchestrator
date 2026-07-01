// pux-org-loader — pi extension that turns pi into the CTO of a configured org.
//
// Wires the orgs/<name>/org.toml + cto.md substrate (shipped under orgs/_demo/)
// into pi's runtime: appends the CTO prompt body to the system prompt, filters
// the active tool list to the CTO's whitelist, and rejects any tool call that
// slips past the filter.
//
// Activated by the --org <name> CLI flag. Without it, this extension is inert
// and pi runs as a plain sandbox agent.
//
// Phase 4 adds the delegate_to tool (spawns a child pi session with a role's
// prompt + whitelist). Keeping that out of Phase 3 because the spawn primitive
// is shared with the dispatch wrapper — cleaner to ship them together.

import { readFile } from "node:fs/promises";
import { join } from "node:path";
import process from "node:process";
import { Type } from "typebox";
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import * as TOML from "@iarna/toml";

interface RoleSpec {
  name: string;
  prompt: string;
  max_rounds?: number;
  tools?: string[];
  model?: string;
}

interface OrgConfig {
  name: string;
  description?: string;
  sandbox_image?: string;
  idle_shutdown_secs?: number;
  cto?: {
    prompt: string;
    max_rounds?: number;
    tools?: string[];
    model?: string;
  };
  roles?: RoleSpec[];
}

interface LoadedOrg {
  config: OrgConfig;
  ctoPromptBody: string;
  orgDir: string;
}

const cache = new Map<string, LoadedOrg | null>();

async function loadOrg(orgName: string, projectRoot: string): Promise<LoadedOrg | null> {
  if (cache.has(orgName)) return cache.get(orgName) ?? null;

  const orgDir = join(projectRoot, "orgs", orgName);
  const tomlPath = join(orgDir, "org.toml");
  const ctoPath = join(orgDir, "cto.md");

  let tomlText: string;
  try {
    tomlText = await readFile(tomlPath, "utf-8");
  } catch (err) {
    console.error(
      `pux-org-loader: cannot read ${tomlPath}: ${(err as Error).message}. ` +
        `Run without --org, or create orgs/${orgName}/org.toml.`,
    );
    cache.set(orgName, null);
    return null;
  }

  let config: OrgConfig;
  try {
    config = TOML.parse(tomlText) as unknown as OrgConfig;
  } catch (err) {
    console.error(`pux-org-loader: failed to parse ${tomlPath}: ${(err as Error).message}`);
    cache.set(orgName, null);
    return null;
  }

  if (!config.cto?.prompt) {
    console.error(
      `pux-org-loader: ${tomlPath} missing [cto] block with prompt= — cannot load org.`,
    );
    cache.set(orgName, null);
    return null;
  }

  let ctoPromptBody = "";
  try {
    ctoPromptBody = await readFile(ctoPath, "utf-8");
  } catch (err) {
    console.error(`pux-org-loader: cannot read CTO prompt ${ctoPath}: ${(err as Error).message}`);
    cache.set(orgName, null);
    return null;
  }

  const loaded: LoadedOrg = { config, ctoPromptBody, orgDir };
  cache.set(orgName, loaded);
  return loaded;
}

async function readRolePrompt(org: LoadedOrg, roleName: string): Promise<string | null> {
  const role = org.config.roles?.find((r) => r.name === roleName);
  if (!role) {
    console.error(
      `pux-org-loader: role '${roleName}' not declared in ${join(org.orgDir, "org.toml")}.`,
    );
    return null;
  }
  try {
    return await readFile(join(org.orgDir, role.prompt), "utf-8");
  } catch (err) {
    console.error(
      `pux-org-loader: cannot read role prompt ${role.prompt}: ${(err as Error).message}`,
    );
    return null;
  }
}

export default function (pi: ExtensionAPI) {
  // --org <name>: project-relative org directory. Empty = no org (plain agent).
  pi.registerFlag("org", {
    description: "Activate org mode: load orgs/<name>/org.toml + CTO prompt.",
    type: "string",
    default: "",
  });

  // Inject the CTO body and shrink the active tool list to the whitelist.
  pi.on("before_agent_start", async (event, _ctx) => {
    const orgName = pi.getFlag("org") as string | undefined;
    if (!orgName) return event;

    const projectRoot = process.cwd();
    const org = await loadOrg(orgName, projectRoot);
    if (!org) return event;

    const header =
      `\n\n## Org: ${org.config.name}\n\n` +
      (org.config.description ? `${org.config.description}\n\n` : "") +
      `### CTO prompt body (from orgs/${orgName}/cto.md)\n\n${org.ctoPromptBody}`;

    return {
      ...event,
      systemPrompt: (event.systemPrompt ?? "") + header,
    };
  });

  // Apply the whitelist once at session start (cheaper than re-checking each
  // tool_call). setActiveTools removes non-whitelisted tools from the prompt
  // entirely so the LLM never sees them.
  pi.on("session_start", async (_event, ctx) => {
    const orgName = pi.getFlag("org") as string | undefined;
    if (!orgName) return;

    const org = await loadOrg(orgName, process.cwd());
    if (!org?.config.cto?.tools) return;

    // The whitelist names tools the CTO may call directly. delegate_to is
    // Phase 4 — drop it here so setActiveTools doesn't refuse on missing tool.
    const whitelist = org.config.cto.tools.filter((t) => t !== "delegate_to");
    if (whitelist.length === 0) return;

    const active = pi.getActiveTools().filter((name) => whitelist.includes(name));
    // Always keep core pi tools the operator needs (read/edit/bash are pi built-ins
    // separate from MCP-direct tools — but to be safe, intersect against whitelist).
    pi.setActiveTools(active);
    ctx.ui.notify(`pux-org-loader: ${orgName} CTO whitelist active (${active.length} tools)`, "info");
  });

  // Belt-and-suspenders: deny any tool call outside the whitelist. Catches
  // tools that slip past setActiveTools (e.g. newly registered mid-session).
  pi.on("tool_call", async (event, _ctx) => {
    const orgName = pi.getFlag("org") as string | undefined;
    if (!orgName) return;

    const org = await loadOrg(orgName, process.cwd());
    if (!org?.config.cto?.tools) return;

    const whitelist = org.config.cto.tools;
    if (!whitelist.includes(event.toolName)) {
      return {
        block: true,
        reason: `Tool '${event.toolName}' is not in the ${orgName} CTO whitelist (${whitelist.join(", ")}).`,
      };
    }
  });

  // Phase 4 will register delegate_to here. The signature is locked in via
  // the type so the role prompts can reference it from day one.
  void readRolePrompt;
  void Type;
}
