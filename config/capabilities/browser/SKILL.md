# Browser Capability

A Chrome browser runs inside the sandbox, visible on VNC. All navigation and
interaction happens through CDP (Chrome DevTools Protocol) — the same Chrome
window you see on screen is the one the tools control.

## Tools

### browse_to
Navigate to a URL. Returns page title, URL, and a preview of page content.
**ALWAYS use this to navigate — do NOT use bash+curl.**

Parameter: `url` (required) — the full URL to navigate to.

Example:
```
browse_to({url: "https://www.google.com"})
```

### find_element
Find a page element by semantic criteria and optionally interact with it.
**ALWAYS call this to interact with the page — never describe what you would do.**

Parameters: role, name, label, text, placeholder, selector, action, type_text, submit

Actions:
- Find only (no action): returns the element's properties
- `action: "click"` — click the found element
- `action: "type"` — type text into the found element (requires type_text parameter)
- `submit: true` — press Enter after typing

Examples:
```
find_element({selector: "input[name='q']", action: "type", type_text: "hello world", submit: true})
find_element({role: "button", name: "Search", action: "click"})
find_element({selector: "a[href='/login']"})
```

### snapshot_a11y
Get the accessibility tree of the current page — lists all interactive elements with
their ARIA role, name, and CSS selector. Use this to discover what's on the page before
interacting.

### browser_screenshot
Take a screenshot of the current browser page. The image is automatically described
by the vision system — you'll see a text description of what's on screen.
Use this to visually verify page state, check layouts, or confirm actions worked.

No parameters required.

## Workflow
1. Navigate: call `browse_to` with the target URL
2. Discover: call `snapshot_a11y` to find interactive elements
3. Interact: call `find_element` with action="click" or action="type"
4. Verify: call `browser_screenshot` to visually confirm the result

## CRITICAL RULES
- ALWAYS call tools to interact with the browser — NEVER describe what you would do
- NEVER claim the browser is open without actually calling browse_to first
- NEVER use bash+curl to navigate — use browse_to instead
- If a tool returns an error, report the error honestly — do not fabricate results
- The browser starts on a blank page — you MUST navigate to a URL first
