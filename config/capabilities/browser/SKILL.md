# Browser Capability

A headless Chrome runs inside the sandbox via CDP (Chrome DevTools Protocol).
The browser session persists across tool calls — cookies and login state are preserved.

## Tools

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

### bash (for navigation)
Navigate using the internal SeleniumBase server:
```bash
curl -s -X POST http://localhost:9876/navigate -H 'Content-Type: application/json' -d '{"url": "https://www.google.com"}'
```

Other sb_server endpoints via bash:
- `curl -s http://localhost:9876/read_dom` — get page structure
- `curl -s http://localhost:9876/screenshot` — capture screenshot
- `curl -s -X POST http://localhost:9876/click_element -H 'Content-Type: application/json' -d '{"selector": "#btn"}'`
- `curl -s -X POST http://localhost:9876/type_text -H 'Content-Type: application/json' -d '{"selector": "#input", "text": "hello"}'`

## Workflow
1. Navigate: use bash+curl to `http://localhost:9876/navigate` with target URL
2. Discover: call `snapshot_a11y` or bash+curl to `read_dom` to find interactive elements
3. Interact: call `find_element` with action="click" or action="type"
4. Verify: call `snapshot_a11y` again to check the result

## CRITICAL RULES
- ALWAYS call tools to interact with the browser — NEVER describe what you would do
- NEVER claim the browser is open without actually calling navigate first
- If a tool returns an error, report the error honestly — do not fabricate results
- The browser starts on a blank page — you MUST navigate to a URL first
