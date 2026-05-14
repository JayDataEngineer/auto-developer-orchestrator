# Browser Capability

A persistent SeleniumBase browser runs on localhost:9876 inside the sandbox.

## Available Functions

All commands via curl:
`curl -s -X POST http://localhost:9876/<action> -H 'Content-Type: application/json' -d '<json>'`

navigate(url): Go to a URL
  Wait for page load before reading the DOM.
  Returns status code and final URL (follows redirects).

click_element(selector): Click an element by CSS selector
  Prefer specific selectors over broad ones.
  If click fails, the page may have changed — re-read the DOM.

type_text(selector, text): Type text into a field
  Clears existing content before typing.
  For forms, fill each field then click the submit button.

read_dom(): Get the current page DOM as structured text
  ALWAYS call this before clicking to identify correct selectors.
  Returns elements with selectors you can use in click/type calls.

screenshot(): Capture a screenshot of the current page
  Expensive — prefer read_dom() for state checks.

## Workflow
1. navigate() to the target page
2. read_dom() to find elements and selectors
3. click_element() or type_text() to interact
4. read_dom() again to verify changes worked

## Tips
- The browser session is stateful — cookies and login state persist between commands
- For downloads, check /sandbox/workspace/ after triggering
- If a page is slow, wait a moment then re-read the DOM
- For forms, type into each field then click submit — don't use keyboard shortcuts
