/**
 * StatusBar — bottom status bar with project info and token usage.
 */

import React from "react";
import { Box, Text } from "ink";
import { usePuxStore } from "@pux/shared";
import { symbols, BLACK_CIRCLE } from "../theme.js";

interface StatusBarProps {
	model: string;
	project: string;
}

export function StatusBar({ model, project }: StatusBarProps) {
	const lastUsage = usePuxStore((s) => s.lastUsage);
	const contextMetrics = usePuxStore((s) => s.contextMetrics);
	const compacting = usePuxStore((s) => s.compacting);

	let status = model;
	if (lastUsage) {
		const inK = lastUsage.input > 1000 ? `${(lastUsage.input / 1000).toFixed(1)}k` : String(lastUsage.input);
		const outK = lastUsage.output > 1000 ? `${(lastUsage.output / 1000).toFixed(1)}k` : String(lastUsage.output);
		status += ` ${symbols.dot} in:${inK} out:${outK}`;
	}
	if (contextMetrics) {
		const pct = Math.round(contextMetrics.contextUtil * 100);
		status += ` ${symbols.dot} ctx:${pct}%`;
	}
	if (compacting) {
		status += ` ${symbols.dot} compacting`;
	}

	return (
		<Box paddingX={1}>
			<Text dimColor>{` ${BLACK_CIRCLE} ${project} ${symbols.dot} ${status} `}</Text>
		</Box>
	);
}
