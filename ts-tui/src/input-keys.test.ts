import { test, expect } from "bun:test";

/**
 * Test the Shift+Enter detection logic from app.tsx (extracted here).
 *
 * CSI-u modifier encoding (1-indexed):
 *   1=none, 2=shift, 3=alt, 4=shift+alt, 5=ctrl, 6=ctrl+shift, ...
 * shift bit = ((mod - 1) & 1) === 1
 *
 * modifyOtherKeys: same 1-indexed modifier encoding.
 */

function isShiftEnterCSI(s: string): boolean {
  const m = s.match(/^\x1b\[(13|57414);([\d]+)(u|~)/);
  if (m && m[2]) {
    const mod = parseInt(m[2], 10);
    return ((mod - 1) & 1) === 1;
  }
  return false;
}

function isShiftEnterXterm(s: string): boolean {
  // Format 1: \x1b[27;mod;code~  (modifyOtherKeys level 1 — three numbers)
  const m3 = s.match(/^\x1b\[27;(\d+);(\d+)~/);
  if (m3 && m3[1] && m3[2]) {
    const mod = parseInt(m3[1], 10);
    const code = parseInt(m3[2], 10);
    return (code === 13 || code === 57414) && ((mod - 1) & 1) === 1;
  }
  // Format 2: \x1b[code;mod~  (modifyOtherKeys level 2 — two numbers)
  const m2 = s.match(/^\x1b\[(\d+);(\d+)~/);
  if (m2 && m2[1] && m2[2]) {
    const code = parseInt(m2[1], 10);
    const mod = parseInt(m2[2], 10);
    return (code === 13 || code === 57414) && ((mod - 1) & 1) === 1;
  }
  return false;
}

function isShiftEnter(s: string): boolean {
  return isShiftEnterCSI(s) || isShiftEnterXterm(s);
}

// ─── CSI-u ────────────────────────────────────────────────

test("Kitty CSI-u: Shift+Enter = \\x1b[13;2u", () => {
  expect(isShiftEnter("\x1b[13;2u")).toBe(true);
});

test("Kitty CSI-u: Shift+KP_Enter = \\x1b[57414;2u", () => {
  expect(isShiftEnter("\x1b[57414;2u")).toBe(true);
});

test("CSI-u: Shift+Alt+Enter = \\x1b[13;4u", () => {
  expect(isShiftEnter("\x1b[13;4u")).toBe(true);
});

test("CSI-u: Ctrl+Shift+Enter = \\x1b[13;6u", () => {
  expect(isShiftEnter("\x1b[13;6u")).toBe(true);
});

test("CSI-u: Ctrl+Shift+Alt+Enter = \\x1b[13;8u", () => {
  expect(isShiftEnter("\x1b[13;8u")).toBe(true);
});

test("CSI-u: Alt+Enter (no shift) = \\x1b[13;3u → false", () => {
  expect(isShiftEnter("\x1b[13;3u")).toBe(false);
});

test("CSI-u: Ctrl+Enter (no shift) = \\x1b[13;5u → false", () => {
  expect(isShiftEnter("\x1b[13;5u")).toBe(false);
});

test("CSI-u: Plain Enter (no modifiers) = \\x1b[13u → false", () => {
  expect(isShiftEnter("\x1b[13u")).toBe(false);
});

test("CSI-u: Non-enter key with shift = \\x1b[65;2u → false", () => {
  expect(isShiftEnter("\x1b[65;2u")).toBe(false);
});

// ─── xterm modifyOtherKeys ────────────────────────────────

test("xterm: Shift+Enter = \\x1b[27;2;13~", () => {
  expect(isShiftEnter("\x1b[27;2;13~")).toBe(true);
});

test("xterm: Shift+Enter (format 2) = \\x1b[13;2~", () => {
  expect(isShiftEnter("\x1b[13;2~")).toBe(true);
});

test("xterm: Alt+Enter = \\x1b[27;3;13~ → false", () => {
  expect(isShiftEnter("\x1b[27;3;13~")).toBe(false);
});

test("xterm: Plain Enter = \\x1b[27;1;13~ → false", () => {
  expect(isShiftEnter("\x1b[27;1;13~")).toBe(false);
});

test("xterm: Shift+Alt+Enter = \\x1b[27;4;13~ → true", () => {
  expect(isShiftEnter("\x1b[27;4;13~")).toBe(true);
});

test("xterm: Ctrl+Shift+Enter = \\x1b[27;6;13~ → true", () => {
  expect(isShiftEnter("\x1b[27;6;13~")).toBe(true);
});

// ─── Negative tests ───────────────────────────────────────

test("Plain Enter = \\r → false", () => {
  expect(isShiftEnter("\r")).toBe(false);
});

test("Plain Enter = \\n → false", () => {
  expect(isShiftEnter("\n")).toBe(false);
});

test("Tab = \\t → false", () => {
  expect(isShiftEnter("\t")).toBe(false);
});

test("Ordinary character = 'a' → false", () => {
  expect(isShiftEnter("a")).toBe(false);
});

test("Empty string → false", () => {
  expect(isShiftEnter("")).toBe(false);
});
