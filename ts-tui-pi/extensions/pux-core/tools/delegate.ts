import { Container, Spacer, Text } from "@mariozechner/pi-tui";
import type { Theme } from "../../../src/modes/interactive/theme/theme.js";

/**
 * Custom renderCall for delegate_to / delegate_async tools.
 * Shows agent role + task preview with a distinct visual style.
 */
export function renderDelegateCall(
	args: { instructions?: string; step?: string; agent_name?: string; task?: string; role?: string },
	theme: Theme,
): Container {
	const c = new Container();
	const role = args.instructions || args.step || args.agent_name || args.role || "agent";
	const task = args.task || "";
	const preview = task.length > 80 ? task.slice(0, 80) + "..." : task;

	const dot = theme.fg("accent", "●");
	const roleText = theme.fg("toolTitle", theme.bold(role));
	c.addChild(new Text(`${dot} ${roleText}`, 1, 0));

	if (preview) {
		c.addChild(new Text(theme.fg("dim", `  ${preview}`), 1, 0));
	}

	return c;
}

/**
 * Custom renderResult for delegate_to / delegate_async tools.
 * Shows agent status and output summary.
 */
export function renderDelegateResult(
	result: { content: Array<{ type: string; text?: string }>; details?: any; isError: boolean },
	options: { expanded: boolean },
	theme: Theme,
): Container {
	const c = new Container();

	// Extract text output
	const textContent = result.content
		.filter((block) => block.type === "text" && block.text)
		.map((block) => block.text!)
		.join("\n");

	const dot = result.isError
		? theme.fg("error", "●")
		: theme.fg("success", "●");

	// First line of output as summary
	const firstLine = textContent.split("\n").find((l) => l.trim()) ?? "";
	const summary = firstLine.length > 120 ? firstLine.slice(0, 120) + "..." : firstLine;

	c.addChild(new Text(`${dot} ${theme.fg("toolTitle", theme.bold("delegation"))}`, 1, 0));

	if (summary) {
		c.addChild(new Text(theme.fg("dim", `  ${summary}`), 1, 0));
	}

	// Expanded view shows full output
	if (options.expanded && textContent.length > 0) {
		c.addChild(new Spacer(1));
		const lines = textContent.split("\n").slice(0, 20);
		for (const line of lines) {
			c.addChild(new Text(theme.fg("dim", `  ${line}`), 1, 0));
		}
	}

	return c;
}
