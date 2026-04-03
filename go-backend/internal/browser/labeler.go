package browser

import (
	"encoding/json"
)

// labelerJS is injected into every page to label interactive elements
const labelerJS = `
(() => {
	// Remove any existing label overlay
	const existingOverlay = document.getElementById('__browser_label_overlay__');
	if (existingOverlay) existingOverlay.remove();

	const elements = [];
	const interactiveSelectors = 'a, button, input, select, textarea, [role="button"], [role="link"], [role="tab"], [role="menuitem"], [onclick], [type="submit"]';
	const nodes = document.querySelectorAll(interactiveSelectors);

	// Create overlay for visual labels
	const overlay = document.createElement('div');
	overlay.id = '__browser_label_overlay__';
	overlay.style.cssText = 'position:fixed;top:0;left:0;width:100%;height:100%;pointer-events:none;z-index:999999;';

	let id = 1;
	const seen = new Set();

	for (const el of nodes) {
		// Skip hidden/invisible elements
		const rect = el.getBoundingClientRect();
		if (rect.width === 0 || rect.height === 0) continue;

		// Skip elements outside viewport (with some margin)
		if (rect.bottom < -100 || rect.top > window.innerHeight + 100) continue;

		// Build unique selector
		let selector = '';
		if (el.id) {
			selector = '#' + CSS.escape(el.id);
		} else {
			selector = buildSelector(el);
		}

		// Skip duplicate selectors
		if (seen.has(selector)) continue;
		seen.add(selector);

		// Get element text
		let text = (el.textContent || '').trim().substring(0, 50);
		if (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA') {
			text = el.placeholder || el.value || el.name || el.type || '';
			text = text.substring(0, 50);
		}

		// Add visual label
		const label = document.createElement('div');
		label.style.cssText = 'position:absolute;pointer-events:none;background:rgba(255,0,0,0.85);color:white;font-size:10px;font-family:monospace;padding:1px 3px;border-radius:2px;z-index:1000000;line-height:1.2;white-space:nowrap;';
		label.textContent = '' + id;
		label.style.left = (rect.left + window.scrollX) + 'px';
		label.style.top = (rect.top + window.scrollY) + 'px';
		overlay.appendChild(label);

		elements.push({
			id: id,
			tag: el.tagName.toLowerCase(),
			text: text,
			role: el.getAttribute('role') || '',
			selector: selector
		});
		id++;
	}

	document.body.appendChild(overlay);
	return JSON.stringify(elements);
})();

function buildSelector(el) {
	if (el.id) return '#' + CSS.escape(el.id);
	const parent = el.parentElement;
	if (!parent) return el.tagName.toLowerCase();

	const siblings = Array.from(parent.children).filter(c => c.tagName === el.tagName);
	if (siblings.length === 1) {
		const parentSel = buildSelector(parent);
		return parentSel + ' > ' + el.tagName.toLowerCase();
	}
	const index = siblings.indexOf(el) + 1;
	const parentSel = buildSelector(parent);
	return parentSel + ' > ' + el.tagName.toLowerCase() + ':nth-child(' + (Array.from(parent.children).indexOf(el) + 1) + ')';
}
`

// parseElements parses the JSON string returned by the labeler JS
func parseElements(jsonStr string) []LabeledElement {
	var elements []LabeledElement
	if err := json.Unmarshal([]byte(jsonStr), &elements); err != nil {
		return []LabeledElement{}
	}
	return elements
}
