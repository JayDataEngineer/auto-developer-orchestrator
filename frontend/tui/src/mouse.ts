/**
 * Mouse tracking intentionally disabled.
 *
 * Mouse mode (1000/1002/1006) captures scroll wheel events, breaking
 * terminal-native scrollback. Users can't scroll up to see history.
 *
 * Ctrl+P toggles thinking blocks instead.
 */

let tracked = false;

export function isMouseTracking(): boolean {
	return tracked;
}

export function initMouseTracking(): void {
	tracked = true;
}

export function disableMouseTracking(): void {
	tracked = false;
}

export function onClick(_handler: ((col: number, row: number) => void) | null): void {
	// No-op — mouse tracking disabled
}

export function filterAndEmitMouseEvents(input: string): string {
	return input;
}
