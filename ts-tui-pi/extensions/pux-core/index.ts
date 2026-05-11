/**
 * PUX Core Extension
 *
 * Provides custom TUI rendering for PUX-specific tools (delegate_to, delegate_async, memory).
 * These tools are executed by the Go backend — this extension only handles visual rendering
 * in the terminal UI via renderCall/renderResult on the tool definitions.
 */

import { Type } from "@sinclair/typebox";
import type { ExtensionAPI } from "../../src/core/extensions/types.js";
import { renderDelegateCall, renderDelegateResult } from "./tools/delegate.js";
import { renderMemoryCall } from "./tools/memory.js";

export default function registerPuxCoreExtension(pi: ExtensionAPI): void {
	// Register render-only tool definitions for PUX delegation tools.
	// The Go backend handles actual execution — these definitions provide
	// custom TUI rendering via renderCall/renderResult.

	pi.registerTool({
		name: "delegate_to",
		label: "Delegate",
		description: "Delegate a task to an employee agent (render-only — execution handled by Go backend)",
		parameters: Type.Object({
			instructions: Type.Optional(Type.String()),
			step: Type.Optional(Type.String()),
			agent_name: Type.Optional(Type.String()),
			role: Type.Optional(Type.String()),
			task: Type.Optional(Type.String()),
		}),
		execute: async () => ({ content: [] }),
		renderCall: (args, theme) => renderDelegateCall(args as any, theme),
		renderResult: (result, options, theme) => renderDelegateResult(result as any, options, theme),
	});

	pi.registerTool({
		name: "delegate_async",
		label: "Delegate Async",
		description: "Delegate a task asynchronously to an employee agent (render-only)",
		parameters: Type.Object({
			instructions: Type.Optional(Type.String()),
			step: Type.Optional(Type.String()),
			agent_name: Type.Optional(Type.String()),
			role: Type.Optional(Type.String()),
			task: Type.Optional(Type.String()),
		}),
		execute: async () => ({ content: [] }),
		renderCall: (args, theme) => renderDelegateCall(args as any, theme),
		renderResult: (result, options, theme) => renderDelegateResult(result as any, options, theme),
	});

	pi.registerTool({
		name: "memory",
		label: "Memory",
		description: "Memory operations (render-only)",
		parameters: Type.Object({
			key: Type.Optional(Type.String()),
			action: Type.Optional(Type.String()),
		}),
		execute: async () => ({ content: [] }),
		renderCall: (args, theme) => renderMemoryCall(args as any, theme),
	});

	pi.registerTool({
		name: "save_memory",
		label: "Save Memory",
		description: "Save to memory (render-only)",
		parameters: Type.Object({
			key: Type.Optional(Type.String()),
			action: Type.Optional(Type.String()),
		}),
		execute: async () => ({ content: [] }),
		renderCall: (args, theme) => renderMemoryCall(args as any, theme),
	});
}
