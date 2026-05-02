export type ImageProtocol = "kitty" | "iterm2" | null;

interface Capabilities {
  images: ImageProtocol;
  trueColor: boolean;
  hyperlinks: boolean;
}

interface CellDimensions {
  widthPx: number;
  heightPx: number;
}

let cached: Capabilities | null = null;

function detect(): Capabilities {
  const tp = (process.env.TERM_PROGRAM || "").toLowerCase();
  const term = (process.env.TERM || "").toLowerCase();

  if (process.env.KITTY_WINDOW_ID || tp === "kitty")
    return { images: "kitty", trueColor: true, hyperlinks: true };
  if (tp === "ghostty" || term.includes("ghostty") || process.env.GHOSTTY_RESOURCES_DIR)
    return { images: "kitty", trueColor: true, hyperlinks: true };
  if (process.env.WEZTERM_PANE || tp === "wezterm")
    return { images: "kitty", trueColor: true, hyperlinks: true };
  if (process.env.ITERM_SESSION_ID || tp === "iterm.app")
    return { images: "iterm2", trueColor: true, hyperlinks: true };

  const ct = (process.env.COLORTERM || "").toLowerCase();
  return {
    images: null,
    trueColor: ct === "truecolor" || ct === "24bit",
    hyperlinks: true,
  };
}

export function imageProtocol(): ImageProtocol {
  if (!cached) cached = detect();
  return cached.images;
}

/** Default terminal cell size — 9x18px typical for modern terms */
const CELL: CellDimensions = { widthPx: 9, heightPx: 18 };

function encodeKitty(
  b64: string,
  opts: { maxW?: number; maxH?: number } = {}
): string {
  const CHUNK = 4096;
  const parts: string[] = [];
  if (opts.maxW) parts.push(`c=${opts.maxW}`);
  if (opts.maxH) parts.push(`r=${opts.maxH}`);
  parts.push("a=T", "f=100", "q=2");

  const prefix = `\x1b_G${parts.join(",")};`;
  const suffix = "\x1b\\";

  let out = "";
  let pos = 0;
  while (pos < b64.length) {
    const chunk = b64.slice(pos, pos + CHUNK);
    pos += CHUNK;
    out += prefix + (pos >= b64.length ? "" : "m=1;") + chunk + suffix;
  }
  return out;
}

function encodeITerm2(
  b64: string,
  opts: { maxW?: number; maxH?: number } = {}
): string {
  let params = "inline=1";
  if (opts.maxW) params += `;width=${opts.maxW}px`;
  if (opts.maxH) params += `;height=${opts.maxH}px`;
  return `\x1b]1337;File=${params}:${b64}\x07`;
}

/**
 * Render a base64-encoded PNG/JPEG image as a terminal escape sequence.
 * Returns the raw escape sequence string to embed in a Text component.
 * Supports Kitty, Ghostty, WezTerm (kitty protocol) and iTerm2.
 * Returns null on unsupported terminals.
 */
export function renderImage(
  b64Data: string,
  opts: { maxW?: number; maxH?: number } = {}
): string | null {
  const proto = imageProtocol();
  if (!b64Data) return null;
  if (proto === "kitty") {
    const maxW = opts.maxW ?? Math.floor((process.stdout.columns || 80) * 0.8);
    const maxH = opts.maxH ?? Math.floor(24 * 0.6);
    return encodeKitty(b64Data, {
      maxW: Math.min(maxW, Math.floor((process.stdout.columns || 80) * CELL.widthPx)),
      maxH: Math.min(maxH, Math.floor(24 * CELL.heightPx)),
    });
  }
  if (proto === "iterm2") {
    return encodeITerm2(b64Data, {
      maxW: opts.maxW ?? Math.floor((process.stdout.columns || 80) * CELL.widthPx * 0.8),
      maxH: opts.maxH ?? 24 * CELL.heightPx,
    });
  }
  return null;
}

/**
 * Text placeholder for terminals without image support.
 * Shows dimensions and file info extracted from base64 header.
 */
export function imagePlaceholder(b64: string, label?: string): string {
  let dims = "";
  try {
    const buf = Buffer.from(b64.slice(0, 100), "base64");
    if (buf[0] === 0x89 && buf[1] === 0x50) {
      // PNG: IHDR starts at byte 16, width at 16, height at 20
      const w = buf.readUInt32BE(16);
      const h = buf.readUInt32BE(20);
      dims = ` ${w}x${h}`;
    }
  } catch {}
  return `[${label || "Image"}${dims}]`;
}

/** Check if the terminal supports inline images */
export function hasImageSupport(): boolean {
  return imageProtocol() !== null;
}
