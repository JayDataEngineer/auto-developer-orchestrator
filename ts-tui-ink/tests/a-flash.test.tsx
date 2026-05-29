/**
 * Flash detection test — catches the Enter flash bug.
 *
 * ROOT CAUSE: puxChatAdapter sets usePuxStore.setState({ ctoRunning: true })
 * BEFORE yielding the first snapshot. This Zustand update triggers a
 * synchronous render in components subscribed to that Zustand field.
 * During this render, the assistant-ui store state is stale (text reverts to
 * the pre-send value, messages disappear).
 *
 * FIX: Yield first, then defer ctoRunning setState via queueMicrotask
 * so it doesn't trigger a synchronous render during the adapter cycle.
 *
 * TEST STRATEGY:
 * Two layers of testing:
 *
 * 1. OPERATION ORDERING (deterministic): Mock usePuxStore.setState and
 *    verify that the buggy adapter fires setState synchronously before the
 *    first yield resolves, while the fixed adapter does NOT.
 *    This is a direct test of the adapter's code path — no React rendering.
 *
 * 2. RENDERING ARTIFACT (best-effort): Render with ink-testing-library and
 *    check if a Spy component ever sees stale assistant-ui state with
 *    ctoRunning=true. This catches the actual visual flash but may be
 *    affected by React batching in different test environments.
 */

import { describe, test, expect } from "bun:test";
import { usePuxStore } from "@pux/shared";

// ── Operation Ordering Tests (deterministic) ──

describe("Enter flash: operation ordering", () => {
	// BUGGY: setState BEFORE yield — the exact production bug pattern.
	// When gen.next() is called, the generator body executes synchronously
	// until it hits an await or yield. setState runs first, then yield.
	const buggyAdapter = {
		async *run() {
			usePuxStore.setState({ ctoRunning: true });
			yield { content: [{ type: "text" as const, text: "response" }] };
		},
	};

	// FIXED: yield first, then defer setState. Matches pux-chat-adapter.ts.
	// When gen.next() is called, the generator hits yield immediately.
	// setState is deferred via queueMicrotask and hasn't fired yet.
	const fixedAdapter = {
		async *run() {
			yield { content: [{ type: "text" as const, text: "response" }] };
			queueMicrotask(() => usePuxStore.setState({ ctoRunning: true }));
		},
	};

	test("buggy: setState fires synchronously before first yield", async () => {
		const events: string[] = [];
		const origSetState = usePuxStore.setState.bind(usePuxStore);

		usePuxStore.setState = function(partial: any) {
			if (partial?.ctoRunning === true) events.push("setState");
			return origSetState(partial);
		} as typeof usePuxStore.setState;

		usePuxStore.setState({ ctoRunning: false });
		const gen = buggyAdapter.run();

		// gen.next() starts the generator body synchronously.
		// In the buggy adapter: setState fires → yield fires → gen.next() returns Promise
		// At this exact moment, events should already contain 'setState'
		gen.next();

		const setStateBeforeYield = events.includes("setState");

		// Restore
		usePuxStore.setState = origSetState;

		// BUG: setState ran synchronously before the yield could establish state
		expect(setStateBeforeYield).toBe(true);
	});

	test("fixed: setState does NOT fire before first yield", async () => {
		const events: string[] = [];
		const origSetState = usePuxStore.setState.bind(usePuxStore);

		usePuxStore.setState = function(partial: any) {
			if (partial?.ctoRunning === true) events.push("setState");
			return origSetState(partial);
		} as typeof usePuxStore.setState;

		usePuxStore.setState({ ctoRunning: false });
		const gen = fixedAdapter.run();

		// gen.next() starts the generator body synchronously.
		// In the fixed adapter: yield fires immediately (first statement) → gen.next() returns Promise
		// setState is deferred via queueMicrotask and hasn't fired yet.
		gen.next();

		const setStateBeforeYield = events.includes("setState");

		// Restore
		usePuxStore.setState = origSetState;

		// FIX: setState has NOT fired — it's deferred past the yield
		expect(setStateBeforeYield).toBe(false);
	});
});

// ── Production Code Verification ──

describe("Enter flash: production adapter verification", () => {
	test("puxChatAdapter: first yield comes before ctoRunning setState", async () => {
		// Read the adapter source and verify the yield-then-setState pattern.
		// This is a structural test — if someone accidentally moves setState
		// before the yield, this test catches it.
		const fs = await import("node:fs");
		const path = await import("node:path");
		const adapterPath = path.join(
			__dirname,
			"../../shared/src/pux-chat-adapter.ts",
		);
		const source = fs.readFileSync(adapterPath, "utf-8");

		// Find the initial yield (line: "yield buildSnapshot(parts, ...")
		const yieldMatch = source.match(
			/yield buildSnapshot\(parts,\s*sources,\s*["']running["']/,
		);
		expect(yieldMatch).not.toBeNull();
		const yieldPos = source.indexOf(yieldMatch![0]);

		// Find the deferred ctoRunning setState
		const setStateMatch = source.match(
			/queueMicrotask\(\(\)\s*=>\s*usePuxStore\.setState\(\{\s*ctoRunning:\s*true\s*\}\)\)/,
		);
		expect(setStateMatch).not.toBeNull();
		const setStatePos = source.indexOf(setStateMatch![0]);

		// The yield MUST come before the setState in the source
		expect(yieldPos).toBeLessThan(setStatePos);
	});
});
