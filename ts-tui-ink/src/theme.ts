/**
 * Pux TUI theme — colors and figures inspired by Claude Code CLI.
 *
 * Ink uses chalk named colors: magenta, cyan, green, red, yellow, etc.
 * Also supports #hex, rgb(r,g,b), ansi256(n).
 */

// ── Figures ──

export const BLACK_CIRCLE = process.platform === "darwin" ? "⏺" : "●";
export const BULLET = "∙";
export const BLOCKQUOTE_BAR = "▎";

// ── Colors ──
// These must be valid chalk color names or #hex values.
// Ink's colorize() checks: named chalk colors, #hex, ansi256(n), rgb(r,g,b)

export const colors = {
	// Brand
	brand: "magenta",

	// Roles
	user: "greenBright",
	assistant: "blue",

	// Status
	success: "green",
	error: "red",
	warning: "yellow",
	running: "cyan",

	// Text
	text: "white",
	textDim: "white",
	textMuted: "gray",
	subtle: "gray",
} as const;

// ── Symbols ──

export const symbols = {
	/** Tool status: running */
	toolRunning: "○",
	/** Tool status: done */
	toolDone: "●",
	/** Tool status: error */
	toolError: "✕",
	/** Separator */
	dot: "·",
	/** Arrow */
	arrow: "→",
	/** Check */
	check: "✓",
	/** Cross */
	cross: "✗",
} as const;
