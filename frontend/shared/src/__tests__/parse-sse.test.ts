/**
 * parseSSE — adversarial buffer-split tests.
 *
 * The SSE parser is the first link in the streaming pipeline. If it misparses,
 * every downstream event handler breaks. These tests prove it handles the
 * adversarial cases that real network chunking produces:
 *
 *   - mid-prefix splits ("event: text_d" + "elta")
 *   - mid-data splits
 *   - mid-boundary splits (one chunk ends after \n, next starts with \n)
 *   - multiple events in one chunk
 *   - keepalive events
 *   - half-event at end → goes to remaining
 *
 * Tests run against the REAL exported parseSSE from pux-chat-adapter.ts.
 * Mutation anchor: if someone changes parseSSE's split/pop logic, these
 * tests fail.
 */

import { describe, it, expect } from "vitest";
import { parseSSE } from "../pux-chat-adapter";

describe("parseSSE: complete events in one chunk", () => {
	it("parses a single complete event", () => {
		const buffer = "event: text_delta\ndata: {\"text\":\"hello\"}\n\n";
		const { events, remaining } = parseSSE(buffer);
		expect(events).toEqual([
			{ event: "text_delta", data: '{"text":"hello"}' },
		]);
		expect(remaining).toBe("");
	});

	it("parses multiple events in one chunk", () => {
		const buffer = [
			"event: text_delta",
			'data: {"text":"a"}',
			"",
			"event: text_delta",
			'data: {"text":"b"}',
			"",
			"event: tool_execution_start",
			'data: {"toolId":"tc1"}',
			"",
		].join("\n");
		const { events, remaining } = parseSSE(buffer);
		expect(events).toHaveLength(3);
		expect(events[0]).toEqual({ event: "text_delta", data: '{"text":"a"}' });
		expect(events[1]).toEqual({ event: "text_delta", data: '{"text":"b"}' });
		expect(events[2]).toEqual({ event: "tool_execution_start", data: '{"toolId":"tc1"}' });
		expect(remaining).toBe("");
	});

	it("handles keepalive events (no data, just keepalive keyword)", () => {
		// The adapter checks `if (event === "keepalive") continue;` but parseSSE
		// must still surface the event so the adapter can skip it.
		const buffer = ": keepalive comment\n\n";
		const { events, remaining } = parseSSE(buffer);
		// Comment lines (starting with ":") are not "event:" or "data:" —
		// parseSSE ignores them. No events should be emitted.
		expect(events).toEqual([]);
		expect(remaining).toBe("");
	});

	it("handles explicit keepalive event", () => {
		const buffer = "event: keepalive\ndata: {}\n\n";
		const { events } = parseSSE(buffer);
		expect(events).toEqual([{ event: "keepalive", data: "{}" }]);
	});
});

describe("parseSSE: adversarial buffer splits", () => {
	it("returns partial prefix as remaining when chunk ends mid-'event:' line", () => {
		// First chunk cuts off mid-prefix
		const chunk1 = "event: text_d";
		const r1 = parseSSE(chunk1);
		expect(r1.events).toEqual([]);
		expect(r1.remaining).toBe("event: text_d");

		// Simulate the adapter's buffer accumulation: remaining + next chunk
		const buffer = r1.remaining + "elta\ndata: {\"text\":\"x\"}\n\n";
		const r2 = parseSSE(buffer);
		expect(r2.events).toEqual([
			{ event: "text_delta", data: '{"text":"x"}' },
		]);
		expect(r2.remaining).toBe("");
	});

	it("returns partial data line as remaining when chunk ends mid-'data:'", () => {
		const chunk1 = "event: text_delta\ndata: {\"text\":\"hel";
		const r1 = parseSSE(chunk1);
		expect(r1.events).toEqual([]);
		// The event: line was processed but no data: line completed in this
		// chunk. parseSSE preserves the event: line in remaining so the next
		// call can re-process it together with the rest of the data: line.
		// Without this, the event type ("text_delta") would be lost — the
		// data: line would emit with event: "" instead.
		expect(r1.remaining).toBe("event: text_delta\ndata: {\"text\":\"hel");

		// Next chunk completes the data: line — adapter concatenates remaining
		const buffer = r1.remaining + "lo\"}\n\n";
		const r2 = parseSSE(buffer);
		expect(r2.events).toEqual([
			{ event: "text_delta", data: '{"text":"hello"}' },
		]);
	});

	it("handles split at the \\n\\n boundary (chunk ends after first \\n)", () => {
		// Chunk 1: event+data+\n (no second \n yet)
		const chunk1 = "event: text_delta\ndata: {\"text\":\"x\"}\n";
		const r1 = parseSSE(chunk1);
		// parseSSE pops the trailing "" — but here there's no trailing "" because
		// the buffer ends with \n. Let me trace:
		//   buffer.split("\n") = ["event: text_delta", "data: {...}", ""]
		//   lines.pop() = "" → remaining
		//   for: ["event: text_delta", "data: {...}"]
		//   data: triggers event push
		// So events has 1, remaining is ""
		expect(r1.events).toEqual([
			{ event: "text_delta", data: '{"text":"x"}' },
		]);
		expect(r1.remaining).toBe("");

		// Chunk 2: just the second \n + next event
		const chunk2 = "\nevent: text_delta\ndata: {\"text\":\"y\"}\n\n";
		const r2 = parseSSE(chunk2);
		// chunk2 starts with \n, so split = ["", "event: text_delta", "data: {...}", ""]
		// pop returns "" → remaining
		// for: ["", "event: text_delta", "data: {...}"]
		// "" is boundary (no-op), then event+data → push
		expect(r2.events).toEqual([
			{ event: "text_delta", data: '{"text":"y"}' },
		]);
	});

	it("returns entire buffer as remaining when no newline at all", () => {
		const chunk = "event: text_delta";
		const r = parseSSE(chunk);
		expect(r.events).toEqual([]);
		expect(r.remaining).toBe("event: text_delta");
	});

	it("empty buffer returns empty events and empty remaining", () => {
		const r = parseSSE("");
		expect(r.events).toEqual([]);
		expect(r.remaining).toBe("");
	});

	it("chunk with only newlines returns empty", () => {
		const r = parseSSE("\n\n\n");
		expect(r.events).toEqual([]);
		// split("\n") of "\n\n\n" = ["", "", "", ""], pop = "" → remaining
		expect(r.remaining).toBe("");
	});
});

describe("parseSSE: edge cases", () => {
	it("event with empty data field", () => {
		const buffer = "event: keepalive\ndata: \n\n";
		const { events } = parseSSE(buffer);
		expect(events).toEqual([{ event: "keepalive", data: "" }]);
	});

	it("data line without preceding event field", () => {
		// The backend always sends event: first, but if it didn't,
		// parseSSE should still produce an event with empty event name.
		const buffer = "data: {\"text\":\"orphan\"}\n\n";
		const { events } = parseSSE(buffer);
		expect(events).toEqual([{ event: "", data: '{"text":"orphan"}' }]);
	});

	it("handles [DONE] sentinel", () => {
		const buffer = "data: [DONE]\n\n";
		const { events } = parseSSE(buffer);
		expect(events).toEqual([{ event: "", data: "[DONE]" }]);
	});

	it("data with embedded characters that look like prefixes", () => {
		// JSON with the string "data: " inside should NOT confuse the parser
		const buffer =
			'event: text_delta\ndata: {"text":"this has data: inside"}\n\n';
		const { events } = parseSSE(buffer);
		expect(events).toEqual([
			{ event: "text_delta", data: '{"text":"this has data: inside"}' },
		]);
	});

	it("preserves whitespace in data after 'data: ' prefix", () => {
		const buffer = 'event: text_delta\ndata:   spaced\n\n';
		const { events } = parseSSE(buffer);
		// slice(6) removes "data: " (6 chars), leaving "  spaced"
		expect(events).toEqual([
			{ event: "text_delta", data: "  spaced" },
		]);
	});
});

describe("parseSSE: realistic subagent stream excerpt", () => {
	it("parses a multi-event subagent stream chunk correctly", () => {
		// Mirrors what a real delegate_to SSE stream burst looks like.
		// Each data line MUST be prefixed with "data: " per SSE spec.
		const buffer = [
			"event: subagent_start",
			'data: {"agentId":"jake_1","agentName":"jake","task":"do thing"}',
			"",
			"event: thinking_delta",
			'data: {"agentName":"jake","text":"hmm"}',
			"",
			"event: text_delta",
			'data: {"agentName":"jake","text":"Working"}',
			"",
			"event: tool_execution_start",
			'data: {"agentName":"jake","toolId":"tc_1","toolName":"bash","args":{"command":"ls"}}',
			"",
			"event: tool_execution_end",
			'data: {"agentName":"jake","toolId":"tc_1","result":"file.txt"}',
			"",
			"event: subagent_end",
			'data: {"agentId":"jake_1","agentName":"jake","status":"complete"}',
			"",
		].join("\n");

		const { events, remaining } = parseSSE(buffer);
		expect(events).toHaveLength(6);
		expect(events.map((e) => e.event)).toEqual([
			"subagent_start",
			"thinking_delta",
			"text_delta",
			"tool_execution_start",
			"tool_execution_end",
			"subagent_end",
		]);
		// All event names + datas are intact
		expect(events[0].data).toContain('"agentName":"jake"');
		expect(events[5].data).toContain('"status":"complete"');
		expect(remaining).toBe("");
	});
});
