// Stub: config
import * as path from "node:path";
import * as os from "node:os";
export function getThemesDir(): string { return path.join(os.homedir(), ".pux", "themes"); }
export function getCustomThemesDir(): string { return path.join(os.homedir(), ".pux", "custom-themes"); }
