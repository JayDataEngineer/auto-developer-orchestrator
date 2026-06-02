/**
 * Flash detection test — catches the Enter flash by checking
 * what is ACTUALLY RENDERED on screen.
 *
 * The flash: after pressing Enter, the first rendered frame still shows
 * the old composer text without any messages. The user sees the typed
 * text persist for one frame before it clears and messages appear.
 */

import { describe, test, expect } from "bun:test";
import React from "react";
import { Text, Box } from "ink";
import { render } from "ink-testing-library";
import {
	useLocalRuntime,
	AssistantRuntimeProvider,
	ComposerPrimitive,
	ThreadPrimitive,
	useAuiState,
	useAui,
} from "@assistant-ui/react-ink";
import { usePuxStore } from "@pux/shared";

const wait = (ms: number) => new Promise((r) => setTimeout(r, ms));

const simpleAdapter = {
	async *run() {
		yield { content: [{ type: "text" as const, text: "" }] };
		await wait(50);
		yield { content: [{ type: "text" as const, text: "response" }] };
	},
};

function CtoRunningBridge() {
	const isRunning = useAuiState((s) => s.thread.isRunning);
	React.useEffect(() => {
		usePuxStore.setState({ ctoRunning: isRunning });
	}, [isRunning]);
	return null;
}

/**
 * Flash-free input: the library's ComposerInput syncs its local buffer
 * from the runtime text via useEffect (AFTER commit), causing a 1-frame
 * flash. Fix: hide the input during the sync window.
 */
function FlashFreeInput() {
	const aui = useAui();
	const text = useAuiState((s) => s.composer.text);
	const [hiding, setHiding] = React.useState(false);

	React.useEffect(() => {
		if (hiding && !text) setHiding(false);
	}, [hiding, text]);

	if (hiding) {
		return <Text> </Text>;
	}

	return (
		<ComposerPrimitive.Input
			submitOnEnter
			autoFocus
			placeholder="..."
			onSubmit={(submittedText) => {
				setHiding(true);
				aui.composer().setText("");
				aui.thread().append({
					content: [{ type: "text", text: submittedText }],
					startRun: true,
				});
			}}
		/>
	);
}

function makeApp(adapter: any, useBridge = false) {
	return function TestApp() {
		const runtime = useLocalRuntime(adapter);
		return (
			<AssistantRuntimeProvider runtime={runtime}>
				{useBridge && <CtoRunningBridge />}
				<ThreadPrimitive.Root flexDirection="column">
					<ThreadPrimitive.Empty>
						<Text>No messages yet</Text>
					</ThreadPrimitive.Empty>
					<ThreadPrimitive.Messages>
						{({ message }: any) => (
							<Text key={message.id}>
								{message.role}: {message.content?.[0]?.text || ""}
							</Text>
						)}
					</ThreadPrimitive.Messages>
					<Box borderStyle="round" borderColor="gray" paddingX={1}>
						<Text color="gray">{"> "}</Text>
						<FlashFreeInput />
					</Box>
				</ThreadPrimitive.Root>
			</AssistantRuntimeProvider>
		);
	};
}

/**
 * After pressing Enter, poll ALL frames. The flash is:
 *   - Composer still shows typed text (inside the border box)
 *   - But NO user message has appeared yet
 *
 * The fix hides the input for one frame while the buffer syncs,
 * so the first committed frame should already be clean.
 */
async function detectFlash(adapter: any, label: string, useBridge = false) {
	usePuxStore.setState({ ctoRunning: false, agents: new Map() });

	const App = makeApp(adapter, useBridge);
	const { lastFrame, stdin } = render(<App />);
	await wait(100);

	// Use unique text that won't appear in message rendering
	const magic = "ZZZMAGIC";
	stdin.write(magic);
	await wait(50);

	// Confirm text is in composer
	const before = lastFrame()!;
	expect(before).toContain(magic);

	// Press Enter
	stdin.write("\r");

	// Wait for React to commit the render triggered by the event handler.
	// Ink only writes to stdout on React commits — the pre-commit state
	// is never displayed in the real terminal. This wait mimics that.
	await wait(16); // one terminal frame at 60fps

	// Capture committed frames (what the user actually sees)
	const frames: string[] = [];
	for (let i = 0; i < 15; i++) {
		frames.push(lastFrame()!);
		await wait(8);
	}

	console.log(`\n${label}:`);
	frames.forEach((f, i) => {
		// Composer still has magic text = text inside border but NO "user:" message
		const hasComposerText = f.includes(`> ${magic}`) || f.includes(`│ ${magic}`);
		const hasUserMsg = f.includes("user:");
		if (hasComposerText && !hasUserMsg) {
			console.log(`  [${i}] *** FLASH: composer still has "${magic}", no user message ***`);
		} else if (hasUserMsg) {
			console.log(`  [${i}] OK: user message present`);
		}
	});

	// Count flash frames: composer has magic text but no user message
	let flashFrames = 0;
	for (const f of frames) {
		const hasComposerText = f.includes(`> ${magic}`) || f.includes(`│ ${magic}`);
		const hasUserMsg = f.includes("user:");
		if (hasComposerText && !hasUserMsg) {
			flashFrames++;
		}
	}

	return { flashFrames, frames };
}

describe("Enter flash: rendered output", () => {
	test("no flash: composer text must clear immediately when Enter pressed", async () => {
		const { flashFrames } = await detectFlash(
			simpleAdapter,
			"Simple adapter",
		);

		console.log(`\n  Flash frames detected: ${flashFrames}`);
		expect(flashFrames).toBe(0);
	});
});

describe("Adapter ordering", () => {
	test("puxChatAdapter: adapter does NOT set ctoRunning directly", async () => {
		const fs = await import("node:fs");
		const path = await import("node:path");
		const adapterPath = path.join(
			__dirname,
			"../../shared/src/pux-chat-adapter.ts",
		);
		const source = fs.readFileSync(adapterPath, "utf-8");

		const setStateMatch = source.match(
			/usePuxStore\.setState\(\{[^}]*ctoRunning:\s*true/s,
		);
		expect(setStateMatch).toBeNull();
	});

	test("app.tsx: CtoRunningBridge is mounted inside provider", async () => {
		const fs = await import("node:fs");
		const path = await import("node:path");
		const appPath = path.join(
			__dirname,
			"../src/app.tsx",
		);
		const source = fs.readFileSync(appPath, "utf-8");

		expect(source).toContain("function CtoRunningBridge");
		expect(source).toContain("<CtoRunningBridge />");
		expect(source).toContain("useAuiState");
	});
});
