#!/usr/bin/env bun
/**
 * Raw stdin key sequence dumper.
 * Run: bun run src/key-dump.tsx
 * Press Shift+Enter and other keys to see what escape sequences
 * your terminal sends. Press Ctrl+C to exit.
 */
import React from "react";
import { render, useInput, useStdin, Box, Text } from "ink";
import { useState, useEffect } from "react";

function App() {
  const { stdin } = useStdin();
  const [updates, setUpdates] = useState<string[]>([]);
  const update = (msg: string) => setUpdates(v => [...v.slice(-19), msg]);

  useEffect(() => {
    if (!stdin) return;
    const handler = (data: Buffer) => {
      const hex = Array.from(data).map(b => "0x" + b.toString(16).padStart(2, "0")).join(" ");
      const esc = JSON.stringify(data.toString());
      const seq = data.length > 1 ? ` ← ESC SEQ (len=${data.length})` : "";
      update(`raw stdin  [${hex}]  ${esc}${seq}`);
    };
    stdin.on("data", handler);
    return () => { stdin.off("data", handler); };
  }, [stdin]);

  useInput((inp, key) => {
    const parts: string[] = [];
    if (inp) parts.push(`inp=${JSON.stringify(inp)}`);
    const kf = Object.entries(key).filter(([, v]) => v);
    if (kf.length) parts.push(kf.map(([k]) => k).join(" "));
    update(`useInput   ${parts.join(" | ")}`);
  });

  return (
    <Box flexDirection="column" padding={1}>
      <Text bold>Press keys — Shift+Enter, Enter, Tab, etc. Ctrl+C to quit.</Text>
      <Box flexDirection="column" marginTop={1}>
        {updates.map((u, i) => (
          <Text key={i}>{u}</Text>
        ))}
      </Box>
    </Box>
  );
}

render(<App />);
