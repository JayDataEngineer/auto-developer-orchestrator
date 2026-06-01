/**
 * Pux TUI theme — colors and figures inspired by Claude Code CLI.
 *
 * Ink uses chalk named colors: magenta, cyan, green, red, yellow, etc.
 * Also supports #hex, rgb(r,g,b), ansi256(n).
 *
 * Themed via useColors() hook — reactive to the Zustand store's theme field.
 * Static `colors` export provides the default theme for legacy usage.
 */

import React, { createContext, useContext, useMemo } from "react";
import { usePuxStore } from "@pux/shared";

// ── Figures ──

export const BLACK_CIRCLE = process.platform === "darwin" ? "⏺" : "●";
export const BULLET = "∙";
export const BLOCKQUOTE_BAR = "▎";

// ── Theme Types ──

export interface ThemeColors {
	brand: string;
	user: string;
	assistant: string;
	success: string;
	error: string;
	warning: string;
	running: string;
	text: string;
	textDim: string;
	textMuted: string;
	subtle: string;
}

interface ThemeDef {
	id: string;
	name: string;
	desc: string;
	colors: ThemeColors;
}

// ── Theme Definitions ──

const themeDefs: ThemeDef[] = [
	{
		id: "default",
		name: "Default",
		desc: "Claude Code-inspired palette",
		colors: {
			brand: "#d77757",       // Claude orange
			user: "#4eba65",        // bright green
			assistant: "#b1b9f9",   // light blue-purple
			success: "#4eba65",     // bright green
			error: "#ff6b80",       // bright red
			warning: "#e0af68",     // amber
			running: "#73daca",     // teal
			text: "#ffffff",        // white
			textDim: "#999999",     // light gray (was "gray" — too dark)
			textMuted: "#6a737d",   // medium gray (was "gray" — too dark)
			subtle: "#505050",      // dark gray
		},
	},
	{
		id: "dark",
		name: "Dark",
		desc: "Green brand, lower contrast",
		colors: {
			brand: "#50fa7b",
			user: "#8aff80",
			assistant: "#6272a4",
			success: "#50fa7b",
			error: "#ff5555",
			warning: "#f1fa8c",
			running: "#8be9fd",
			text: "#f8f8f2",
			textDim: "#6272a4",
			textMuted: "#44475a",
			subtle: "#6272a4",
		},
	},
	{
		id: "light",
		name: "Light",
		desc: "Blue brand, light background friendly",
		colors: {
			brand: "#0366d6",
			user: "#22863a",
			assistant: "#586069",
			success: "#28a745",
			error: "#d73a49",
			warning: "#dbab09",
			running: "#0366d6",
			text: "#24292e",
			textDim: "#586069",
			textMuted: "#d1d5da",
			subtle: "#6a737d",
		},
	},
	{
		id: "catppuccin",
		name: "Catppuccin",
		desc: "Catppuccin Mocha palette",
		colors: {
			brand: "#cba6f7",
			user: "#a6e3a1",
			assistant: "#89b4fa",
			success: "#a6e3a1",
			error: "#f38ba8",
			warning: "#f9e2af",
			running: "#89dceb",
			text: "#cdd6f4",
			textDim: "#a6adc8",
			textMuted: "#45475a",
			subtle: "#6c7086",
		},
	},
	{
		id: "dracula",
		name: "Dracula",
		desc: "Dracula palette",
		colors: {
			brand: "#ff79c6",
			user: "#50fa7b",
			assistant: "#8be9fd",
			success: "#50fa7b",
			error: "#ff5555",
			warning: "#f1fa8c",
			running: "#8be9fd",
			text: "#f8f8f2",
			textDim: "#6272a4",
			textMuted: "#44475a",
			subtle: "#6272a4",
		},
	},
	{
		id: "mono",
		name: "Mono",
		desc: "All gray, monochrome palette",
		colors: {
			brand: "#b0b0b0",
			user: "#d0d0d0",
			assistant: "#a0a0a0",
			success: "#a0a0a0",
			error: "#8a8a8a",
			warning: "#c0c0c0",
			running: "#b0b0b0",
			text: "#e0e0e0",
			textDim: "#909090",
			textMuted: "#555555",
			subtle: "#707070",
		},
	},
	{
		id: "tokyonight",
		name: "Tokyo Night",
		desc: "Tokyo Night Storm palette",
		colors: {
			brand: "#bb9af7",
			user: "#9ece6a",
			assistant: "#7aa2f7",
			success: "#9ece6a",
			error: "#f7768e",
			warning: "#e0af68",
			running: "#73daca",
			text: "#c0caf5",
			textDim: "#a9b1d6",
			textMuted: "#3b4261",
			subtle: "#565f89",
		},
	},
];

// ── Theme Map ──

export const themes: Record<string, { name: string; desc: string; colors: ThemeColors }> = {};
for (const t of themeDefs) {
	themes[t.id] = { name: t.name, desc: t.desc, colors: t.colors };
}

export const themeList = themeDefs.map((t) => ({ id: t.id, name: t.name, desc: t.desc }));

// ── Default (legacy) colors ──

export const colors = themes.default.colors;

// ── Symbols (not themed) ──

export const symbols = {
	toolRunning: "○",
	toolDone: "●",
	toolError: "✕",
	dot: "·",
	arrow: "→",
	check: "✓",
	cross: "✗",
} as const;

// ── Theme Context ──

const ThemeCtx = createContext<ThemeColors>(colors);

export function ThemeProvider({ children }: { children: React.ReactNode }) {
	const themeId = usePuxStore((s) => s.theme);
	const value = useMemo(() => themes[themeId]?.colors ?? colors, [themeId]);
	return <ThemeCtx.Provider value={value}>{children}</ThemeCtx.Provider>;
}

export function useColors(): ThemeColors {
	return useContext(ThemeCtx);
}
