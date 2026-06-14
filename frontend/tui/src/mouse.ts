/**
 * Mouse support for Ink TUI.
 *
 * Enables SGR mouse mode (1006) + button tracking (1002).
 * Parses click events from stdin and dispatches to handlers.
 *
 * Tradeoff: mouse tracking breaks terminal-native text selection.
 * This is the same tradeoff Claude Code and OpenCode make.
 * Users get clickable UI elements in exchange.
 *
 * SGR mouse format:
 *   Press:   ESC[<button;col;rowM
 *   Release: ESC[<button;col;rowm
 *
 *   button 0 = left, 1 = middle, 2 = right
 *   col/row are 1-based terminal coordinates
 */

let tracked = false;
let clickHandler: ((col: number, row: number) => void) | null = null;

export function isMouseTracking(): boolean {
	return tracked;
}

export function initMouseTracking(): void {
	if (tracked) return;
	tracked = true;
	// Enable button-event tracking (1002) + SGR mouse format (1006)
	// 1002 captures press/release/drag (not motion) — less noise than 1003
	process.stdout.write("\x1b[?1002h\x1b[?1006h");
}

export function disableMouseTracking(): void {
	if (!tracked) return;
	tracked = false;
	process.stdout.write("\x1b[?1002l\x1b[?1006l");
}

/** Register a handler for mouse clicks. Only left-clicks (button 0). */
export function onClick(handler: ((col: number, row: number) => void) | null): void {
	clickHandler = handler;
}

// SGR mouse regex: ESC[<button;col;rowM (press) or m (release)
const SGR_MOUSE_RE = /\x1b\[<(\d+);(\d+);(\d+)([Mm])/g;

/**
 * Parse stdin for SGR mouse sequences. Extracts click events and
 * removes the escape sequences so Ink doesn't choke on them.
 * Returns the filtered stdin (mouse sequences stripped).
 */
export function filterAndEmitMouseEvents(input: string): string {
	if (!tracked) return input;

	let hasMouse = false;
	let result = input;

	// Check for any SGR mouse sequences
	if (input.includes("\x1b[")) {
		SGR_MOUSE_RE.lastIndex = 0;
		const matches = [...input.matchAll(SGR_MOUSE_RE)];
		if (matches.length > 0) {
			hasMouse = true;
			for (const m of matches) {
				const button = parseInt(m[1], 10);
				const col = parseInt(m[2], 10);
				const row = parseInt(m[3], 10);
				const isPress = m[4] === "M";

				// Only handle left-click press (button 0, 2, or 32-34 for SGR)
				// SGR button encoding: 0=left, 1=middle, 2=right, 32+scroll
				if (isPress && (button === 0 || button === 2) && clickHandler) {
					clickHandler(col, row);
				}
			}
			// Strip all mouse sequences from input
			result = input.replace(SGR_MOUSE_RE, "");
		}
	}

	return result;
}
