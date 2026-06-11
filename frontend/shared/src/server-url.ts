/**
 * Server URL provider — URL prefix for API requests.
 *
 * Web: empty prefix (Vite proxy handles routing).
 * TUI: full URL like "http://localhost:3847".
 *
 * Call setBaseUrl() at TUI startup. Web does nothing.
 */

let _base = "";

export function setBaseUrl(url: string): void {
	_base = url.replace(/\/$/, "");
}

export function apiUrl(path: string): string {
	return _base ? `${_base}${path}` : path;
}
