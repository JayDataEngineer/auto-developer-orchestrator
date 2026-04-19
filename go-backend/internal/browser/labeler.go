package browser

import (
	"encoding/json"
)

// labelerJS is injected into every page to label interactive elements.
// Uses the Set-of-Mark (SoM) pattern: labels elements with numbered boxes,
// captures bounding box coordinates, and filters noise (nav, footer, etc.)
// so the agent model gets a clean, spatial representation of the page.
//
// Enhancements (Track C):
//   - ARIA role and label extraction (shown as aria="..." in tag)
//   - Scroll container detection (|SCROLL| tag suffix)
//   - JS click event listener detection (+ suffix on tag)
//   - Password field masking (value replaced with bullets)
//   - Shadow DOM traversal (recursive into shadow roots)
const labelerJS = `
(() => {
	// Remove any existing label overlay
	const existingOverlay = document.getElementById('__browser_label_overlay__');
	if (existingOverlay) existingOverlay.remove();

	const elements = [];
	// Only truly interactive elements — no divs, spans, imgs
	const interactiveSelectors = 'a, button, input, select, textarea, [role="button"], [role="link"], [type="submit"]';

	// Create overlay for visual labels (SoM annotations)
	const overlay = document.createElement('div');
	overlay.id = '__browser_label_overlay__';
	overlay.style.cssText = 'position:fixed;top:0;left:0;width:100%;height:100%;pointer-events:none;z-index:999999;';

	let id = 1;
	const seen = new Set();
	const vw = window.innerWidth;
	const vh = window.innerHeight;

	// ── Scroll container detection ──────────────────────────────
	function isScrollable(el) {
		try {
			const style = getComputedStyle(el);
			return ((style.overflowY === 'auto' || style.overflowY === 'scroll') && el.scrollHeight > el.clientHeight) ||
				((style.overflowX === 'auto' || style.overflowX === 'scroll') && el.scrollWidth > el.clientWidth);
		} catch (e) {
			return false;
		}
	}

	// ── JS click event listener detection ───────────────────────
	// getEventListeners is Chrome DevTools only, so we use attribute-based detection
	function hasClickHandler(el) {
		return el.onclick !== null || el.getAttribute('onclick') !== null;
	}

	// ── Build tag with annotations for display ──────────────────
	// Returns { base: "button", display: "button+" }
	function buildTagInfo(el) {
		const base = el.tagName.toLowerCase();
		let display = base;

		// Scroll detection
		if (isScrollable(el)) {
			display = display + '|SCROLL|';
		}

		// Click handler detection
		if (hasClickHandler(el)) {
			display = display + '+';
		}

		return { base: base, display: display };
	}

	// ── Build ARIA annotation string ────────────────────────────
	function buildARIA(el) {
		const parts = [];
		const ariaRole = el.getAttribute('role') || '';
		const ariaLabel = el.getAttribute('aria-label') || '';
		const ariaLabelledBy = el.getAttribute('aria-labelledby') || '';
		const baseTag = el.tagName.toLowerCase();

		// Include role only when it differs from the semantic tag
		if (ariaRole && ariaRole !== baseTag) parts.push('role=' + ariaRole);
		if (ariaLabel) parts.push('label=' + ariaLabel);
		if (ariaLabelledBy) parts.push('labelledby=' + ariaLabelledBy);

		return parts.join(' ');
	}

	// ── Core element collection ─────────────────────────────────
	function processElement(el) {
		// Hard cap at 25 elements — prevents overwhelming the model
		if (id > 25) return false;

		const rect = el.getBoundingClientRect();
		// Skip invisible elements
		if (rect.width === 0 || rect.height === 0) return true;
		// Skip tiny elements (likely hidden or decorative)
		if (rect.width < 10 && rect.height < 10) return true;
		// Skip elements outside viewport
		if (rect.bottom < 0 || rect.top > vh) return true;

		// Build unique selector
		let selector = '';
		if (el.id) {
			selector = '#' + CSS.escape(el.id);
		} else {
			selector = buildSelector(el);
		}
		// Skip duplicate selectors
		if (seen.has(selector)) return true;
		seen.add(selector);

		// Get element text (short)
		let text = '';
		if (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA') {
			// Password field masking
			if (el.type === 'password') {
				text = '\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022';
			} else {
				text = el.placeholder || el.value || el.name || el.type || '';
			}
		} else {
			text = (el.textContent || '').trim();
		}
		text = text.substring(0, 60).replace(/\s+/g, ' ');

		// Skip elements with no visible text AND not an input
		if (!text && el.tagName !== 'INPUT' && el.tagName !== 'SELECT') return true;

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

		// Build ARIA info string
		const aria = buildARIA(el);
		const tagInfo = buildTagInfo(el);

		elements.push({
			id: id,
			tag: tagInfo.base,
			display_tag: tagInfo.display,
			text: text,
			role: el.getAttribute('role') || '',
			aria: aria,
			selector: selector,
			x: Math.round(rect.left),
			y: Math.round(rect.top),
			w: Math.round(rect.width),
			h: Math.round(rect.height),
			parent: parent
		});
		id++;
		return true; // continue
	}

	// ── Shadow DOM traversal ────────────────────────────────────
	function collectFromShadowRoots(root) {
		// Collect interactive elements from the root itself
		const nodes = root.querySelectorAll(interactiveSelectors);
		for (const el of nodes) {
			if (!processElement(el)) break; // hit cap
			// Recurse into shadow roots of found elements
			if (el.shadowRoot) {
				collectFromShadowRoots(el.shadowRoot);
			}
		}
		// Also check all elements for shadow roots (not just interactive ones)
		const allElements = root.querySelectorAll('*');
		for (const el of allElements) {
			if (id > 25) break;
			if (el.shadowRoot) {
				collectFromShadowRoots(el.shadowRoot);
			}
		}
	}

	// ── Main collection from document ───────────────────────────
	const mainNodes = document.querySelectorAll(interactiveSelectors);
	for (const el of mainNodes) {
		if (!processElement(el)) break;
	}

	// ── Also traverse shadow DOMs ───────────────────────────────
	if (id <= 25) {
		collectFromShadowRoots(document);
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
