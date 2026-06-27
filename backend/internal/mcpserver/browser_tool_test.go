package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// sbFakeExec captures the curl command and returns a canned sb_server
// response. Same shape as fakeSandboxExec but the canned output is the
// sb_server JSON envelope.
func newSBFake(out string) *fakeSandboxExec {
	return &fakeSandboxExec{out: out}
}

func TestBrowserNavigateBuildsCorrectCurl(t *testing.T) {
	fake := newSBFake(`{"ok":true,"title":"Example","url":"https://example.com"}`)
	tool := NewBrowserNavigateTool(fake, BrowserToolConfig{})

	result, err := tool.Execute(context.Background(), map[string]any{
		"url": "https://example.com",
	})
	if err != nil {
		t.Fatalf("execute errored: %v", err)
	}
	m := result.(map[string]any)
	if m["ok"] != true {
		t.Errorf("ok = %v, want true (full result: %v)", m["ok"], m)
	}
	if m["title"] != "Example" {
		t.Errorf("title wrong: %v", m["title"])
	}

	// Verify the curl command targets the right endpoint with the URL payload.
	if !strings.Contains(fake.lastCmd, "/navigate") {
		t.Errorf("command missing /navigate endpoint: %q", fake.lastCmd)
	}
	if !strings.Contains(fake.lastCmd, "-X POST") {
		t.Errorf("command not POST: %q", fake.lastCmd)
	}
	if !strings.Contains(fake.lastCmd, "https://example.com") {
		t.Errorf("URL missing from payload: %q", fake.lastCmd)
	}
}

func TestBrowserNavigateRequiresURL(t *testing.T) {
	fake := newSBFake("")
	tool := NewBrowserNavigateTool(fake, BrowserToolConfig{})

	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatalf("expected Go error when url missing")
	}
}

func TestBrowserClickByLabel(t *testing.T) {
	fake := newSBFake(`{"ok":true,"clicked":true}`)
	tool := NewBrowserClickTool(fake, BrowserToolConfig{})

	_, err := tool.Execute(context.Background(), map[string]any{
		"index": 7,
	})
	if err != nil {
		t.Fatalf("execute errored: %v", err)
	}
	if !strings.Contains(fake.lastCmd, "/click") {
		t.Errorf("endpoint wrong: %q", fake.lastCmd)
	}
	// Index must be JSON-encoded in the body, not the bare integer.
	body := extractCurlBody(fake.lastCmd)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("body not valid JSON: %v (body: %q)", err, body)
	}
	if parsed["index"] != float64(7) {
		t.Errorf("index not propagated: %v", parsed["index"])
	}
}

func TestBrowserClickBySelector(t *testing.T) {
	fake := newSBFake(`{"ok":true}`)
	tool := NewBrowserClickTool(fake, BrowserToolConfig{})

	_, err := tool.Execute(context.Background(), map[string]any{
		"selector": "button#submit",
	})
	if err != nil {
		t.Fatalf("execute errored: %v", err)
	}
	body := extractCurlBody(fake.lastCmd)
	if !strings.Contains(body, "button#submit") {
		t.Errorf("selector not in body: %s", body)
	}
}

func TestBrowserTypeRequiresTarget(t *testing.T) {
	fake := newSBFake("")
	tool := NewBrowserTypeTool(fake, BrowserToolConfig{})

	// text without target → error
	_, err := tool.Execute(context.Background(), map[string]any{"text": "hello"})
	if err == nil {
		t.Fatalf("expected error when text has no target")
	}
}

func TestBrowserTypeWithSelector(t *testing.T) {
	fake := newSBFake(`{"ok":true}`)
	tool := NewBrowserTypeTool(fake, BrowserToolConfig{})

	_, err := tool.Execute(context.Background(), map[string]any{
		"text":    "hello@example.com",
		"selector": "input[name=email]",
	})
	if err != nil {
		t.Fatalf("execute errored: %v", err)
	}
	body := extractCurlBody(fake.lastCmd)
	if !strings.Contains(body, "input[name=email]") {
		t.Errorf("selector missing: %s", body)
	}
	if !strings.Contains(body, "hello@example.com") {
		t.Errorf("text missing: %s", body)
	}
}

func TestBrowserScreenshotCallsRead(t *testing.T) {
	// Screenshot re-uses /read endpoint — it returns the current page state
	// including a fresh labeled screenshot. (Use valid JSON for the canned
	// response so parseSBResponse doesn't error before we can check the URL.)
	fake := newSBFake(`{"ok":true,"screenshot":"iVBORw...","labels":[{"n":1}]}`)
	tool := NewBrowserScreenshotTool(fake, BrowserToolConfig{})

	_, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("execute errored: %v", err)
	}
	if !strings.Contains(fake.lastCmd, "/read") {
		t.Errorf("screenshot should use /read endpoint: %q", fake.lastCmd)
	}
}

func TestBrowserEvaluateScript(t *testing.T) {
	fake := newSBFake(`{"ok":true,"result":"Page Title"}`)
	tool := NewBrowserEvaluateTool(fake, BrowserToolConfig{})

	_, err := tool.Execute(context.Background(), map[string]any{
		"code": "return document.title",
	})
	if err != nil {
		t.Fatalf("execute errored: %v", err)
	}
	body := extractCurlBody(fake.lastCmd)
	if !strings.Contains(body, "document.title") {
		t.Errorf("script not in body: %s", body)
	}
	if !strings.Contains(body, "\"code\":") {
		t.Errorf("field name should be 'code', not 'script': %s", body)
	}
}

func TestBrowserEvaluateRequiresScript(t *testing.T) {
	fake := newSBFake("")
	tool := NewBrowserEvaluateTool(fake, BrowserToolConfig{})

	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatalf("expected Go error when code missing")
	}
}

// TestBrowserTimeout verifies the timeout path surfaces as a Go error
// (browser tools have NO graceful-degradation path — they need sb_server
// up, period). Uses a 1s timeout (curl --max-time rounds sub-second to 1s)
// against a 5s fake delay.
func TestBrowserTimeout(t *testing.T) {
	fake := &fakeSandboxExec{
		out:   "",
		delay: 5 * time.Second,
	}
	tool := NewBrowserNavigateTool(fake, BrowserToolConfig{Timeout: 1 * time.Second})

	_, err := tool.Execute(context.Background(), map[string]any{"url": "https://example.com"})
	if err == nil {
		t.Fatalf("expected Go error on timeout")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("error should mention timeout: %v", err)
	}
}

// TestBrowserMalformedResponse verifies the JSON-parse guard fires when
// sb_server returns garbage (e.g. HTML 500 page from supervisord).
func TestBrowserMalformedResponse(t *testing.T) {
	fake := newSBFake("<html>500 Internal Server Error</html>")
	tool := NewBrowserNavigateTool(fake, BrowserToolConfig{})

	_, err := tool.Execute(context.Background(), map[string]any{"url": "https://example.com"})
	if err == nil {
		t.Fatalf("expected Go error on malformed response")
	}
	if !strings.Contains(err.Error(), "malformed") {
		t.Errorf("error should mention malformed: %v", err)
	}
}

// TestBrowserExecFailure verifies non-zero curl exit propagates as a
// Go error (e.g. sb_server down, container networking broken).
func TestBrowserExecFailure(t *testing.T) {
	fake := &fakeSandboxExec{
		out: "curl: (7) Failed to connect to 127.0.0.1 port 9876",
		err: errFake("exec exited with code 7"),
	}
	tool := NewBrowserNavigateTool(fake, BrowserToolConfig{})

	_, err := tool.Execute(context.Background(), map[string]any{"url": "https://example.com"})
	if err == nil {
		t.Fatalf("expected Go error on curl failure")
	}
	// Error must mention the endpoint so the model knows which call failed.
	if !strings.Contains(err.Error(), "/navigate") {
		t.Errorf("error should mention /navigate endpoint: %v", err)
	}
}

// extractCurlBody pulls the JSON body out of the `-d '<body>'` arg of
// the curl command. Returns "" if no body present.
func extractCurlBody(cmd string) string {
	_, rest, ok := strings.Cut(cmd, "-d '")
	if !ok {
		return ""
	}
	body, _, found := strings.Cut(rest, "'")
	if !found {
		return ""
	}
	return body
}

// errFake is a tiny helper to make table tests readable.
type errFake string

func (e errFake) Error() string { return string(e) }
