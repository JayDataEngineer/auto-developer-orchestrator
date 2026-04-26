package observability

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"
)

// LangfuseClient sends telemetry to a Langfuse instance via its public API.
// If the LAMFUSE_HOST env var is not set, all methods are no-ops.
type LangfuseClient struct {
	baseURL    string
	publicKey  string
	secretKey  string
	httpClient *http.Client
	mu         sync.Mutex
}

// NewLangfuseClient creates a client using env vars.
// Set LANGFUSE_HOST, LANGFUSE_PUBLIC_KEY, LANGFUSE_SECRET_KEY to enable.
func NewLangfuseClient() *LangfuseClient {
	host := os.Getenv("LANGFUSE_HOST")
	if host == "" {
		return nil // disabled
	}
	return &LangfuseClient{
		baseURL:    host + "/api/public",
		publicKey:  os.Getenv("LANGFUSE_PUBLIC_KEY"),
		secretKey:  os.Getenv("LANGFUSE_SECRET_KEY"),
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

// Enabled returns true if Langfuse is configured.
func (c *LangfuseClient) Enabled() bool { return c != nil }

// ── Trace types ──────────────────────────────────────────────────────

type lfTrace struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Timestamp string            `json:"timestamp"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

type lfSpan struct {
	ID        string          `json:"id"`
	TraceID   string          `json:"traceId"`
	Name      string          `json:"name"`
	StartTime string          `json:"startTime"`
	EndTime   string          `json:"endTime,omitempty"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	Output    json.RawMessage `json:"output,omitempty"`
}

type lfGeneration struct {
	ID        string          `json:"id"`
	TraceID   string          `json:"traceId"`
	Name      string          `json:"name"`
	StartTime string          `json:"startTime"`
	EndTime   string          `json:"endTime,omitempty"`
	Model     string          `json:"model,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	Output    json.RawMessage `json:"output,omitempty"`
	Usage     lfUsage         `json:"usage,omitempty"`
}

type lfUsage struct {
	Input  int `json:"input,omitempty"`
	Output int `json:"output,omitempty"`
	Total  int `json:"total,omitempty"`
}

type lfIngest struct {
	Batch []lfIngestEvent `json:"batch"`
}

type lfIngestEvent struct {
	Type string      `json:"type"`
	Body interface{} `json:"body"`
}

// ── Public API ───────────────────────────────────────────────────────

// TraceRun starts a new trace for an agent run and calls the callback
// with a TraceHandle that can create spans and generations.
func (c *LangfuseClient) TraceRun(name, sessionID string, fn func(t *TraceHandle)) {
	if c == nil {
		return
	}
	traceID := fmt.Sprintf("trace-%d", time.Now().UnixNano())
	th := &TraceHandle{
		client:    c,
		traceID:   traceID,
		sessionID: sessionID,
	}
	c.send(&lfIngest{Batch: []lfIngestEvent{{
		Type: "trace-create",
		Body: lfTrace{
			ID:        traceID,
			Name:      name,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Metadata:  map[string]string{"session": sessionID},
		},
	}}})
	fn(th)
}

// TraceHandle is a builder for spans and generations within a trace.
type TraceHandle struct {
	client    *LangfuseClient
	traceID   string
	sessionID string
	counter   int
	mu        sync.Mutex
}

// Span records a span (tool execution, sub-agent, etc.).
func (th *TraceHandle) Span(name string, start, end time.Time, input, output map[string]interface{}) {
	if th == nil || th.client == nil {
		return
	}
	th.mu.Lock()
	th.counter++
	id := fmt.Sprintf("%s-span-%d", th.traceID, th.counter)
	th.mu.Unlock()

	inB, _ := json.Marshal(input)
	outB, _ := json.Marshal(output)
	th.client.send(&lfIngest{Batch: []lfIngestEvent{{
		Type: "span-create",
		Body: lfSpan{
			ID:        id,
			TraceID:   th.traceID,
			Name:      name,
			StartTime: start.UTC().Format(time.RFC3339),
			EndTime:   end.UTC().Format(time.RFC3339),
			Input:     inB,
			Output:    outB,
		},
	}}})
}

// Generation records an LLM generation (prompt → response).
func (th *TraceHandle) Generation(name, model string, start, end time.Time, inputTokens, outputTokens, totalTokens int) {
	if th == nil || th.client == nil {
		return
	}
	th.mu.Lock()
	th.counter++
	id := fmt.Sprintf("%s-gen-%d", th.traceID, th.counter)
	th.mu.Unlock()

	th.client.send(&lfIngest{Batch: []lfIngestEvent{{
		Type: "generation-create",
		Body: lfGeneration{
			ID:        id,
			TraceID:   th.traceID,
			Name:      name,
			StartTime: start.UTC().Format(time.RFC3339),
			EndTime:   end.UTC().Format(time.RFC3339),
			Model:     model,
			Usage: lfUsage{
				Input:  inputTokens,
				Output: outputTokens,
				Total:  totalTokens,
			},
		},
	}}})
}

func (c *LangfuseClient) send(event *lfIngest) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	data, _ := json.Marshal(event)
	req, _ := http.NewRequest("POST", c.baseURL+"/ingestion", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(c.publicKey, c.secretKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	// Best-effort — don't block the agent on telemetry failures
	io.Copy(io.Discard, resp.Body)
}
