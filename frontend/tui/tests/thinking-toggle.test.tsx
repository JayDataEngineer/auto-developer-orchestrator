import { describe, test, expect } from "vitest";
import { usePuxStore } from "@pux/shared";

describe("Thinking toggle", () => {
	test("toggleThinking flips thinkingExpanded", () => {
		usePuxStore.setState({ thinkingExpanded: false });
		expect(usePuxStore.getState().thinkingExpanded).toBe(false);
		usePuxStore.getState().toggleThinking();
		expect(usePuxStore.getState().thinkingExpanded).toBe(true);
		usePuxStore.getState().toggleThinking();
		expect(usePuxStore.getState().thinkingExpanded).toBe(false);
	});
});
