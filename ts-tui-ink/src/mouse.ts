/**
 * Mouse scroll for Ink TUI.
 *
 * Uses DECSET mode 1007 (Alternate Scroll Mode) which tells the terminal
 * to convert scroll wheel events into arrow key sequences. This works without
 * capturing clicks or interfering with terminal-native text selection.
 *
 * Mode 1000 (normal mouse tracking) was previously used but it captures click
 * events, which breaks click-and-drag text selection in the terminal.
 */

// ── DECSET sequences ──

const ENABLE_ALT_SCROLL = "\x1b[?1007h";
const DISABLE_ALT_SCROLL = "\x1b[?1007l";

let tracked = false;

export function isMouseTracking(): boolean {
	return tracked;
}

export function initMouseTracking(): void {
	if (tracked) return;
	process.stdout.write(ENABLE_ALT_SCROLL);
	tracked = true;
}

export function disableMouseTracking(): void {
	if (!tracked) return;
	process.stdout.write(DISABLE_ALT_SCROLL);
	tracked = false;
}

/**
 * No-op — kept for import compatibility in main.tsx stdin patch.
 * With mode 1007 the terminal handles scroll-to-arrow conversion natively,
 * so there are no SGR mouse sequences to filter.
 */
export function filterAndEmitMouseEvents(input: string): string {
	return input;
}
