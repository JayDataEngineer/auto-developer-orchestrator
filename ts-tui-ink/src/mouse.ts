/**
 * SGR mouse tracking for Ink TUI.
 *
 * Enables DECSET modes 1000 + 1006 (press/release, SGR coordinates).
 * Does NOT enable 1002 (drag) — preserves terminal native text selection.
 * Users can hold Shift to bypass mouse reporting entirely.
 *
 * Parses ESC[<btn;col;rowM/m sequences from stdin, strips them from the
 * input stream before Ink sees them, and fires typed callbacks.
 *
 * Wheel events are converted to arrow key sequences injected back into the
 * stream so Ink processes them as keyboard events (scroll wheel works).
 */

// ── DECSET sequences ──

const ENABLE_MOUSE = "\x1b[?1000h\x1b[?1006h";
const DISABLE_MOUSE = "\x1b[?1006l\x1b[?1000l";

// SGR mouse sequence regex: ESC [ < btn ; col ; row M/m
const SGR_MOUSE_RE = /\x1b\[<(\d+);(\d+);(\d+)([Mm])/g;

export interface ParsedMouse {
	button: number;
	action: "press" | "release";
	col: number;
	row: number;
	isWheel: boolean;
	wheelDir?: "up" | "down";
}

export type MouseCallback = (evt: ParsedMouse) => void;

let callbacks: MouseCallback[] = [];
let tracked = false;

export function isMouseTracking(): boolean {
	return tracked;
}

export function initMouseTracking(): void {
	if (tracked) return;
	process.stdout.write(ENABLE_MOUSE);
	tracked = true;
}

export function disableMouseTracking(): void {
	if (!tracked) return;
	process.stdout.write(DISABLE_MOUSE);
	tracked = false;
}

export function onMouseEvent(cb: MouseCallback): () => void {
	callbacks.push(cb);
	return () => {
		callbacks = callbacks.filter((c) => c !== cb);
	};
}

export function clearMouseCallbacks(): void {
	callbacks = [];
}

// ── React hook ──

import { useEffect, useState } from "react";

/**
 * Subscribe to mouse events. The callback is invoked for every
 * non-wheel press/release.
 */
export function useMouseEvent(cb: MouseCallback): void {
	useEffect(() => {
		return onMouseEvent(cb);
	}, [cb]);
}

/**
 * Track the most recent mouse click position (row, col).
 * Returns null if no click has been received yet.
 */
export function useLastClick(): { col: number; row: number } | null {
	const [pos, setPos] = useState<{ col: number; row: number } | null>(null);
	useEffect(() => {
		return onMouseEvent((evt) => {
			if (evt.action === "press") {
				setPos({ col: evt.col, row: evt.row });
			}
		});
	}, []);
	return pos;
}

/**
 * Parse SGR mouse sequences from a string. Returns the input with
 * mouse sequences removed. Wheel events are converted to arrow key
 * sequences so Ink sees them as keyboard scroll events.
 * Non-wheel events fire callbacks for application handling.
 */
export function filterAndEmitMouseEvents(input: string): string {
	if (!tracked || !SGR_MOUSE_RE.test(input)) return input;

	SGR_MOUSE_RE.lastIndex = 0;

	let result = "";
	let lastIndex = 0;

	for (const match of input.matchAll(SGR_MOUSE_RE)) {
		result += input.slice(lastIndex, match.index);

		const btn = parseInt(match[1], 10);
		const col = parseInt(match[2], 10);
		const row = parseInt(match[3], 10);
		const isPress = match[4] === "M";

		if (btn & 0x40) {
			// Wheel event → inject arrow key so Ink sees a standard scroll
			const dir = (btn & 0x03) === 0 ? "up" : "down";
			result += dir === "up" ? "\x1b[A" : "\x1b[B";
		} else if (col > 0 && row > 0) {
			// Non-wheel click/drag event
			const evt: ParsedMouse = {
				button: btn & 0x03,
				action: isPress ? "press" : "release",
				col,
				row,
				isWheel: false,
			};
			for (const cb of callbacks) {
				try { cb(evt); } catch { /* don't break other callbacks */ }
			}
		}

		lastIndex = match.index + match[0].length;
	}

	result += input.slice(lastIndex);
	return result;
}
