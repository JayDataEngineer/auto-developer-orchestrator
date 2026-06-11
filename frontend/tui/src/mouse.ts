/**
 * Mouse scroll for Ink TUI.
 *
 * Intentionally does NOT enable any mouse tracking mode.
 *
 * - Mode 1000 captures clicks → breaks text selection
 * - Mode 1007 (Alternate Scroll) converts wheel to Up/Down arrows → but those
 *   get consumed by Ink's useInput handlers (VimInput, overlays), preventing
 *   terminal-native scrollback from working
 *
 * Instead, we rely on Ink's <Static> component graduating old messages to
 * terminal scrollback, where the terminal's native scroll wheel works.
 * No DECSET modes needed.
 */

let tracked = false;

export function isMouseTracking(): boolean {
	return tracked;
}

export function initMouseTracking(): void {
	// No-op: don't enable any mouse tracking mode.
	// Scroll works via Ink <Static> + terminal-native scrollback.
	tracked = true;
}

export function disableMouseTracking(): void {
	tracked = false;
}

/**
 * No-op — kept for import compatibility in main.tsx stdin patch.
 */
export function filterAndEmitMouseEvents(input: string): string {
	return input;
}
