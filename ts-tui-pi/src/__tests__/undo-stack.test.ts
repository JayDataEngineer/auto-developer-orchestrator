import { describe, test, expect } from "bun:test";
import { UndoStack } from "../tui/undo-stack.js";

describe("UndoStack", () => {
  test("push then pop returns the value", () => {
    const stack = new UndoStack<string>();
    stack.push("hello");
    expect(stack.pop()).toBe("hello");
  });

  test("pop on empty stack returns undefined", () => {
    const stack = new UndoStack<string>();
    expect(stack.pop()).toBeUndefined();
  });

  test("LIFO order", () => {
    const stack = new UndoStack<number>();
    stack.push(1);
    stack.push(2);
    stack.push(3);
    expect(stack.pop()).toBe(3);
    expect(stack.pop()).toBe(2);
    expect(stack.pop()).toBe(1);
    expect(stack.pop()).toBeUndefined();
  });

  test("clear removes all entries", () => {
    const stack = new UndoStack<string>();
    stack.push("a");
    stack.push("b");
    stack.clear();
    expect(stack.pop()).toBeUndefined();
  });

  test("length tracks stack size", () => {
    const stack = new UndoStack<number>();
    expect(stack.length).toBe(0);
    stack.push(1);
    expect(stack.length).toBe(1);
    stack.push(2);
    expect(stack.length).toBe(2);
    stack.pop();
    expect(stack.length).toBe(1);
    stack.clear();
    expect(stack.length).toBe(0);
  });

  test("clone-on-push: modifying original does not affect stored value", () => {
    const stack = new UndoStack<{ x: number }>();
    const obj = { x: 1 };
    stack.push(obj);
    obj.x = 999;
    const popped = stack.pop();
    expect(popped).toEqual({ x: 1 });
    expect(popped).not.toBe(obj);
  });

  test("works with primitive types", () => {
    const stack = new UndoStack<number>();
    expect(stack.pop()).toBeUndefined();
    stack.push(42);
    expect(stack.pop()).toBe(42);
  });

  test("pop returns direct reference (already deep-cloned)", () => {
    const stack = new UndoStack<{ a: number }>();
    stack.push({ a: 1 });
    const popped = stack.pop()!;
    popped.a = 999;
    const poppedAgain = stack.pop();
    expect(poppedAgain).toBeUndefined();
  });
});
