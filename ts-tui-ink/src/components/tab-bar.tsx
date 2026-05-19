/**
 * TabBar — view switching tabs at the top of the TUI.
 *
 * Shows [Chat] [Agents (N)] [Tools] [Files] with active highlight.
 * Keybindings: Ctrl+T cycles views.
 */

import React from "react";
import { Box, Text } from "ink";
import { usePuxStore, type TuiView } from "@pux/shared";
import { useAuiState } from "@assistant-ui/react-ink";
import { useColors, symbols } from "../theme.js";
import { useTerminalSize } from "../use-terminal-size.js";

const VIEWS: { key: TuiView; label: string }[] = [
	{ key: "chat", label: "Chat" },
	{ key: "agents", label: "Agents" },
	{ key: "tools", label: "Tools" },
	{ key: "files", label: "Files" },
	{ key: "conversations", label: "History" },
];

export function TabBar() {
	const activeView = usePuxStore((s) => s.activeTuiView);
	const agents = usePuxStore((s) => s.agents);
	const isRunning = useAuiState((s) => s.thread.isRunning);
	const { cols } = useTerminalSize();

	// Count running agents
	const runningAgents = [...agents.values()].filter((a) => a.status === "running").length;
	const colors = useColors();

	return (
		<Box>
			{VIEWS.map((v, i) => {
				const isActive = activeView === v.key;
				let label = v.label;

				// Add count badges
				if (v.key === "agents" && agents.size > 0) {
					label = `${v.label}(${agents.size})`;
				}

				return (
					<React.Fragment key={v.key}>
						<Text>
							{isActive ? (
								<Text bold color={colors.brand}>
									{" "}{symbols.arrow} {label}{" "}
								</Text>
							) : (
								<Text dimColor>
									{"   "}{label}{" "}
								</Text>
							)}
						</Text>
					</React.Fragment>
				);
			})}
			{/* Right side: status hint */}
			<Text dimColor>
				{" ".repeat(Math.max(0, cols - 60))}
				{isRunning ? "running" : ""}
				{runningAgents > 0 ? ` ${symbols.dot} ${runningAgents} agents` : ""}
				{" "}
				<Text dimColor>Ctrl+T switch</Text>
			</Text>
		</Box>
	);
}
