/**
 * Patches for @assistant-ui/react-ink ComposerInput.
 *
 * Adds Ctrl+Delete (kill-word-forward) and Ctrl+Backspace (kill-word-backward)
 * key bindings that the library doesn't ship with.
 */

const fs = require("fs");
const path = require("path");

const file = path.join(
	__dirname,
	"node_modules",
	"@assistant-ui",
	"react-ink",
	"dist",
	"primitives",
	"composer",
	"ComposerInput.js"
);

if (!fs.existsSync(file)) {
	console.warn("[patches] ComposerInput.js not found, skipping");
	process.exit(0);
}

let src = fs.readFileSync(file, "utf8");

// Check if already patched
if (src.includes("kill-word-forward") && src.includes('key.backspace && key.ctrl')) {
	process.exit(0);
}

// Add Ctrl+Delete → kill-word-forward (before the j/a/e/w/u/k/d sequence)
if (!src.includes("key.delete) {")) {
	const marker = 'if (key.ctrl) {\n\t\t\tif (lowerInput === "j")';
	src = src.replace(
		marker,
		'if (key.ctrl) {\n\t\t\tif (key.delete) {\n\t\t\t\tcommitAction({ type: "kill-word-forward" });\n\t\t\t\treturn;\n\t\t\t}\n\t\t\tif (lowerInput === "j")'
	);
}

// Add Ctrl+Backspace → kill-word-backward
if (!src.includes("key.backspace && key.ctrl")) {
	const marker = 'if (key.backspace) {\n\t\t\tcommitAction({ type: "delete-backward" })';
	src = src.replace(
		marker,
		'if (key.backspace && key.ctrl) {\n\t\t\tcommitAction({ type: "kill-word-backward" });\n\t\t\treturn;\n\t\t}\n\t\tif (key.backspace) {\n\t\t\tcommitAction({ type: "delete-backward" })'
	);
}

fs.writeFileSync(file, src);
console.log("[patches] Applied ComposerInput key bindings");
