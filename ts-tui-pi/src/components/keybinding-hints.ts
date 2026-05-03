// Stub: keybinding-hints — returns simple text for key descriptions
import type { Component as TUIComponent } from "../tui/index.js";
import { Text } from "../tui/index.js";

export function keyHint(key: string): string {
  return `(${key})`;
}

export function keyText(key: string): string {
  return key;
}

export function KeybindingHints(_props: { keys?: string[] }): TUIComponent {
  return Text("") as unknown as TUIComponent;
}
