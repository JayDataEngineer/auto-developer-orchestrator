// browser_tool.go exposes the sandbox's existing sb_server (persistent
// SeleniumBase HTTP service inside the container) as MCP tools.
//
// The sandbox image ships /usr/local/bin/sb_server.py which supervisord
// runs on port 9876 INSIDE the container (namespace-isolated from the
// host MCP server's port — no conflict). It exposes a rich browser API:
// navigate, click, type, screenshot, evaluate, etc. State persists across
// calls because the Python process holds the SeleniumBase session.
//
// All 5 endpoints share the same shape (curl POST → JSON response). Only
// the endpoint path + the body-construction logic vary. We encode that
// variation declaratively as a slice of browserSpec structs and dispatch
// through one BrowserTool type. Adding a 6th browser tool = append one
// spec entry, no new type.
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

// browserBase is the shared scaffolding for all browser tools.
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
	maxTimeSec := max(1, int(math.Ceil(b.timeout.Seconds())))
	parts := []string{
		"curl -s -S",
		fmt.Sprintf("--max-time %d", maxTimeSec),
		"-X POST",
		fmt.Sprintf("%s%s", sbServerAddr, endpoint),
		"-H 'Content-Type: application/json'",
	}
	for k, v := range extraHeaders {
		parts = append(parts, fmt.Sprintf("-H %s", shQ(k+": "+v)))
	}
	if body != "" {
		parts = append(parts, "-d", shQ(body))
	}
	cmd := strings.Join(parts, " ")

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

// mustJSONString JSON-encodes a string. Used for embedding strings into
// hand-built JSON payloads (we don't full-marshal because sb_server
// accepts extra fields the model didn't set).
func mustJSONString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return fmt.Sprintf("%q", s)
	}
	return string(b)
}

// ── Spec-driven dispatch ──────────────────────────────────────────────

// browserSpec declaratively describes one browser_* tool. The spec slice
// below is the source of truth — to add a new browser tool, append here.
type browserSpec struct {
	name        string
	description string
	schema      json.RawMessage
	endpoint    string
	// buildBody turns MCP args into the sb_server JSON body. Returns ""
	// for empty-body endpoints (browser_screenshot sends "{}"). Returns
	// core.ToolError for arg-validation failures.
	buildBody func(args map[string]any) (string, error)
}

// BrowserTool is the single dispatcher type for the whole browser family.
// One instance per spec entry; registered via RegisterBrowserTools.
type BrowserTool struct {
	spec browserSpec
	base browserBase
}

func (t *BrowserTool) Name() string            { return t.spec.name }
func (t *BrowserTool) Description() string     { return t.spec.description }
func (t *BrowserTool) Schema() json.RawMessage { return t.spec.schema }

func (t *BrowserTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	body, err := t.spec.buildBody(args)
	if err != nil {
		return nil, err
	}
	out, err := t.base.postJSON(ctx, t.spec.endpoint, body, nil)
	if err != nil {
		return nil, err
	}
	return parseSBResponse(out)
}

// browserSpecs is the declarative registry of browser tools.
var browserSpecs = []browserSpec{
	{
		name:        "browser_navigate",
		description: "Open a URL in the sandbox's persistent Chrome. Returns page title, URL, text snippet, and a base64 screenshot with Set-of-Marks labels on interactive elements. The session persists — subsequent browser_click / browser_type / browser_screenshot calls operate on this page until you navigate again.",
		schema: json.RawMessage(`{
			"type": "object",
			"required": ["url"],
			"properties": {
				"url": {"type": "string", "description": "Absolute URL including scheme (https://example.com)"}
			}
		}`),
		endpoint: "/navigate",
		buildBody: func(args map[string]any) (string, error) {
			url, _ := args["url"].(string)
			if url == "" {
				return "", core.NewToolError("browser_navigate", "url is required")
			}
			return fmt.Sprintf(`{"url":%s}`, mustJSONString(url)), nil
		},
	},
	{
		name:        "browser_click",
		description: "Click an element on the current page. Pass either a SoM label (integer from the labeled screenshot) or a CSS selector string. Returns the post-click page state (URL, title, screenshot).",
		schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"index": {"type": "integer", "description": "SoM label (the numbered box on interactive elements from the last screenshot). Sent as 'index' to sb_server."},
				"selector": {"type": "string", "description": "CSS selector (e.g. 'button#submit'). Used when index is omitted."}
			},
			"oneOf": [{"required": ["index"]}, {"required": ["selector"]}]
		}`),
		endpoint: "/click",
		buildBody: marshalArgs("browser_click"),
	},
	{
		name:        "browser_type",
		description: "Type text into a form field on the current page. Uses CDP character-by-character input (React-safe — fires real DOM events). Pass either a SoM label or CSS selector to identify the target input.",
		schema: json.RawMessage(`{
			"type": "object",
			"required": ["text"],
			"properties": {
				"text": {"type": "string", "description": "Text to type into the field"},
				"index": {"type": "integer", "description": "SoM label of the target input. Sent as 'index' to sb_server."},
				"selector": {"type": "string", "description": "CSS selector of the target input"}
			},
			"oneOf": [{"required": ["text", "index"]}, {"required": ["text", "selector"]}]
		}`),
		endpoint: "/type",
		buildBody: func(args map[string]any) (string, error) {
			if _, ok := args["text"].(string); !ok {
				return "", core.NewToolError("browser_type", "text is required")
			}
			if _, hasIndex := args["index"]; !hasIndex {
				if _, hasSel := args["selector"].(string); !hasSel {
					return "", core.NewToolError("browser_type", "either index or selector is required")
				}
			}
			return marshalArgs("browser_type")(args)
		},
	},
	{
		name:        "browser_screenshot",
		description: "Capture the current browser state as a labeled screenshot. Returns base64 PNG + SoM-numbered boxes on interactive elements. Use this to re-orient after page updates, or to get a fresh set of label numbers for clicking.",
		schema:     json.RawMessage(`{"type":"object","properties":{}}`),
		endpoint:   "/read",
		buildBody:  func(_ map[string]any) (string, error) { return "{}", nil },
	},
	{
		name:        "browser_evaluate",
		description: "Evaluate JavaScript on the current page, return the result. Power-tool escape hatch when navigate/click/type/screenshot don't fit (e.g. read window.__NEXT_DATA__, scroll to specific element, fetch XHR). The script runs in the page context — same-origin policy applies.",
		schema: json.RawMessage(`{
			"type": "object",
			"required": ["code"],
			"properties": {
				"code": {"type": "string", "description": "JavaScript expression to evaluate. Use 'return' for explicit value (e.g. 'return document.title')"}
			}
		}`),
		endpoint: "/evaluate",
		buildBody: func(args map[string]any) (string, error) {
			code, _ := args["code"].(string)
			if code == "" {
				return "", core.NewToolError("browser_evaluate", "code is required")
			}
			return fmt.Sprintf(`{"code":%s}`, mustJSONString(code)), nil
		},
	},
}

// marshalArgs returns a buildBody func that JSON-encodes args as-is. Used
// by endpoints (click, type) where the sb_server contract accepts the
// model's argument map directly with no transformation.
func marshalArgs(toolName string) func(map[string]any) (string, error) {
	return func(args map[string]any) (string, error) {
		b, err := json.Marshal(args)
		if err != nil {
			return "", fmt.Errorf("%s: marshal args: %w", toolName, err)
		}
		return string(b), nil
	}
}

// RegisterBrowserTools installs all browser_* tools from the spec slice.
// Called from main.go.
func RegisterBrowserTools(srv *Server, exec SandboxExecutor, cfg BrowserToolConfig) {
	base := newBrowserBase(exec, cfg)
	for _, spec := range browserSpecs {
		srv.RegisterTool(&BrowserTool{spec: spec, base: base})
	}
}

var _ core.Tool = (*BrowserTool)(nil)
