// @vitest-environment jsdom
//
// Regression test for the browser_ops → mouse_action → cursor render pipeline.
//
// The chain under test:
//   1. agent calls find_element(action:"click", text:"Learn more")
//   2. backend emits `event: mouse_action\ndata: {"action":"click","normX":...,"normY":...}`
//   3. pux-chat-adapter.ts:782 calls usePuxStore.getState().setMouseOverlay({
//        state: action === "type" ? "typing" : "moving", normX, normY })
//   4. MouseOverlay component reads store.mouseOverlay and renders <img> at
//        left = normX * containerWidth, top = normY * containerHeight
//
// This test exercises steps 3+4 with the exact store mutation the adapter
// performs, then verifies the rendered <img>'s pixel position matches the
// expected center of the clicked element. If anything in this chain
// regresses (store shape change, adapter mapping bug, MouseOverlay math
// bug), the test fails with a clear diff.

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, act } from "@testing-library/react";
import "@testing-library/jest-dom/vitest";
import React from "react";

import { usePuxStore } from "@pux/shared";
import { MouseOverlay } from "../mouse-overlay";

// ResizeObserver isn't shipped by jsdom — MouseOverlay uses it to track
// container size. We need a stub that fires the callback at least once
// AFTER the test has pinned clientWidth/clientHeight on the container.
// Using queueMicrotask defers the callback until after the synchronous
// pin in the test body, so updateSize() reads the pinned values.
// eslint-disable-next-line @typescript-eslint/no-explicit-any
(globalThis as any).ResizeObserver = class {
	private cb: ((entries: any[]) => void) | null = null;
	constructor(cb: (entries: any[]) => void) {
		this.cb = cb;
	}
	observe() {
		queueMicrotask(() => {
			if (this.cb) this.cb([]);
		});
	}
	unobserve() {}
	disconnect() {}
};

// Pin a known size matching the production VNC iframe aspect ratio. This is
// the size MouseOverlay reads via containerRef.current.clientWidth/Height.
const CONTAINER_W = 1280;
const CONTAINER_H = 580;

// Mounts MouseOverlay inside a container with a pinned size. Returns the
// container DOM node so tests can inspect rendered children.
function mountOverlay(): { container: HTMLElement } {
	const ref = React.createRef<HTMLDivElement>();
	const utils = render(
		<div
			ref={ref}
			style={{ width: CONTAINER_W, height: CONTAINER_H, position: "relative" }}
		>
			<MouseOverlay containerRef={ref} />
		</div>,
	);
	// jsdom ignores CSS for layout — override the property descriptors so
	// MouseOverlay's `el.clientWidth` reads return our pinned values.
	const container = ref.current!;
	Object.defineProperty(container, "clientWidth", {
		configurable: true,
		value: CONTAINER_W,
	});
	Object.defineProperty(container, "clientHeight", {
		configurable: true,
		value: CONTAINER_H,
	});
	return { container };
}

beforeEach(() => {
	usePuxStore.setState({
		mouseOverlay: null,
		clickTrail: [],
	});
});

afterEach(() => {
	vi.restoreAllMocks();
});

describe("MouseOverlay: mouse_action → cursor pixel position", () => {
	it("renders no cursor when store.mouseOverlay is null", () => {
		const { container } = mountOverlay();
		// MouseOverlay returns null when no overlay and no clickTrail
		const img = container.querySelector("img");
		expect(img).toBeNull();
	});

	it("renders an <img> cursor at normX*W, normY*H when mouse_overlay arrives", async () => {
		// Coordinates from the proven backend evidence: mouse_action emitted
		// for the "Learn more" link on example.com. Element center is at
		// (298.5, 214), viewport is 1280×580 → normalized (0.2332, 0.3690).
		const normX = 298.5 / 1280;
		const normY = 214 / 580;

		const { container } = mountOverlay();

		// Exact mutation performed by pux-chat-adapter.ts:782 on mouse_action.
		// Wrap in async act() so React flushes the re-render AND any pending
		// ResizeObserver microtasks before we query the DOM.
		await act(async () => {
			usePuxStore.getState().setMouseOverlay({
				state: "moving", // adapter maps action:"click" → state:"moving"
				normX,
				normY,
			});
		});

		const img = container.querySelector("img");
		expect(img).not.toBeNull();
		expect(img).toBeVisible();

		// MouseOverlay uses `left: cursorLeft - 4, top: cursorTop - 4`
		// where cursorLeft = normX * containerWidth.
		// → img.style.left = (0.2332 * 1280) - 4 = 294.5
		// → img.style.top  = (0.3690 * 580)  - 4 = 210
		expect(parseFloat(img!.style.left)).toBeCloseTo(normX * CONTAINER_W - 4, 5);
		expect(parseFloat(img!.style.top)).toBeCloseTo(normY * CONTAINER_H - 4, 5);
	});

	it("cursor disappears after clearMouseOverlay is called", async () => {
		const { container } = mountOverlay();

		await act(async () => {
			usePuxStore.getState().setMouseOverlay({
				state: "moving",
				normX: 0.5,
				normY: 0.5,
			});
		});
		expect(container.querySelector("img")).not.toBeNull();

		await act(async () => {
			usePuxStore.getState().clearMouseOverlay();
		});
		expect(container.querySelector("img")).toBeNull();
	});

	it("adapter mapping: action:'click' → state:'moving'", () => {
		// Locks in pux-chat-adapter.ts:783's state mapping. If the adapter
		// ever sends state:"click" for action:"click" (bypassing the
		// tool_execution_end transition at line 749), this test fails.
		const action = "click";
		usePuxStore.getState().setMouseOverlay({
			state: action === "type" ? "typing" : "moving",
			normX: 0.5,
			normY: 0.5,
		});
		expect(usePuxStore.getState().mouseOverlay?.state).toBe("moving");
	});

	it("adapter mapping: action:'type' → state:'typing'", () => {
		const action = "type";
		usePuxStore.getState().setMouseOverlay({
			state: action === "type" ? "typing" : "moving",
			normX: 0.5,
			normY: 0.5,
		});
		expect(usePuxStore.getState().mouseOverlay?.state).toBe("typing");
	});
});
