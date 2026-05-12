/**
 * Sub-agent detail overlay — full conversation view for a single sub-agent.
 *
 * Renders the sub-agent's live conversation (thinking, text, tool calls)
 * in a scrollable overlay. Triggered by Ctrl+J from the main TUI.
 */

import { Container, Text } from "@mariozechner/pi-tui";
import type { Theme } from "../../../modes/interactive/theme/theme.js";
import type { SubAgentState, SubAgentEntry } from "../../../core/sub-agent-tracker.js";

const GLYPH_RUNNING = "\u25CF"; // ●
const GLYPH_SUCCESS = "\u2713"; // ✓
const GLYPH_ERROR = "\u2717";   // ✗

function trunc(s: string, max: number): string {
	return s.length <= max ? s : s.slice(0, max - 1) + "\u2026";
}

function fmtDuration(ms: number): string {
	if (ms < 1000) return `${ms}ms`;
	const s = Math.floor(ms / 1000);
	if (s < 60) return `${s}s`;
	return `${Math.floor(s / 60)}m${s % 60}s`;
}

export class SubAgentDetailOverlay extends Container {
	private state: SubAgentState;
	private theme: Theme;
	private scrollOffset = 0;
	private autoScroll = true;

	constructor(state: SubAgentState, theme: Theme) {
		super();
		this.state = state;
		this.theme = theme;
		this.rebuild();
	}

	update(state: SubAgentState): void {
		this.state = state;
		this.rebuild();
	}

	private rebuild(): void {
		this.clearChildren();
		const t = this.theme;
		const s = this.state;
		const elapsed = s.status === "running" && s.startedAt
			? fmtDuration(Date.now() - s.startedAt)
			: s.endedAt && s.startedAt
				? fmtDuration(s.endedAt - s.startedAt)
				: "";

		const icon = s.status === "running"
			? t.fg("accent", GLYPH_RUNNING)
			: s.status === "failed"
				? t.fg("error", GLYPH_ERROR)
				: t.fg("success", GLYPH_SUCCESS);

		// Header
		const header = `${icon} ${t.bold(s.agentName)}${t.fg("muted", ": " + trunc(s.task, 50))}${t.fg("dim", ` \u00B7 ${s.toolCount} tools \u00B7 ${elapsed}`)}${t.fg("dim", "          [ESC to close]")}`;
		this.addChild(new Text(header, 0, 0));
		this.addChild(new Text(t.fg("border", "\u2500".repeat(60)), 0, 0));

		// Conversation entries
		const entries = s.conversation;
		if (entries.length === 0) {
			this.addChild(new Text(t.fg("dim", "  Waiting for activity..."), 0, 0));
			return;
		}

		for (const entry of entries) {
			this.renderEntry(entry, t);
		}
	}

	private renderEntry(entry: SubAgentEntry, t: Theme): void {
		switch (entry.type) {
			case "thinking":
				if (entry.text) {
					const lines = this.wrapText(entry.text, 56);
					for (const line of lines.slice(-3)) {
						this.addChild(new Text(t.fg("dim", `  [thinking] ${line}`), 0, 0));
					}
				}
				break;

			case "text":
				if (entry.text) {
					const lines = this.wrapText(entry.text, 58);
					for (const line of lines) {
						this.addChild(new Text(`  ${line}`, 0, 0));
					}
				}
				break;

			case "tool_start": {
				const argsPreview = entry.toolArgs
					? trunc(this.stringifyArgs(entry.toolArgs), 40)
					: "";
				this.addChild(new Text(
					t.fg("accent", `  > ${entry.toolName || "tool"}`) +
					(argsPreview ? t.fg("dim", `(${argsPreview})`) : ""),
					0, 0,
				));
				break;
			}

			case "tool_end": {
				const resultText = this.extractResultText(entry.toolResult);
				if (resultText) {
					const lines = this.wrapText(resultText, 56);
					for (const line of lines.slice(0, 3)) {
						this.addChild(new Text(t.fg("dim", `    ${line}`), 0, 0));
					}
				}
				break;
			}

			case "tool_update": {
				if (entry.text) {
					const lines = this.wrapText(entry.text, 56);
					for (const line of lines.slice(0, 2)) {
						this.addChild(new Text(t.fg("dim", `    ${line}`), 0, 0));
					}
				}
				break;
			}
		}
	}

	private wrapText(text: string, maxWidth: number): string[] {
		const rawLines = text.split("\n");
		const result: string[] = [];
		for (const line of rawLines) {
			if (line.length <= maxWidth) {
				result.push(line);
			} else {
				for (let i = 0; i < line.length; i += maxWidth) {
					result.push(line.slice(i, i + maxWidth));
				}
			}
		}
		return result;
	}

	private stringifyArgs(args: any): string {
		if (!args) return "";
		try {
			const s = JSON.stringify(args);
			return s.length > 60 ? s.slice(0, 57) + "..." : s;
		} catch {
			return String(args).slice(0, 60);
		}
	}

	private extractResultText(result: any): string {
		if (!result) return "";
		if (typeof result === "string") return result.slice(0, 200);
		if (result.content) {
			if (typeof result.content === "string") return result.content.slice(0, 200);
			if (Array.isArray(result.content)) {
				return result.content
					.filter((c: any) => c.type === "text" && c.text)
					.map((c: any) => c.text)
					.join("\n")
					.slice(0, 200);
			}
		}
		if (result.text) return result.text.slice(0, 200);
		return JSON.stringify(result).slice(0, 200);
	}
}
