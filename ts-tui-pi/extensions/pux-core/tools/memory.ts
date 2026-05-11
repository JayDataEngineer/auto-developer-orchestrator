import { Container, Text } from "@mariozechner/pi-tui";
import type { Theme } from "../../../src/modes/interactive/theme/theme.js";

/**
 * Custom renderCall for memory / save_memory tools.
 * Shows the memory key being accessed.
 */
export function renderMemoryCall(
	args: { key?: string; action?: string },
	theme: Theme,
): Container {
	const c = new Container();
	const key = args.key || args.action || "";
	const dot = theme.fg("warning", "●");
	const label = theme.fg("toolTitle", theme.bold("memory"));

	c.addChild(new Text(`${dot} ${label}${key ? ` ${theme.fg("dim", "·")} ${key}` : ""}`, 1, 0));
	return c;
}
