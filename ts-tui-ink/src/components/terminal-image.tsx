import React, { useEffect, useRef } from "react";
import { Box, Text } from "ink";
import { colors, BLOCKQUOTE_BAR } from "../theme.js";

export interface TerminalImageProps {
  image: string;
  filename?: string;
  maxWidth?: number;
}

function detectTerminal(): "kitty" | "iterm2" | "none" {
  if (
    process.env.KITTY_WINDOW_ID ||
    process.env.TERM === "xterm-kitty"
  ) {
    return "kitty";
  }
  if (process.env.TERM_PROGRAM === "iTerm.app") {
    return "iterm2";
  }
  return "none";
}

function renderKitty(pngBase64: string): void {
  const chunkSize = 4096;
  const totalChunks = Math.ceil(pngBase64.length / chunkSize);
  for (let i = 0; i < totalChunks; i++) {
    const chunk = pngBase64.slice(i * chunkSize, (i + 1) * chunkSize);
    const isLast = i === totalChunks - 1;
    const payload =
      `\x1b_Ga=T,f=100,d=A,C=1,m=${totalChunks > 1 ? 1 : 0}` +
      (isLast ? ",m=0" : "") +
      `;${chunk}\x1b\\`;
    process.stdout.write(payload);
  }
  if (totalChunks > 1) {
    process.stdout.write("\x1b_Ga=T,f=100,d=A,C=1,m=0;\x1b\\");
  }
}

function renderITerm2(pngBase64: string): void {
  const inline = Buffer.from(pngBase64, "base64").toString("base64");
  process.stdout.write(`\x1b]1337;File=inline=1;preserveAspectRatio=1:${inline}\x07`);
}

function extractBase64(dataUri: string): { base64: string; mime: string } | null {
  const match = dataUri.match(/^data:([^;]+);base64,(.+)$/);
  if (match) {
    return { base64: match[2], mime: match[1] };
  }
  return null;
}

export function TerminalImage({ image, filename, maxWidth }: TerminalImageProps) {
  const rendered = useRef(false);

  useEffect(() => {
    if (rendered.current) return;
    rendered.current = true;

    const term = detectTerminal();
    const extracted = extractBase64(image);

    if (term === "kitty" && extracted) {
      renderKitty(extracted.base64);
    } else if (term === "iterm2" && extracted) {
      renderITerm2(extracted.base64);
    }
  }, [image]);

  const term = detectTerminal();
  const canRender = term !== "none" && extractBase64(image) !== null;

  if (canRender) {
    return (
      <Box flexDirection="column" paddingLeft={2} marginY={0}>
        {filename && (
          <Text dimColor>
            {BLOCKQUOTE_BAR} {filename}
          </Text>
        )}
      </Box>
    );
  }

  const preview = image.length > 80 ? image.slice(0, 77) + "..." : image;
  return (
    <Box flexDirection="column" paddingLeft={2} marginY={0}>
      <Text dimColor>
        {BLOCKQUOTE_BAR} image
        {filename ? `: ${filename}` : ""}
      </Text>
      <Text color="gray">
        {BLOCKQUOTE_BAR} {preview}
      </Text>
      <Text dimColor>
        {BLOCKQUOTE_BAR} (use Kitty or iTerm2 terminal to view inline)
      </Text>
    </Box>
  );
}
