package browser

import (
	"encoding/json"
)

// labelerJS is injected into every page to label interactive elements.
// Uses the Set-of-Mark (SoM) pattern: labels elements with numbered boxes,
// captures bounding box coordinates, and filters noise (nav, footer, etc.)
// so the agent model gets a clean, spatial representation of the page.
const labelerJS = `
(() => {
	// Remove any existing label overlay
	const existingOverlay = document.getElementById('__browser_label_overlay__');
	if (existingOverlay) existingOverlay.remove();

	const elements = [];
	// Only truly interactive elements — no divs, spans, imgs
	const interactiveSelectors = 'a, button, input, select, textarea, [role="button"], [role="link"], [type="submit"]';
	const nodes = document.querySelectorAll(interactiveSelectors);

	// Create overlay for visual labels (SoM annotations)
	const overlay = document.createElement('div');
	overlay.id = '__browser_label_overlay__';
	overlay.style.cssText = 'position:fixed;top:0;left:0;width:100%;height:100%;pointer-events:none;z-index:999999;';

	let id = 1;
	const seen = new Set();
	const vw = window.innerWidth;
	const vh = window.innerHeight;

	for (const el of nodes) {
		// Hard cap at 25 elements — prevents overwhelming the model
		if (id > 25) break;

		const rect = el.getBoundingClientRect();
		// Skip invisible elements
		if (rect.width === 0 || rect.height === 0) continue;
		// Skip tiny elements (likely hidden or decorative)
		if (rect.width < 10 && rect.height < 10) continue;
		// Skip elements outside viewport
		if (rect.bottom < 0 || rect.top > vh) continue;

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

		// Get element text (short)
		let text = '';
		if (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA') {
			text = el.placeholder || el.value || el.name || el.type || '';
		} else {
			text = (el.textContent || '').trim();
		}
		text = text.substring(0, 60).replace(/\s+/g, ' ');

		// Skip elements with no visible text AND not an input
		if (!text && el.tagName !== 'INPUT' && el.tagName !== 'SELECT') continue;

		// Add visual label (SoM annotation — red numbered box)
		const label = document.createElement('div');
		label.style.cssText = 'position:fixed;pointer-events:none;background:rgba(220,38,38,0.9);color:white;font-size:11px;font-family:monospace;padding:1px 4px;border-radius:2px;z-index:1000000;line-height:1.3;white-space:nowrap;font-weight:bold;';
		label.textContent = '' + id;
		label.style.left = rect.left + 'px';
		label.style.top = rect.top + 'px';
		overlay.appendChild(label);

		// Find nearest semantic parent (form, nav, main, header, footer, section)
		let parent = '';
		const containerTags = ['FORM', 'NAV', 'MAIN', 'HEADER', 'FOOTER', 'SECTION', 'ARTICLE', 'ASIDE', 'DIALOG'];
		let p = el.parentElement;
		while (p && p !== document.body) {
			if (containerTags.includes(p.tagName)) {
				parent = p.tagName.toLowerCase();
				break;
			}
			p = p.parentElement;
		}

		elements.push({
			id: id,
			tag: el.tagName.toLowerCase(),
			text: text,
			role: el.getAttribute('role') || '',
			selector: selector,
			x: Math.round(rect.left),
			y: Math.round(rect.top),
			w: Math.round(rect.width),
			h: Math.round(rect.height),
			parent: parent
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
