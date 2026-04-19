package llama

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// HTTPEngine communicates with llama-server over HTTP.
// It replaces the in-process CGo engine with HTTP calls to llama-server,
// gaining GBNF grammar support for constrained tool call generation.
type HTTPEngine struct {
	baseURL    string
	client     *http.Client
	logger     *zap.Logger
	modelName  string

	mu      sync.RWMutex
	loaded  bool
	slots   int
	slotCtx int // context size per slot
}

// HTTPEngineConfig holds configuration for creating an HTTPEngine.
type HTTPEngineConfig struct {
	BaseURL   string // e.g. "http://localhost:8001"
	ModelName string // display name for logs/events
	Logger    *zap.Logger
}

// NewHTTPEngine creates a new HTTP engine (does NOT load the model — llama-server does that).
func NewHTTPEngine(cfg HTTPEngineConfig) *HTTPEngine {
	if cfg.Logger == nil {
		cfg.Logger = zap.NewNop()
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "http://localhost:8001"
	}
	if cfg.ModelName == "" {
		cfg.ModelName = "gemma-4-26b"
	}
	return &HTTPEngine{
		baseURL:   cfg.BaseURL,
		client:    &http.Client{Timeout: 120 * time.Second},
		logger:    cfg.Logger,
		modelName: cfg.ModelName,
	}
}

// CheckHealth verifies the llama-server is running and the model is loaded.
func (e *HTTPEngine) CheckHealth() error {
	resp, err := e.client.Get(e.baseURL + "/health")
	if err != nil {
		return fmt.Errorf("llama-server not reachable at %s: %w", e.baseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("llama-server health check failed: HTTP %d", resp.StatusCode)
	}

	var result struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode health response: %w", err)
	}

	if result.Status != "ok" {
		return fmt.Errorf("llama-server status: %s", result.Status)
	}

	e.mu.Lock()
	e.loaded = true
	e.mu.Unlock()
	return nil
}

// LoadModel is a no-op for HTTP engine — llama-server loads the model at startup.
// It verifies connectivity instead.
func (e *HTTPEngine) LoadModel() error {
	if err := e.CheckHealth(); err != nil {
		return err
	}
	e.logger.Info("HTTP engine connected to llama-server", zap.String("url", e.baseURL))
	return nil
}

// NewSession creates a new HTTP-based session that uses a llama-server slot.
func (e *HTTPEngine) NewSession(ctxSize int) (*Session, error) {
	if !e.IsLoaded() {
		return nil, fmt.Errorf("engine not connected to llama-server")
	}

	// Generate a unique session ID for KV cache persistence
	sessionID := fmt.Sprintf("sess-%d", time.Now().UnixNano())

	return &Session{
		engine:    e,
		sessionID: sessionID,
		ctxSize:   ctxSize,
		history:   []Turn{},
	}, nil
}

// NewSessionWithGrammar creates a session with a GBNF grammar for constrained generation.
func (e *HTTPEngine) NewSessionWithGrammar(ctxSize int, grammar string) (*Session, error) {
	sess, err := e.NewSession(ctxSize)
	if err != nil {
		return nil, err
	}
	sess.grammar = grammar
	return sess, nil
}

// IsLoaded returns whether the engine is connected to llama-server.
func (e *HTTPEngine) IsLoaded() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.loaded
}

// LoadDuration returns 0 for HTTP engine (model loaded by llama-server at startup).
func (e *HTTPEngine) LoadDuration() time.Duration {
	return 0
}

// WarmUp sends a single-token request to pre-compile CUDA kernels on the server side.
func (e *HTTPEngine) WarmUp() error {
	e.logger.Info("Warming up llama-server with single-token request...")

	req := CompletionRequest{
		Prompt:      "Hello",
		MaxTokens:   1,
		Temperature: 0.1,
		CachePrompt: true,
	}

	t0 := time.Now()
	_, err := e.complete(req)
	if err != nil {
		e.logger.Warn("Warm-up request had error (non-fatal)", zap.Error(err))
	} else {
		e.logger.Info("llama-server warm-up complete", zap.Duration("duration", time.Since(t0)))
	}
	return nil
}

// Close is a no-op for HTTP engine.
func (e *HTTPEngine) Close() error {
	e.mu.Lock()
	e.loaded = false
	e.mu.Unlock()
	e.logger.Info("HTTP engine disconnected")
	return nil
}

// ModelName returns the display model name.
func (e *HTTPEngine) ModelName() string {
	return e.modelName
}

// complete sends a completion request to llama-server and returns the full response.
func (e *HTTPEngine) complete(req CompletionRequest) (*CompletionResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", e.baseURL+"/v1/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("llama-server request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("llama-server HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var result CompletionResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("decode response: %w (body: %s)", err, string(respBody))
	}

	return &result, nil
}

// completeStream sends a completion request and streams tokens via callback.
// llama-server returns SSE format: lines of "data: {json}\n\n" terminated by "data: [DONE]\n\n"
func (e *HTTPEngine) completeStream(req CompletionRequest, onToken func(token string) bool) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", e.baseURL+"/v1/completions", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("llama-server request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("llama-server HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse SSE stream: "data: {json}\n\n" lines
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // up to 1MB lines
	for scanner.Scan() {
		line := scanner.Text()

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}

		// Check for stream end
		if line == "data: [DONE]" {
			break
		}

		// Parse SSE data line
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		jsonStr := line[6:] // strip "data: " prefix

		var event struct {
			Choices []struct {
				Text string `json:"text"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(jsonStr), &event); err != nil {
			// Skip malformed lines
			continue
		}

		if len(event.Choices) > 0 {
			text := event.Choices[0].Text
			if text != "" {
				if !onToken(text) {
					break
				}
			}
		}
	}

	return scanner.Err()
}

// CompletionRequest maps to llama-server's /v1/completions request body.
type CompletionRequest struct {
	Prompt      string  `json:"prompt"`
	MaxTokens   int     `json:"n_predict,omitempty"`
	Temperature float32 `json:"temperature,omitempty"`
	TopP        float32 `json:"top_p,omitempty"`
	TopK        int     `json:"top_k,omitempty"`
	RepeatPenalty float32 `json:"repeat_penalty,omitempty"`

	// KV cache persistence — session_id tells llama-server to reuse the slot's KV cache
	CachePrompt bool   `json:"cache_prompt"`
	SessionID   string `json:"session_id,omitempty"`

	// Grammar-constrained generation
	Grammar string `json:"grammar,omitempty"`

	// Stop sequences
	Stop []string `json:"stop,omitempty"`

	// Stream mode
	Stream bool `json:"stream,omitempty"`
}

// CompletionResponse maps to llama-server's /v1/completions response.
type CompletionResponse struct {
	Content string `json:"content"`
	Stop    bool   `json:"stop"`
	Tokens  int    `json:"tokens_predicted"`
	// Usage tracking
	Timings struct {
		PromptN       int     `json:"prompt_n"`
		PredictedN    int     `json:"predicted_n"`
		PredictedMs   float64 `json:"predicted_ms"`
		PromptMs      float64 `json:"prompt_ms"`
	} `json:"timings"`
}
