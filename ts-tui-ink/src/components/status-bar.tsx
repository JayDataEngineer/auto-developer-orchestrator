/**
 * StatusBar — bottom status bar with model and token usage.
 *
 * Left: model name. Right: token usage (in/out), context %.
 */

import React from "react";
import { Box, Text, useStdout } from "ink";
import { usePuxStore } from "@pux/shared";
import { symbols } from "../theme.js";

interface StatusBarProps {
	model: string;
	project: string;
}

export function StatusBar({ model }: StatusBarProps) {
	const lastUsage = usePuxStore((s) => s.lastUsage);
	const contextMetrics = usePuxStore((s) => s.contextMetrics);
	const compacting = usePuxStore((s) => s.compacting);
	const { stdout } = useStdout();
	const cols = stdout?.columns ?? 80;

	// Build left side: model name
	const left = model;

	// Build right side: token usage
	let right = "";
	if (lastUsage) {
		const inK = lastUsage.input > 1000 ? `${(lastUsage.input / 1000).toFixed(1)}k` : String(lastUsage.input);
		const outK = lastUsage.output > 1000 ? `${(lastUsage.output / 1000).toFixed(1)}k` : String(lastUsage.output);
		right = `in:${inK} out:${outK}`;
	}
	if (contextMetrics) {
		const pct = Math.round(contextMetrics.contextUtil * 100);
		right += ` ${symbols.dot} ctx:${pct}%`;
	}
	if (compacting) {
		right += ` ${symbols.dot} compacting`;
	}

	// Pad to fill width
	const content = ` ${left} ${right} `;
	const totalLen = stripAnsiLen(content);
	const padding = Math.max(0, cols - totalLen);
	const padded = right
		? ` ${left}${" ".repeat(padding)}${right} `
		: ` ${left} `;

	return (
		<Box>
			<Text dimColor>{padded}</Text>
		</Box>
	);
}

/** Get visible length of string (excluding ANSI codes) */
function stripAnsiLen(str: string): number {
	// eslint-disable-next-line no-control-regex
	return str.replace(/\x1b\[[0-9;]*m/g, "").length;
}
