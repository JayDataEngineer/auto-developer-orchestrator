/**
 * StatusBar — compact status line at the bottom.
 *
 * Uses inverse text so it stands out on all terminals.
 */

import React from "react";
import { Box, Text } from "ink";
import { usePuxStore } from "@pux/shared";
import { symbols } from "../theme.js";

interface StatusBarProps {
	model: string;
}

export function StatusBar({ model }: StatusBarProps) {
	const lastUsage = usePuxStore((s) => s.lastUsage);
	const contextMetrics = usePuxStore((s) => s.contextMetrics);
	const compacting = usePuxStore((s) => s.compacting);

	// Build a single status string
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
			<Text inverse>{` ${status} `}</Text>
		</Box>
	);
}
