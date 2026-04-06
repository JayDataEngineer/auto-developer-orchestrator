/**
 * Hooks Extension
 *
 * Listens to tool_call and tool_result events and runs shell hooks
 * from .pi/hooks/ directory. This enables self-correction and validation.
 *
 * Hook scripts:
 *   .pi/hooks/pre-tool-use.sh    - receives HOOK_TOOL_NAME, HOOK_TOOL_INPUT
 *   .pi/hooks/post-tool-use.sh   - receives HOOK_TOOL_NAME, HOOK_TOOL_INPUT, HOOK_TOOL_OUTPUT
 *   .pi/hooks/on-tool-failure.sh - receives HOOK_TOOL_NAME, HOOK_TOOL_INPUT, HOOK_ERROR
 *
 * Scripts output JSON on stdout:
 *   {"action": "allow"}                          - proceed normally
 *   {"action": "deny", "reason": "..."}          - block the tool
 *   {"action": "retry", "input": {...}}          - retry with modified input (failure hook only)
 */

import type { ExtensionAPI, ExtensionContext } from "@mariozechner/pi-coding-agent";
import { execSync, spawn } from "node:child_process";
import * as fs from "node:fs";
import * as path from "node:path";

interface HookResult {
	action: "allow" | "deny" | "retry";
	reason?: string;
	input?: Record<string, unknown>;
}

function runHook(hooksDir: string, hookType: string, env: Record<string, string>): HookResult | null {
	// Check for executable scripts
	const candidates = [hookType, `${hookType}.sh`, `${hookType}.bash`, `${hookType}.py`];

	for (const name of candidates) {
		const scriptPath = path.join(hooksDir, name);
		if (!fs.existsSync(scriptPath)) continue;
		try {
			fs.accessSync(scriptPath, fs.constants.X_OK);
		} catch {
			continue; // not executable
		}

		const envVars = { ...process.env, ...env };
		const envArgs = Object.entries(envVars).map(([k, v]) => `${k}=${v}`);
		try {
			const output = execSync(scriptPath, {
				env: envVars,
				encoding: "utf-8",
				timeout: 10000,
				maxBuffer: 1024 * 1024,
			}).trim();

			if (!output) return null;

			try {
				return JSON.parse(output) as HookResult;
			} catch {
				return null; // non-JSON output, ignore
			}
		} catch {
			return null; // hook failed, don't block execution
		}
	}

	return null;
}

export default function (pi: ExtensionAPI) {
	pi.on("tool_call", async (event, ctx) => {
		const hooksDir = path.join(ctx.cwd, ".pi", "hooks");
		if (!fs.existsSync(hooksDir)) return;

		const toolName = event.toolName || "";
		const toolInput = event.input ? JSON.stringify(event.input) : "";

		const result = runHook(hooksDir, "pre-tool-use", {
			HOOK_EVENT: "tool_call",
			HOOK_TOOL_NAME: toolName,
			HOOK_TOOL_INPUT: toolInput,
			HOOK_TURN_ID: event.toolCallId || "",
		});

		if (result?.action === "deny") {
			// Block the tool call
			ctx.abort();
		}
		// Note: We can't mutate tool input directly, but we can block/allow
	});

	pi.on("tool_result", async (event, ctx) => {
		const hooksDir = path.join(ctx.cwd, ".pi", "hooks");
		if (!fs.existsSync(hooksDir)) return;

		const toolName = event.toolName || "";
		const toolInput = event.input ? JSON.stringify(event.input) : "";
		const toolOutput = event.result ? JSON.stringify(event.result) : "";

		const isError = event.isError || false;

		if (isError) {
			const result = runHook(hooksDir, "on-tool-failure", {
				HOOK_EVENT: "tool_failure",
				HOOK_TOOL_NAME: toolName,
				HOOK_TOOL_INPUT: toolInput,
				HOOK_ERROR: toolOutput,
				HOOK_TURN_ID: event.toolCallId || "",
			});

			if (result?.action === "retry" && result.input) {
				// Send a follow-up message with retry suggestion
				const retryMsg = `The ${toolName} tool failed. Suggested retry parameters: ${JSON.stringify(result.input)}`;
				pi.sendMessage({
					role: "user",
					content: [{ type: "text", text: retryMsg }],
				} as any);
			}
		} else {
			const result = runHook(hooksDir, "post-tool-use", {
				HOOK_EVENT: "tool_result",
				HOOK_TOOL_NAME: toolName,
				HOOK_TOOL_INPUT: toolInput,
				HOOK_TOOL_OUTPUT: toolOutput,
				HOOK_TURN_ID: event.toolCallId || "",
			});

			if (result?.reason) {
				// Inject feedback as a user message
				const feedbackMsg = `Post-execution note for ${toolName}: ${result.reason}`;
				pi.sendMessage({
					role: "user",
					content: [{ type: "text", text: feedbackMsg }],
				} as any);
			}
		}
	});
}
