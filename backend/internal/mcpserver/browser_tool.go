// browser_tool.go exposes the sandbox's existing sb_server (persistent
// SeleniumBase HTTP service inside the container) as MCP tools.
//
// The sandbox image ships /usr/local/bin/sb_server.py which supervisord
// runs on port 9876 INSIDE the container (namespace-isolated from the
// host MCP server's port — no conflict). It exposes a rich browser API:
// navigate, click, type, screenshot, evaluate, etc. State persists across
// calls because the Python process holds the SeleniumBase session.
//
// We wrap the 5 most useful endpoints as distinct MCP tools. The model
// gets typed schemas; internally each tool is a curl call to sb_server.
//
// Why not one generic "browser" tool with a `method` param? Two reasons:
// (1) per-tool schemas are clearer for the model than a union type,
// (2) the MCP spec favors many narrow tools over one wide tool.
package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// sbServerAddr is the in-container address of sb_server.py. The sandbox
// listens on 9876 by supervisord config. localhost (not the host-side
// forwarded port) because we exec inside the container.
const sbServerAddr = "http://127.0.0.1:9876"

// BrowserToolConfig tunes browser tool behavior. Zero-value defaults are
// sensible; the only knob operators typically touch is timeout (vision-
// heavy pages may need longer).
type BrowserToolConfig struct {
	// Timeout caps each sb_server round-trip. Default 60s — navigation
	// to heavy sites can take 10-20s, plus SoM labeling time.
	Timeout time.Duration
}

// browserBase is the shared scaffolding for all browser tools. Each
// concrete tool sets endpoint + arg-marshaler + result-parser.
type browserBase struct {
	exec    SandboxExecutor
	timeout time.Duration
}

func newBrowserBase(exec SandboxExecutor, cfg BrowserToolConfig) browserBase {
	t := cfg.Timeout
	if t == 0 {
		t = 60 * time.Second
	}
	return browserBase{exec: exec, timeout: t}
}

// postJSON hits sb_server with the given endpoint + JSON body, returns
// the response body. Errors are wrapped with the endpoint name so the
// tool result makes the failing call obvious.
func (b *browserBase) postJSON(ctx context.Context, endpoint, body string, extraHeaders map[string]string) (string, error) {
	// Build curl command. -s (silent), -S (show errors), --max-time (cap,
	// rounded UP so sub-second timeouts aren't truncated to 0),
	// -X POST, -H Content-Type, -d body.
	maxTimeSec := max(1, int(math.Ceil(b.timeout.Seconds())))
	parts := []string{
		"curl -s -S",
		fmt.Sprintf("--max-time %d", maxTimeSec),
		"-X POST",
		fmt.Sprintf("%s%s", sbServerAddr, endpoint),
		"-H 'Content-Type: application/json'",
	}
	for k, v := range extraHeaders {
		parts = append(parts, fmt.Sprintf("-H %s", shQVision(k+": "+v)))
	}
	if body != "" {
		parts = append(parts, "-d", shQVision(body))
	}
	cmd := strings.Join(parts, " ")

	// Context timeout matches the curl flag. We round up at the curl level
	// (sub-second becomes 1s) so this Go timeout is always ≥ curl's. The +1s
	// budget covers curl's own teardown after hitting --max-time.
	execCtx, cancel := context.WithTimeout(ctx, b.timeout+time.Second)
	defer cancel()

	out, err := b.exec.Exec(execCtx, cmd)
	if execCtx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("browser %s: timed out after %v", endpoint, b.timeout)
	}
	if err != nil {
		return out, fmt.Errorf("browser %s: exec failed: %w (output: %s)", endpoint, err, tailOutput(out, 400))
	}
	return out, nil
}

// parseSBResponse extracts the result/data field from sb_server's response.
// sb_server returns JSON like {ok: true, ...fields} or {ok: false, error:...}.
// We surface the whole payload to the model — different endpoints return
// different shapes (some have screenshot, some have links, some have text)
// and a generic extractor would lose information.
func parseSBResponse(raw string) (map[string]any, error) {
	var parsed map[string]any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, fmt.Errorf("malformed sb_server response: %w (raw=%q)", err, tailOutput(raw, 400))
	}
	return parsed, nil
}

// ── Tool 1: browser_navigate ─────────────────────────────────────────────

// BrowserNavigateTool opens a URL in the sandbox browser. Returns page
// metadata + initial screenshot (base64 PNG). The browser session persists
// across tool calls — subsequent click/type/screenshot calls operate on
// the page this tool navigated to.
type BrowserNavigateTool struct{ base browserBase }

func NewBrowserNavigateTool(exec SandboxExecutor, cfg BrowserToolConfig) *BrowserNavigateTool {
	return &BrowserNavigateTool{base: newBrowserBase(exec, cfg)}
}
func (t *BrowserNavigateTool) Name() string { return "browser_navigate" }
func (t *BrowserNavigateTool) Description() string {
	return "Open a URL in the sandbox's persistent Chrome. " +
		"Returns page title, URL, text snippet, and a base64 screenshot with " +
		"Set-of-Marks labels on interactive elements. " +
		"The session persists — subsequent browser_click / browser_type / " +
		"browser_screenshot calls operate on this page until you navigate again."
}
func (t *BrowserNavigateTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"required": ["url"],
		"properties": {
			"url": {"type": "string", "description": "Absolute URL including scheme (https://example.com)"}
		}
	}`)
}
func (t *BrowserNavigateTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	url, _ := args["url"].(string)
	if url == "" {
		return nil, core.NewToolError("browser_navigate", "url is required")
	}
	body := fmt.Sprintf(`{"url":%s}`, mustJSONString(url))
	out, err := t.base.postJSON(ctx, "/navigate", body, nil)
	if err != nil {
		return nil, err
	}
	return parseSBResponse(out)
}

// ── Tool 2: browser_click ────────────────────────────────────────────────

// BrowserClickTool clicks an element by SoM label (the numbered boxes
// from /navigate) or by CSS selector.
type BrowserClickTool struct{ base browserBase }

func NewBrowserClickTool(exec SandboxExecutor, cfg BrowserToolConfig) *BrowserClickTool {
	return &BrowserClickTool{base: newBrowserBase(exec, cfg)}
}
func (t *BrowserClickTool) Name() string { return "browser_click" }
func (t *BrowserClickTool) Description() string {
	return "Click an element on the current page. Pass either a SoM label " +
		"(integer from the labeled screenshot) or a CSS selector string. " +
		"Returns the post-click page state (URL, title, screenshot)."
}
func (t *BrowserClickTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"index": {"type": "integer", "description": "SoM label (the numbered box on interactive elements from the last screenshot). Sent as 'index' to sb_server."},
			"selector": {"type": "string", "description": "CSS selector (e.g. 'button#submit'). Used when index is omitted."}
		},
		"oneOf": [{"required": ["index"]}, {"required": ["selector"]}]
	}`)
}
func (t *BrowserClickTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	body, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("browser_click: marshal args: %w", err)
	}
	out, err := t.base.postJSON(ctx, "/click", string(body), nil)
	if err != nil {
		return nil, err
	}
	return parseSBResponse(out)
}

// ── Tool 3: browser_type ─────────────────────────────────────────────────

// BrowserTypeTool types text into an input. Clears the field first by
// default (sb_server behavior).
type BrowserTypeTool struct{ base browserBase }

func NewBrowserTypeTool(exec SandboxExecutor, cfg BrowserToolConfig) *BrowserTypeTool {
	return &BrowserTypeTool{base: newBrowserBase(exec, cfg)}
}
func (t *BrowserTypeTool) Name() string { return "browser_type" }
func (t *BrowserTypeTool) Description() string {
	return "Type text into a form field on the current page. " +
		"Uses CDP character-by-character input (React-safe — fires real DOM events). " +
		"Pass either a SoM label or CSS selector to identify the target input."
}
func (t *BrowserTypeTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"required": ["text"],
		"properties": {
			"text": {"type": "string", "description": "Text to type into the field"},
			"index": {"type": "integer", "description": "SoM label of the target input. Sent as 'index' to sb_server."},
			"selector": {"type": "string", "description": "CSS selector of the target input"}
		},
		"oneOf": [{"required": ["text", "index"]}, {"required": ["text", "selector"]}]
	}`)
}
func (t *BrowserTypeTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if _, ok := args["text"].(string); !ok {
		return nil, core.NewToolError("browser_type", "text is required")
	}
	if _, hasLabel := args["index"]; !hasLabel {
		if _, hasSel := args["selector"].(string); !hasSel {
			return nil, core.NewToolError("browser_type", "either index or selector is required")
		}
	}
	body, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("browser_type: marshal args: %w", err)
	}
	out, err := t.base.postJSON(ctx, "/type", string(body), nil)
	if err != nil {
		return nil, err
	}
	return parseSBResponse(out)
}

// ── Tool 4: browser_screenshot ───────────────────────────────────────────

// BrowserScreenshotTool captures the current page state as a base64 PNG
// with SoM labels on interactive elements. Free — doesn't navigate.
type BrowserScreenshotTool struct{ base browserBase }

func NewBrowserScreenshotTool(exec SandboxExecutor, cfg BrowserToolConfig) *BrowserScreenshotTool {
	return &BrowserScreenshotTool{base: newBrowserBase(exec, cfg)}
}
func (t *BrowserScreenshotTool) Name() string { return "browser_screenshot" }
func (t *BrowserScreenshotTool) Description() string {
	return "Capture the current browser state as a labeled screenshot. " +
		"Returns base64 PNG + SoM-numbered boxes on interactive elements. " +
		"Use this to re-orient after page updates, or to get a fresh set of " +
		"label numbers for clicking."
}
func (t *BrowserScreenshotTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (t *BrowserScreenshotTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	out, err := t.base.postJSON(ctx, "/read", "{}", nil)
	if err != nil {
		return nil, err
	}
	return parseSBResponse(out)
}

// ── Tool 5: browser_evaluate ─────────────────────────────────────────────

// BrowserEvaluateTool runs arbitrary JavaScript on the page and returns
// the result. Power tool — escape hatch when the typed tools don't fit.
type BrowserEvaluateTool struct{ base browserBase }

func NewBrowserEvaluateTool(exec SandboxExecutor, cfg BrowserToolConfig) *BrowserEvaluateTool {
	return &BrowserEvaluateTool{base: newBrowserBase(exec, cfg)}
}
func (t *BrowserEvaluateTool) Name() string { return "browser_evaluate" }
func (t *BrowserEvaluateTool) Description() string {
	return "Evaluate JavaScript on the current page, return the result. " +
		"Power-tool escape hatch when navigate/click/type/screenshot don't fit " +
		"(e.g. read window.__NEXT_DATA__, scroll to specific element, fetch XHR). " +
		"The script runs in the page context — same-origin policy applies."
}
func (t *BrowserEvaluateTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"required": ["code"],
		"properties": {
			"code": {"type": "string", "description": "JavaScript expression to evaluate. Use 'return' for explicit value (e.g. 'return document.title')"}
		}
	}`)
}
func (t *BrowserEvaluateTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	code, _ := args["code"].(string)
	if code == "" {
		return nil, core.NewToolError("browser_evaluate", "code is required")
	}
	body := fmt.Sprintf(`{"code":%s}`, mustJSONString(code))
	out, err := t.base.postJSON(ctx, "/evaluate", body, nil)
	if err != nil {
		return nil, err
	}
	return parseSBResponse(out)
}

// mustJSONString JSON-encodes a string. Used for embedding strings into
// hand-built JSON payloads (we don't full-marshal because sb_server
// accepts extra fields the model didn't set).
func mustJSONString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		// Should never happen for a plain string — fall back to a safe repr.
		return fmt.Sprintf("%q", s)
	}
	return string(b)
}

// interface assertions
var (
	_ core.Tool = (*BrowserNavigateTool)(nil)
	_ core.Tool = (*BrowserClickTool)(nil)
	_ core.Tool = (*BrowserTypeTool)(nil)
	_ core.Tool = (*BrowserScreenshotTool)(nil)
	_ core.Tool = (*BrowserEvaluateTool)(nil)
)
