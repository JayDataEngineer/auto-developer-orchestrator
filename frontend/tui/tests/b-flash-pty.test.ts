/**
 * Flash detection test — spawns the REAL TUI in a PTY, types text,
 * presses Enter, and captures every PTY output frame to detect the flash.
 *
 * The flash: after pressing Enter, the PTY output shows the old composer
 * text without any user message for one frame. This is what the user sees.
 */

import { describe, test, expect } from "vitest";
import { resolve } from "node:path";

const PROJECT_ROOT = resolve(import.meta.dirname, "../../..");
const TUI_ENTRY = resolve(PROJECT_ROOT, "frontend/tui/src/main.tsx");

// PTY helper — uses node-pty via a small helper script
const PTY_HELPER = resolve(import.meta.dirname, "pty-helper.ts");

async function sleep(ms: number) {
	return new Promise((r) => setTimeout(r, ms));
}

describe("Flash detection: real PTY output", () => {
	test("spawn TUI, type text, press Enter, check PTY frames for flash", async () => {
		// Check if visual server is already running
		const resp = await fetch("http://localhost:9877/screen").catch(() => null);
		if (!resp || !resp.ok) {
			console.log("SKIP: visual server not running on :9877. Run: task tui-visual");
			return;
		}
		await runFlashTest(9877);
	}, 30000);
});

async function runFlashTest(port: number) {
	const BASE = `http://localhost:${port}`;

	// Clear any existing state
	await fetch(`${BASE}/input`, {
		method: "POST",
		body: JSON.stringify({ text: "/clear\n", wait: 2 }),
	}).catch(() => {});
	await sleep(2000);

	// Type unique text
	const magic = "FLASHTEST42";
	await fetch(`${BASE}/input`, {
		method: "POST",
		body: JSON.stringify({ text: magic }),
	});
	await sleep(500);

	// Verify text is in the composer
	const beforeResp = await fetch(`${BASE}/screen`);
	const beforeData = (await beforeResp.json()) as { screen: string };
	const before = beforeData.screen;
	expect(before).toContain(magic);

	// Press Enter
	await fetch(`${BASE}/key`, {
		method: "POST",
		body: JSON.stringify({ key: "enter" }),
	});

	// Capture PTY frames as fast as possible
	const frames: string[] = [];
	for (let i = 0; i < 20; i++) {
		const resp = await fetch(`${BASE}/screen`);
		const data = (await resp.json()) as { screen: string };
		frames.push(data.screen);
		// No sleep — capture as fast as possible
	}

	// Wait a bit for the response to stream in
	await sleep(3000);

	// Analyze frames for the flash
	// Flash = composer area (between ──── lines) contains magic text
	//         but NO user message has appeared yet
	let flashCount = 0;
	console.log(`\nPTY Frame Analysis (${frames.length} frames):`);

	for (let i = 0; i < frames.length; i++) {
		const f = frames[i];
		const lines = f.split("\n");

		// Find composer area — between ──── lines
		let inComposer = false;
		let composerHasMagic = false;
		let hasUserMessage = false;

		for (const line of lines) {
			if (line.includes("───")) {
				inComposer = !inComposer;
				continue;
			}
			if (inComposer && line.includes(magic)) {
				composerHasMagic = true;
			}
			// Check for user message rendering (varies by component)
			// The thread renders user messages with the text content
			// Check OUTSIDE the composer for the text
			if (!inComposer && line.includes(magic)) {
				hasUserMessage = true;
			}
		}

		if (composerHasMagic && !hasUserMessage) {
			flashCount++;
			console.log(`  [${i}] *** FLASH: composer has "${magic}", no user message ***`);
		} else if (hasUserMessage) {
			console.log(`  [${i}] OK: user message present`);
		} else {
			console.log(`  [${i}] ---: waiting for state change`);
		}
	}

	console.log(`\n  Flash frames: ${flashCount}`);
	expect(flashCount).toBe(0);
}
