# Browser Capability

A Chrome browser runs inside the sandbox, visible on VNC. All navigation and
interaction happens through CDP (Chrome DevTools Protocol) — the same Chrome
window you see on screen is the one the tools control.

The browser uses **stealth patches** (navigator.webdriver, plugins, languages,
permissions, WebGL, hardwareConcurrency, deviceMemory, chrome.runtime) to
avoid bot detection. These are injected via CDP's `Page.addScriptToEvaluateOnNewDocument`
based on puppeteer-extra-plugin-stealth evasions. The stealth JavaScript source
is in `backend/internal/browser/stealth.go` and can be updated when upstream
evasions change.

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

### find_element_visual
**Visual grounding fallback** — locate an element by natural-language description when DOM tools fail. Uses MCP `ground_ui` (ShowUI-2B vision model).

**WHEN TO USE THIS (strict):**
- Canvas / WebGL apps (Excalidraw, Figma, Google Maps, video players) — no DOM nodes exist inside the canvas
- Image maps, SVG icons without text, screenshots embedded as images
- Heavily obfuscated SPAs where class names are random and `find_element` can't match
- After `find_element` / `snapshot_a11y` / `evaluate_js` returned nothing useful

**WHEN NOT TO USE THIS:**
- Ordinary HTML forms, buttons, inputs — use `find_element` instead
- Pages where `snapshot_a11y` already lists the target
- As a substitute for `browse_to` or `read_page`

Parameters:
- `query` (required) — specific description: "the red rectangle tool icon in the left toolbar", NOT just "button"
- `action` (optional) — `"click"` to dispatch a CDP mouse click at the returned pixel coordinates

Returns: `{x, y, width, height, x_norm, y_norm}` — viewport pixel coordinates plus normalized [0..1] values.

Examples:
```
find_element_visual({query: "the green color picker swatch in the toolbar"})
find_element_visual({query: "the play button on the video player", action: "click"})
find_element_visual({query: "the canvas drawing area center"})
```

**FALLBACK PROTOCOL:** Try `find_element` → `snapshot_a11y` → `evaluate_js` first. If all return nothing useful, use `find_element_visual`. Do NOT guess coordinates.

### snapshot_a11y
Get the accessibility tree of the current page — lists all interactive elements with
their ARIA role, name, and CSS selector. Use this to discover what's on the page before
interacting.

No parameters required.

### scroll_page
Scroll the current page up or down by one viewport.

Parameter: `direction` (optional) — `"up"` or `"down"` (default: `"down"`).

Use cases: reveal lazy-loaded content, bring elements into the viewport before clicking, paginate through infinite-scroll feeds.

### read_page
Extract structured content from the current page: title, URL, visible text, images (with `src` + `alt`), links (with text + URL).

Use this when you need page **data** (not a screenshot) — e.g., to find all links matching a pattern, extract article text, or inventory the images on the page.

### evaluate_js
Execute arbitrary JavaScript in the page and return the result. Use this to:
- Extract data not exposed by other tools (computed styles, hidden form values, JSON embedded in `<script>` tags)
- Dispatch synthetic events (mouseover, mouseenter, dragstart) — see "Interaction patterns" below
- Pierce shadow DOM or iframes when DOM tools can't reach inside them
- Read `window.*` globals set by the page's own scripts

Parameter: `script` (required) — a JavaScript expression that returns a value.

Example:
```
evaluate_js({script: "document.querySelectorAll('a[href*=\"/blog/\"]').length"})
```

### download_file
Download a file to the sandbox workspace. Returns the file path + size.

Use this instead of `bash + curl/wget` so the file lands in a known location the rest of the tools can reference.

Parameters: `url` (required), `path` (optional — default: workspace root with the URL's basename).

### observe
Combined capture: screenshot + element list + AI vision description in one call. Useful as a single-shot "what's going on" probe when you're disoriented.

### search_web
Search the web (via the browser's default search engine) and return result titles + URLs. Useful when you don't have a target URL yet.

### get_cookies / set_cookie / clear_cookies
Read, write, or clear the browser's cookie jar for the current page.

### get_storage / set_storage / clear_storage
Read, write, or clear `localStorage` for the current page. Useful for sites that store auth tokens or UI state in localStorage rather than cookies.

### browser_screenshot
Take a screenshot of the current browser page. The image is automatically described
by the vision system — you'll see a text description of what's on screen.
Use this to visually verify page state, check layouts, or confirm actions worked.

No parameters required.

### select_option
Select an option from a `<select>` dropdown by value or visible text.

Parameters:
- `selector` (required) — CSS selector for the `<select>` element
- `value` (optional) — select by the option's value attribute
- `label` (optional) — select by the option's visible text

Examples:
```
select_option({selector: "#country", value: "CA"})
select_option({selector: "#country", label: "Canada"})
```

### upload_file
Upload a file to a file input element using CDP (bypasses browser security restrictions).

Parameters:
- `file_path` (required) — absolute path to the file in the sandbox filesystem
- `selector` (required) — CSS selector for the `<input type="file">` element

Example:
```
upload_file({file_path: "/sandbox/workspace/document.pdf", selector: "#upload"})
```

### inject_file
Write a file (base64-encoded) into the sandbox filesystem. Use this when you need to upload a file that doesn't exist in the sandbox yet (e.g., an image, PDF, CSV, or archive the page is asking for).

Parameters:
- `dest_path` (required) — destination path in the sandbox (e.g., '/sandbox/workspace/document.pdf')
- `content_base64` (required) — base64-encoded content of the file

Example:
```
inject_file({dest_path: "/sandbox/workspace/document.pdf", content_base64: "JVBERi0xLjcN..."})
```

### credential_get
Get saved login credentials for a service from environment variables. Use this to log into any service without hardcoding credentials.

Parameters:
- `service` (required) — service name. The tool looks up `{SERVICE}_USERNAME` and `{SERVICE}_PASSWORD` (uppercase). Case-insensitive.

Example:
```
credential_get({service: "acme"})
→ {username: "john@example.com", password: "***", found: true}
```

### user_profile
Read the user's profile information (name, email, phone, and any other fields they've chosen to persist) from a JSON config file. The config is loaded from `PROFILE_PATH` env var, `~/.pux/user_profile.json`, or the project root.

No parameters required.

Example profile format (`~/.pux/user_profile.json`):
```json
{
  "name": "John Smith",
  "email": "john.smith@example.com",
  "phone": "+1-555-123-4567",
  "custom_fields": { "...": "any key/value pairs the user wants to expose" }
}
```

### save_session
Save the current browser session (cookies + localStorage) to a file for later restoration.

Parameters:
- `path` (optional) — file path in sandbox to save session data (default: /tmp/browser-session.json)

### restore_session
Restore a previously saved browser session from a file.

Parameters:
- `path` (optional) — file path in sandbox to read session data from (default: /tmp/browser-session.json)

## Interaction patterns

These are **general patterns** that apply to any page. They are not site-specific
recipes — read them as primitives and compose them to fit the page in front of you.

### Hover / flyout menus
Many menus only appear after a real `mouseover` event. `find_element({action:"click"})`
does not synthesize one, so flyouts stay hidden. Dispatch it explicitly:

```
evaluate_js({script: `
  const el = document.querySelector('#menu-item');
  el.dispatchEvent(new MouseEvent('mouseover', {bubbles:true}));
  el.dispatchEvent(new MouseEvent('mouseenter', {bubbles:false}));
`})
```

Then call `snapshot_a11y` or `browser_screenshot` to see the now-visible submenu.

### Selectors for elements with numeric or special-character IDs

HTML5 allows IDs like `5`, `1.2.3`, or `foo:bar`, but CSS selectors require
escaping (`#5` is invalid; `[id="5"]` or `#\35` work). `find_element({selector:"#5"})`
silently fails because `document.querySelector("#5")` throws a SyntaxError
inside the JS fallback path, and the upstream CDP `page.select("#5")` rejects
it too. Use attribute-selector form for any ID that starts with a digit or
contains punctuation:

```
find_element({selector:'[id="5"]'})
find_element({selector:'[id="1.2.3"]'})
```

This is the only reliable way to click digit-ID calculator buttons, grid
cells in some data tables, and sections in legacy anchors.

### Drag and drop
HTML5 drag events need a `DataTransfer` object — `find_element` can't synthesize
that. Do it in JS:

```
evaluate_js({script: `
  const src = document.querySelector('#drag-source');
  const dst = document.querySelector('#drop-target');
  const dt = new DataTransfer();
  src.dispatchEvent(new DragEvent('dragstart', {bubbles:true, dataTransfer:dt}));
  dst.dispatchEvent(new DragEvent('drop', {bubbles:true, dataTransfer:dt}));
  src.dispatchEvent(new DragEvent('dragend', {bubbles:true, dataTransfer:dt}));
`})
```

For sliders and canvas-based dragging, use `find_element_visual` to get pixel
coordinates and dispatch `pointerdown` / `pointermove` / `pointerup` via JS.

### Shadow DOM
Web components (custom elements, modern design systems) hide their internals
inside a shadow root. `find_element` and `snapshot_a11y` cannot pierce shadow
boundaries — query them via JS:

```
evaluate_js({script: `
  const host = document.querySelector('my-widget');
  return host.shadowRoot.querySelector('button').textContent;
`})
```

If the shadow mode is `closed`, you usually cannot reach inside at all — fall
back to `find_element_visual` (pixel coordinates) + synthetic click at those
coordinates.

### Iframes
Content inside an `<iframe>` is a separate document. DOM tools cannot cross
the frame boundary, but `evaluate_js` can — it walks `document.querySelectorAll('iframe')`
and inspects `.contentWindow.document`. Cross-origin iframes throw a SecurityError;
for those, the only path is to navigate the top-level page to the iframe's `src`
directly.

```
evaluate_js({script: `
  const f = document.querySelector('iframe');
  return f.contentDocument.querySelector('button#submit').textContent;
`})
```

### Dialog boxes (alert / prompt / confirm)
The browser auto-dismisses `window.alert`/`confirm`/`prompt` dialogs (accept =
true / OK). If you're not sure, probe first:

```
evaluate_js({script: `typeof window.__lastDialogMessage !== 'undefined' ? window.__lastDialogMessage : 'none'`})
```

Modal **HTML** dialogs (e.g., `<dialog>` element, or a `<div role="dialog">`)
are not JS dialogs — handle them with the normal `find_element({action:"click"})`
on their close/confirm button.

### Infinite scroll / lazy-loaded lists
The page appends new items only as you approach the bottom. Loop:

1. `scroll_page({direction:"down"})`
2. `snapshot_a11y` (or `read_page`) — record the new items
3. Stop when two consecutive scrolls return no new items

Set a hard cap (e.g., 50 iterations) so a broken "load more" endpoint doesn't
loop forever.

### Multi-step forms (wizards)
Each "Next" button typically validates the current step before revealing the
next. Pattern:

1. Fill the visible step with `find_element({action:"type"})` + `select_option`
2. Click "Next" with `find_element({action:"click"})`
3. **Verify** with `browser_screenshot` — did a new step appear, or did a
   validation error stay on the current one?
4. If validation error: read it, fix the offending field, click "Next" again.
5. Repeat until the final "Submit" succeeds and the URL changes.

Never assume a click worked — always re-snapshot and check.

### Bot detection / Cloudflare Turnstile

Two interchangeable Chrome backends sit behind a waterfall. `/navigate`
auto-routes — you don't pick the mode.

The response carries `mode` and `waterfall` fields:

- `mode: "attach"`, `waterfall: "attach"` — Mode A (long-lived Chrome) landed clean. Default path.
- `mode: "stealth"`, `waterfall: "attach_then_stealth"` — Mode A hit a CF challenge; the driver spawned Mode Stealth (rotated fingerprint) and re-drove the URL.
- `mode: "stealth"`, `waterfall: "stealth_cached"` — domain remembered as CF; skipped Mode A.
- `mode: "stealth"`, `waterfall: "stealth_blocked"`, `cf_still_present: true` — **both backends lost**. Don't keep retrying the URL. Move to **pre-extracted cookies** (see "Pulling host-browser cookies" below).

`/solve_captcha` (`POST /api/sandbox/{id}/sb/solve_captcha`) dispatches SeleniumBase's solver for CF Turnstile / reCAPTCHA / hCaptcha / DataDome / FriendlyCaptcha. It works on test sitekeys but **does not clear real CF Turnstile today** — don't waste a call hoping it will.

For fingerprinted flows (DataDome, PerimeterX, checkout bots):

- `humanlike: true` on `find_element({action:"click"})` moves the cursor along a Bezier curve before the press. Default off (~250ms cost). Turn on for these flows.
- `evaluate_js({script: "await new Promise(r => setTimeout(r, 1500))"})` between actions.
- If Turnstile appears and the page hasn't loaded real content after 3 scroll/wait cycles, report it back. Don't fight server-side-gated widgets.

### Cross-tab / popup windows
Links with `target="_blank"` open a new tab. The browser driver attaches to the
first tab by default. To switch context, use `evaluate_js` to read
`window.open()`'s returned handle, or navigate the current tab to the popup's
URL directly (simpler — popups close when their opener is reused).

### Pulling host-browser cookies into the sandbox
The sandbox container cannot read the host browser's cookie DB or keyring, so
the extraction happens host-side. The flow:

1. **Host-side** (once per session, via `bootstrap.sh`): run
   `extract_browser_cookies.py --browser brave --domain example.com --out data/.browser-session-example.com.json`
2. The JSON is bind-mounted into the sandbox at `/sandbox/workspace/data/.browser-session-example.com.json`.
3. In the agent: `restore_session({path: "/sandbox/workspace/data/.browser-session-example.com.json"})`
   applies the cookies + localStorage to the browser.

The same script supports chrome, brave, edge, chromium, opera, opera_gx,
vivaldi, and firefox — flatpak installs auto-detected via `FLATPAK_PATHS`.

#### Wiring it into any org (the standard recipe)

Browser-using orgs declare a `[[sandbox.bootstrap.host_setup]]` block in
their `org.toml`. The bootstrap template handles venv creation, dep install,
check-mode dispatch, and bind-mounting the output file. Copy-paste recipe:

```toml
[sandbox]
tier = "standard"
# ...image, runtime_class, resources, env...

pip_packages = ["browser-cookie3", "pycryptodome", "jeepney"]

[[sandbox.bootstrap.host_setup]]
name = "linkedin_cookies"
description = "Extract LinkedIn cookies from the host browser (cookie DB + GNOME keyring not reachable from inside the container)"
script = "@shared/sandbox/extract_browser_cookies.py"
args = ["--browser", "brave", "--domain", "linkedin.com", "--out", "data/.browser-session-linkedin.com.json"]
check_args = ["--browser", "brave", "--domain", "linkedin.com", "--check"]
python_deps = ["browser-cookie3", "pycryptodome", "jeepney"]
```

For multiple domains, declare one `[[sandbox.bootstrap.host_setup]]` block
per domain. Each will be checked + extracted independently during
`./bootstrap.sh --check` and `./bootstrap.sh` respectively.

In-sandbox code can resolve the canonical session path via the shared
`paths` module:

```python
from paths import browser_session
session_file = browser_session("linkedin.com")
# → /sandbox/workspace/data/.browser-session-linkedin.com.json
```

Or call `restore_session` directly with the absolute path. Either way the
session JSON has the shape `restore_session` expects (cookies + localStorage
+ url + saved_at + source + domain).

## Workflow
1. Prepare: call `inject_file` to place any files the page will need into the sandbox
2. Prepare: call `user_profile` to get profile info, `credential_get` for login credentials
3. Navigate: call `browse_to` with the target URL
4. Discover: call `snapshot_a11y` to find interactive elements
5. Interact: call `find_element` with action="click" or action="type"
6. For dropdowns: call `select_option` with the selector and value/label
7. For file uploads: call `upload_file` with the file path and file input selector
8. Verify: call `browser_screenshot` to visually confirm the result
9. Persist: call `save_session` after login to avoid re-authenticating

## CRITICAL RULES
- ALWAYS call tools to interact with the browser — NEVER describe what you would do
- NEVER claim the browser is open without actually calling browse_to first
- NEVER use bash+curl to navigate — use browse_to instead
- If a tool returns an error, report the error honestly — do not fabricate results
- The browser starts on a blank page — you MUST navigate to a URL first
