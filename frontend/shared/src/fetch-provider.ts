/**
 * Fetch provider — injectable fetch for browser vs bun environments.
 *
 * Web: uses browser fetch (Vite proxies /api to Go backend).
 * TUI: uses bun's fetch with full URL (http://localhost:3847).
 *
 * Call setFetch() at TUI startup. Web does nothing.
 */

type FetchFn = typeof fetch;

let _fetch: FetchFn = globalThis.fetch.bind(globalThis);

export function setFetch(fn: FetchFn): void {
	_fetch = fn;
}

export function getFetch(): FetchFn {
	return _fetch;
}
