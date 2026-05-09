package observability

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// LangfuseClient sends telemetry to a Langfuse instance via its public API.
// If LANGFUSE_HOST is not set, all methods are no-ops.
// Events are buffered and flushed in batches to reduce HTTP overhead.
type LangfuseClient struct {
	baseURL    string
	publicKey  string
	secretKey  string
	httpClient *http.Client
	release    string
	env        string
	mu         sync.Mutex
	buffer     []lfIngestEvent
	done       chan struct{}
}

// NewLangfuseClient creates a client using env vars.
// Set LANGFUSE_HOST, LANGFUSE_PUBLIC_KEY, LANGFUSE_SECRET_KEY to enable.
// Defaults to the cluster Langfuse instance when keys are set but host is not.
func NewLangfuseClient() *LangfuseClient {
	host := os.Getenv("LANGFUSE_HOST")
	if host == "" {
		if os.Getenv("LANGFUSE_PUBLIC_KEY") != "" || os.Getenv("LANGFUSE_SECRET_KEY") != "" {
			host = "http://100.86.69.57:30080/langfuse"
		}
	}
	if host == "" {
		return nil // disabled
	}
	c := &LangfuseClient{
		baseURL:    host + "/api/public",
		publicKey:  os.Getenv("LANGFUSE_PUBLIC_KEY"),
		secretKey:  os.Getenv("LANGFUSE_SECRET_KEY"),
		httpClient: &http.Client{Timeout: 5 * time.Second},
		done:       make(chan struct{}),
	}

	// Read release version
	c.release = os.Getenv("LANGFUSE_RELEASE")
	if c.release == "" {
		if out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output(); err == nil {
			c.release = strings.TrimSpace(string(out))
		}
	}

	// Read environment
	c.env = os.Getenv("LANGFUSE_ENVIRONMENT")
	if c.env == "" {
		c.env = "dev"
	}

	// Background flush every 2 seconds
	go c.flushLoop()

	return c
}

// Enabled returns true if Langfuse is configured.
func (c *LangfuseClient) Enabled() bool { return c != nil }

// Release returns the configured release string.
func (c *LangfuseClient) Release() string {
	if c == nil {
		return ""
	}
	return c.release
}

// Environment returns the configured environment string.
func (c *LangfuseClient) Environment() string {
	if c == nil {
		return ""
	}
	return c.env
}

// Flush sends any buffered events to Langfuse.
func (c *LangfuseClient) Flush() {
	if c == nil {
		return
	}
	c.mu.Lock()
	batch := c.buffer
	c.buffer = nil
	c.mu.Unlock()

	if len(batch) == 0 {
		return
	}
	c.send(&lfIngest{Batch: batch})
}

// Close stops the background flush goroutine and flushes remaining events.
func (c *LangfuseClient) Close() {
	if c == nil {
		return
	}
	close(c.done)
	c.Flush()
}

func (c *LangfuseClient) flushLoop() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			c.Flush()
		case <-c.done:
			return
		}
	}
}

func (c *LangfuseClient) enqueue(event lfIngestEvent) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.buffer = append(c.buffer, event)
	shouldFlush := len(c.buffer) >= 10
	c.mu.Unlock()

	if shouldFlush {
		c.Flush()
	}
}

// ── TraceConfig ────────────────────────────────────────────────────

// TraceConfig carries request context to the Langfuse hook without
// changing the core.LoopHook interface.
type TraceConfig struct {
	UserID    string   // agentId or project owner
	SessionID string   // session ID for grouping
	Project   string   // project name
	ModelName string   // model name
	SandboxID string   // sandbox ID
	Message   string   // user prompt (truncated to 200 chars)
	Tags      []string // auto-classified tags
	Release   string   // git commit short hash
	Env       string   // "dev" or "prod"
}

// ClassifyTags returns tags based on keywords in the message.
func ClassifyTags(msg string) []string {
	lower := strings.ToLower(msg)
	var tags []string
	tagRules := []struct {
		keywords []string
		tag      string
	}{
		{[]string{"browse", "website", "url", "scrape", "search", "navigate", "click", "page"}, "browser"},
		{[]string{"code", "implement", "fix", "refactor", "build", "debug", "test", "compile"}, "coding"},
		{[]string{"invest", "stock", "portfolio", "backtest", "signal", "ticker", "market", "price"}, "investing"},
		{[]string{"file", "read", "write", "edit", "directory", "folder", "glob", "grep"}, "file-ops"},
		{[]string{"deploy", "docker", "container", "compose", "kubernetes"}, "infra"},
	}
	for _, rule := range tagRules {
		for _, kw := range rule.keywords {
			if strings.Contains(lower, kw) {
				tags = append(tags, rule.tag)
				break
			}
		}
	}
	if len(tags) == 0 {
		tags = []string{"general"}
	}
	return tags
}

// ── Trace types ──────────────────────────────────────────────────────

type lfTrace struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Timestamp string            `json:"timestamp"`
	UserID    string            `json:"userId,omitempty"`
	SessionID string            `json:"sessionId,omitempty"`
	Tags      []string          `json:"tags,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	Release   string            `json:"release,omitempty"`
	Version   string            `json:"version,omitempty"`
	Input     json.RawMessage   `json:"input,omitempty"`
	Output    json.RawMessage   `json:"output,omitempty"`
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
	ID        string      `json:"id"`
	Type      string      `json:"type"`
	Timestamp string      `json:"timestamp"`
	Body      interface{} `json:"body"`
}

// ── Public API ───────────────────────────────────────────────────────

// TraceRun starts a new trace for an agent run and calls the callback
// with a TraceHandle that can create spans and generations.
func (c *LangfuseClient) TraceRun(name string, cfg TraceConfig, fn func(t *TraceHandle)) {
	if c == nil {
		return
	}
	traceID := fmt.Sprintf("trace-%d", time.Now().UnixNano())
	now := time.Now().UTC().Format(time.RFC3339)
	th := &TraceHandle{
		client:  c,
		traceID: traceID,
		cfg:     cfg,
	}

	// Build enriched metadata
	metadata := map[string]string{}
	if cfg.Project != "" {
		metadata["project"] = cfg.Project
	}
	if cfg.SandboxID != "" {
		metadata["sandbox"] = cfg.SandboxID
	}
	if cfg.ModelName != "" {
		metadata["model"] = cfg.ModelName
	}
	if cfg.Message != "" {
		metadata["user_prompt"] = cfg.Message
	}

	release := cfg.Release
	if release == "" {
		release = c.release
	}
	env := cfg.Env
	if env == "" {
		env = c.env
	}

	// User input as trace input
	var input json.RawMessage
	if cfg.Message != "" {
		input, _ = json.Marshal(map[string]string{"prompt": cfg.Message})
	}

	c.enqueue(lfIngestEvent{
		ID:        fmt.Sprintf("evt-%s-0", traceID),
		Type:      "trace-create",
		Timestamp: now,
		Body: lfTrace{
			ID:        traceID,
			Name:      name,
			Timestamp: now,
			UserID:    cfg.UserID,
			SessionID: cfg.SessionID,
			Tags:      cfg.Tags,
			Metadata:  metadata,
			Release:   release,
			Version:   release,
			Input:     input,
		},
	})
	fn(th)
}

// TraceHandle is a builder for spans, generations, and scores within a trace.
type TraceHandle struct {
	client  *LangfuseClient
	traceID string
	cfg     TraceConfig
	counter int
	mu      sync.Mutex
}

// Score posts a numeric score to the trace (chartable in Langfuse dashboards).
func (th *TraceHandle) Score(name string, value float64, dataType string, comment string) {
	if th == nil || th.client == nil {
		return
	}
	th.client.postScore(th.traceID, name, value, dataType, comment)
}

// Span records a span (tool execution, sub-agent, etc.).
func (th *TraceHandle) Span(name string, start, end time.Time, input, output map[string]interface{}) {
	if th == nil || th.client == nil {
		return
	}
	th.mu.Lock()
	th.counter++
	spanID := fmt.Sprintf("%s-span-%d", th.traceID, th.counter)
	th.mu.Unlock()

	inB, _ := json.Marshal(input)
	outB, _ := json.Marshal(output)
	th.client.enqueue(lfIngestEvent{
		ID:        fmt.Sprintf("evt-%s", spanID),
		Type:      "span-create",
		Timestamp: end.UTC().Format(time.RFC3339),
		Body: lfSpan{
			ID:        spanID,
			TraceID:   th.traceID,
			Name:      name,
			StartTime: start.UTC().Format(time.RFC3339),
			EndTime:   end.UTC().Format(time.RFC3339),
			Input:     inB,
			Output:    outB,
		},
	})
}

// Generation records an LLM generation (prompt -> response).
func (th *TraceHandle) Generation(name, model string, start, end time.Time, inputTokens, outputTokens, totalTokens int) {
	if th == nil || th.client == nil {
		return
	}
	th.mu.Lock()
	th.counter++
	genID := fmt.Sprintf("%s-gen-%d", th.traceID, th.counter)
	th.mu.Unlock()

	th.client.enqueue(lfIngestEvent{
		ID:        fmt.Sprintf("evt-%s", genID),
		Type:      "generation-create",
		Timestamp: end.UTC().Format(time.RFC3339),
		Body: lfGeneration{
			ID:        genID,
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
	})
}

func (c *LangfuseClient) send(event *lfIngest) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	data, _ := json.Marshal(event)
	req, err := http.NewRequest("POST", c.baseURL+"/ingestion", bytes.NewReader(data))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(c.publicKey, c.secretKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		log.Printf("Langfuse ingestion %d: %s", resp.StatusCode, string(body))
	} else {
		io.Copy(io.Discard, resp.Body)
	}
}

// postScore posts a score to a trace via the Langfuse public API.
// Scores show up in Langfuse dashboards as chartable metrics.
func (c *LangfuseClient) postScore(traceID, name string, value float64, dataType, comment string) {
	if c == nil {
		return
	}
	body := map[string]interface{}{
		"traceId":  traceID,
		"name":     name,
		"value":    value,
		"dataType": dataType, // "NUMERIC", "BOOLEAN", "CATEGORICAL"
	}
	if comment != "" {
		body["comment"] = comment
	}

	data, _ := json.Marshal(body)
	req, err := http.NewRequest("POST", c.baseURL+"/scores", bytes.NewReader(data))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(c.publicKey, c.secretKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		log.Printf("Langfuse score post %d: %s", resp.StatusCode, string(respBody))
	} else {
		io.Copy(io.Discard, resp.Body)
	}
}
