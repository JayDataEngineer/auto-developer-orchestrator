package browser

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"go.uber.org/zap"
)

// freeTCPPort returns a port that the OS has just released. Used by tests that
// need a port where nothing is listening — "expected to fail" cases. Hard-coded
// ports (e.g. 9222) couple the test to whatever Chrome is running on the dev
// machine that day. Free-port lookup decouples it.
func freeTCPPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("can't allocate free port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// ── parseElements ─────────────────────────────────────────────

func TestParseElements(t *testing.T) {
	input := `[{"id":1,"tag":"a","text":"Click me","selector":"#btn"},{"id":2,"tag":"input","text":"Email","selector":"#email"}]`
	elements, _, _ := parseElements(input)
	if len(elements) != 2 {
		t.Fatalf("expected 2 elements, got %d", len(elements))
	}
	if elements[0].ID != 1 || elements[0].Tag != "a" {
		t.Errorf("unexpected first element: %+v", elements[0])
	}
	if elements[1].Selector != "#email" {
		t.Errorf("expected #email, got %q", elements[1].Selector)
	}
}

func TestParseElementsEmpty(t *testing.T) {
	elements, _, _ := parseElements("[]")
	if len(elements) != 0 {
		t.Errorf("expected 0 elements, got %d", len(elements))
	}
}

func TestParseElementsInvalidJSON(t *testing.T) {
	elements, _, _ := parseElements("not json")
	if len(elements) != 0 {
		t.Errorf("expected 0 elements for invalid JSON, got %d", len(elements))
	}
}

func TestParseElementsWithViewport(t *testing.T) {
	input := `{"elements":[{"id":1,"tag":"a","text":"Click","selector":"#btn"}],"vw":1920,"vh":1080}`
	elements, vw, vh := parseElements(input)
	if len(elements) != 1 {
		t.Fatalf("expected 1 element, got %d", len(elements))
	}
	if vw != 1920 || vh != 1080 {
		t.Errorf("expected viewport 1920x1080, got %dx%d", vw, vh)
	}
}

// ── SandboxBrowserClient ──────────────────────────────────────

func TestNewSandboxBrowserClientInvalidPort(t *testing.T) {
	_, err := NewSandboxBrowserClient(0, "localhost", zap.NewNop())
	if err == nil {
		t.Error("expected error for port 0")
	}

	_, err = NewSandboxBrowserClient(-1, "localhost", zap.NewNop())
	if err == nil {
		t.Error("expected error for negative port")
	}
}

func TestNewSandboxBrowserClientSuccess(t *testing.T) {
	client, err := NewSandboxBrowserClient(9222, "localhost", zap.NewNop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewSandboxBrowserClientEmptyHostname(t *testing.T) {
	client, err := NewSandboxBrowserClient(9222, "", zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	if client.wsURL != "ws://localhost:9222" {
		t.Errorf("expected ws://localhost:9222, got %q", client.wsURL)
	}
}

func TestSandboxBrowserClientIsConnected(t *testing.T) {
	client, _ := NewSandboxBrowserClient(9222, "localhost", zap.NewNop())
	if client.IsConnected() {
		t.Error("should not be connected initially")
	}
}

func TestSandboxBrowserClientClose(t *testing.T) {
	client, _ := NewSandboxBrowserClient(9222, "localhost", zap.NewNop())
	// Close without connecting should not panic
	client.Close()
}

func TestSandboxBrowserClientGetSnapshot(t *testing.T) {
	client, _ := NewSandboxBrowserClient(9222, "localhost", zap.NewNop())

	snapshot, err := client.GetSnapshot()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snapshot.URL != "" {
		t.Errorf("expected empty URL initially, got %q", snapshot.URL)
	}
}

func TestSandboxBrowserClientSelectorForElement(t *testing.T) {
	client, _ := NewSandboxBrowserClient(9222, "localhost", zap.NewNop())

	client.lastElements = []LabeledElement{
		{ID: 1, Selector: "#submit"},
		{ID: 2, Selector: "#input"},
	}

	if s := client.selectorForElement(1); s != "#submit" {
		t.Errorf("expected #submit, got %q", s)
	}
	if s := client.selectorForElement(99); s != "" {
		t.Errorf("expected empty, got %q", s)
	}
}

func TestSandboxBrowserClientUpdateState(t *testing.T) {
	client, _ := NewSandboxBrowserClient(9222, "localhost", zap.NewNop())

	elements := []LabeledElement{{ID: 1, Tag: "a", Selector: "#link"}}
	client.updateState("http://example.com", "Example", elements, []byte("png"), 1920, 1080)

	snapshot, _ := client.GetSnapshot()
	if snapshot.URL != "http://example.com" {
		t.Errorf("expected http://example.com, got %q", snapshot.URL)
	}
	if snapshot.Title != "Example" {
		t.Errorf("expected Example, got %q", snapshot.Title)
	}
}

func TestSandboxBrowserClientScreenshotNotConnected(t *testing.T) {
	client, _ := NewSandboxBrowserClient(freeTCPPort(t), "localhost", zap.NewNop())
	ctx := context.Background()
	_, err := client.Screenshot(ctx)
	if err == nil {
		t.Error("expected error when not connected")
	}
}

func TestSandboxBrowserClientNavigateNotConnected(t *testing.T) {
	// Use a port where nothing is listening. Earlier this test hard-coded 9222,
	// which silently passed on dev machines without Chrome but FAILED on dev
	// machines that had Chrome on 9222 (the test would actually navigate).
	client, _ := NewSandboxBrowserClient(freeTCPPort(t), "localhost", zap.NewNop())
	ctx := context.Background()
	_, err := client.Navigate(ctx, "http://example.com")
	if err == nil {
		t.Error("expected error when not connected")
	}
}

func TestSandboxBrowserClientClickNoElement(t *testing.T) {
	client, _ := NewSandboxBrowserClient(freeTCPPort(t), "localhost", zap.NewNop())
	ctx := context.Background()
	_, err := client.Click(ctx, 99)
	if err == nil {
		t.Error("expected error for missing element")
	}
}

func TestSandboxBrowserClientTypeNoElement(t *testing.T) {
	client, _ := NewSandboxBrowserClient(freeTCPPort(t), "localhost", zap.NewNop())
	ctx := context.Background()
	_, err := client.Type(ctx, 99, "hello", false)
	if err == nil {
		t.Error("expected error for missing element")
	}
}

// ── VisionClient ──────────────────────────────────────────────

func TestNewVisionClient(t *testing.T) {
	client := NewVisionClient("http://litellm:4000", "key123", nil)
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if client.baseURL != "http://litellm:4000" {
		t.Errorf("unexpected URL: %q", client.baseURL)
	}
	if client.model != "gemma-4-26b" {
		t.Errorf("unexpected model: %q", client.model)
	}
}

func TestNewVisionClientDefaultURL(t *testing.T) {
	t.Setenv("MCP_HUB_ENDPOINT", "")
	client := NewVisionClient("", "", nil)
	if client.baseURL != "http://localhost:8001" {
		t.Errorf("expected default localhost:8001, got %q", client.baseURL)
	}
}

func TestNewVisionClientUsesMCPHub(t *testing.T) {
	t.Setenv("MCP_HUB_ENDPOINT", "http://hub.local:30080")
	client := NewVisionClient("", "", nil)
	if client.baseURL != "http://hub.local:30080/llm" {
		t.Errorf("expected MCP hub URL, got %q", client.baseURL)
	}
}

func TestVisionClientDescribePageNoServer(t *testing.T) {
	client := NewVisionClient("", "", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := client.DescribePage(ctx, []byte("fake-png"))
	if err == nil {
		t.Error("expected error when no server running")
	}
	// Should fail on connection, not on "not configured"
	if err.Error() == "LITELLM_PROXY_URL not configured" {
		t.Error("should not require LITELLM_PROXY_URL — should try localhost:8001")
	}
}

// ── LabeledElement JSON ───────────────────────────────────────

func TestLabeledElementJSON(t *testing.T) {
	el := LabeledElement{ID: 1, Tag: "button", Text: "Submit", Role: "button", Selector: "#submit"}
	data, err := json.Marshal(el)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"id":1,"tag":"button","text":"Submit","role":"button","selector":"#submit"}` {
		t.Errorf("unexpected JSON: %s", data)
	}
}
