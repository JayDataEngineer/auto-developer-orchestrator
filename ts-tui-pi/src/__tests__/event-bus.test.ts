import { describe, test, expect } from "bun:test";
import { createEventBus } from "../core/event-bus.js";

describe("createEventBus", () => {
  test("emit calls registered handler", () => {
    const bus = createEventBus();
    let called = false;
    bus.on("test", () => { called = true; });
    bus.emit("test", "data");
    expect(called).toBe(true);
  });

  test("handler receives data", () => {
    const bus = createEventBus();
    let received: unknown = null;
    bus.on("test", (data) => { received = data; });
    bus.emit("test", { key: 42 });
    expect(received).toEqual({ key: 42 });
  });

  test("on returns unsubscribe function", () => {
    const bus = createEventBus();
    const unsub = bus.on("test", () => {});
    expect(typeof unsub).toBe("function");
  });

  test("unsubscribe stops handler from being called", () => {
    const bus = createEventBus();
    let count = 0;
    const unsub = bus.on("test", () => { count++; });
    bus.emit("test", "a");
    unsub();
    bus.emit("test", "b");
    expect(count).toBe(1);
  });

  test("multiple handlers on same channel", () => {
    const bus = createEventBus();
    const calls: number[] = [];
    bus.on("test", () => calls.push(1));
    bus.on("test", () => calls.push(2));
    bus.emit("test", "data");
    expect(calls).toEqual([1, 2]);
  });

  test("different channels are isolated", () => {
    const bus = createEventBus();
    const calls: string[] = [];
    bus.on("a", () => calls.push("a"));
    bus.on("b", () => calls.push("b"));
    bus.emit("a", "");
    expect(calls).toEqual(["a"]);
  });

  test("handler errors do not crash the bus", () => {
    const bus = createEventBus();
    const consoleSpy = { args: "" };
    const origError = console.error;
    console.error = (...args: any[]) => { consoleSpy.args = args.join(" "); };

    bus.on("test", () => { throw new Error("handler crashed"); });
    bus.emit("test", "data");

    expect(consoleSpy.args).toContain("handler crashed");
    console.error = origError;
  });

  test("clear removes all handlers", () => {
    const bus = createEventBus();
    let count = 0;
    bus.on("test", () => count++);
    bus.clear();
    bus.emit("test", "data");
    bus.emit("test", "more");
    expect(count).toBe(0);
  });
});
