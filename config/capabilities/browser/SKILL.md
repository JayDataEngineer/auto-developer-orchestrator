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
upload_file({file_path: "/tmp/resume.pdf", selector: "#resume"})
```

### inject_file
Write a file (base64-encoded) into the sandbox filesystem. Use this when you need to upload a file that doesn't exist in the sandbox yet (e.g., a resume PDF from the user's profile).

Parameters:
- `dest_path` (required) — destination path in the sandbox (e.g., '/sandbox/workspace/resume.pdf')
- `content_base64` (required) — base64-encoded content of the file

Example:
```
inject_file({dest_path: "/sandbox/workspace/resume.pdf", content_base64: "JVBERi0xLjcN..."})
```

### credential_get
Get saved login credentials for a service from environment variables. Use this to log into job portals without hardcoding credentials.

Parameters:
- `service` (required) — service name (e.g., 'linkedin', 'indeed', 'glassdoor'). Case-insensitive.

Looks up `{SERVICE}_USERNAME` and `{SERVICE}_PASSWORD` (or `_EMAIL` and `_PASS`) env vars.

Example:
```
credential_get({service: "linkedin"})
→ {username: "john@example.com", password: "***", found: true}
```

### user_profile
Read your profile information (name, email, phone, resume path, skills, work history) from a saved JSON config file. The config is loaded from `PROFILE_PATH` env var, `~/.pux/user_profile.json`, or the project root.

No parameters required.

Example profile format (`~/.pux/user_profile.json`):
```json
{
  "name": "John Smith",
  "email": "john.smith@example.com",
  "phone": "+1-555-123-4567",
  "resume_path": "/sandbox/workspace/resume.pdf",
  "skills": ["Go", "Python", "React"],
  "work_history": [{"company": "Tech Corp", "title": "Software Engineer"}]
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

## Workflow
1. Prepare: call `inject_file` to place any needed files (resume PDF, etc.) into the sandbox
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
