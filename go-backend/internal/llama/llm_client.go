package llama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/auto-developer-orchestrator/backend/internal/util"
)

// ChatProvider is the interface that LLMClient implements.
// Used by AgentLoop, OrchestratorLoop, and related subsystems.
// Mocks can implement this for testing.
type ChatProvider interface {
	chatComplete(req ChatCompletionRequest) (*ChatCompletionResponse, error)
	chatCompleteStream(req ChatCompletionRequest, onChunk func(delta StreamDelta, finish FinishReason, usage *StreamUsage) bool) error
	NewSession(ctxSize int) (*Session, error)
	IsLoaded() bool
	IsCloud() bool
	HasVision() bool
	ModelName() string
	CheckHealth() error
	WarmUp() error
	Close() error
}

// LLMClient communicates with an OpenAI-compatible API over HTTP.
// Works with llama-server, Google Gemini (OpenAI compat), or any compatible endpoint.
// Uses /v1/chat/completions with native OpenAI-style tool calling.
type LLMClient struct {
	baseURL          string
	apiKey           string // optional — set for cloud providers (Gemini, OpenAI, etc.)
	client           *http.Client
	logger           *zap.Logger
	modelName        string
	disableStreaming bool // proxies without SSE support use non-streaming API

	mu            sync.RWMutex
	loaded        bool
	hasVision     *bool // nil = not checked yet, true/false = cached result
	contextWindow int   // 0 = not fetched yet, cached from /v1/models or /props
}

// LLMClientConfig holds configuration for creating an LLMClient.
type LLMClientConfig struct {
	BaseURL          string // e.g. "http://localhost:8001" or "https://generativelanguage.googleapis.com/v1beta/openai"
	APIKey           string // optional Bearer token for cloud providers
	ModelName        string // model ID sent in requests and used for logs/events
	DisableStreaming bool   // use non-streaming API (for proxies that don't support SSE)
	Logger           *zap.Logger
}

// NewLLMClient creates a new HTTP engine (does NOT load the model — llama-server does that).
func NewLLMClient(cfg LLMClientConfig) *LLMClient {
	if cfg.Logger == nil {
		cfg.Logger = zap.NewNop()
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "http://localhost:8001"
	}
	if cfg.ModelName == "" {
		cfg.ModelName = "gemma-4-26b"
	}
	return &LLMClient{
		baseURL:          cfg.BaseURL,
		apiKey:           cfg.APIKey,
		client:           &http.Client{Timeout: 5 * time.Minute},
		logger:           cfg.Logger,
		modelName:        cfg.ModelName,
		disableStreaming: cfg.DisableStreaming,
	}
}

// CheckHealth verifies the endpoint is reachable.
// For local llama-server, checks /health. For cloud providers, marks as loaded immediately.
func (e *LLMClient) CheckHealth() error {
	// Cloud providers don't have a /health endpoint — just mark as loaded
	if e.apiKey != "" {
		e.mu.Lock()
		e.loaded = true
		e.mu.Unlock()
		return nil
	}

	resp, err := e.client.Get(e.baseURL + "/health")
	if err != nil {
		return fmt.Errorf("server not reachable at %s: %w", e.baseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check failed: HTTP %d", resp.StatusCode)
	}

	var result struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode health response: %w", err)
	}

	if result.Status != "ok" {
		return fmt.Errorf("server status: %s", result.Status)
	}

	e.mu.Lock()
	e.loaded = true
	e.mu.Unlock()
	return nil
}

// LoadModel is a no-op for HTTP engine — the server loads the model at startup.
// It verifies connectivity instead.
func (e *LLMClient) LoadModel() error {
	if err := e.CheckHealth(); err != nil {
		return err
	}
	e.logger.Info("HTTP engine connected", zap.String("url", e.baseURL), zap.String("model", e.modelName))
	return nil
}

// NewSession creates a new HTTP-based session that uses a llama-server slot.
func (e *LLMClient) NewSession(ctxSize int) (*Session, error) {
	if !e.IsLoaded() {
		return nil, fmt.Errorf("engine not connected to llama-server")
	}

	sessionID := fmt.Sprintf("sess-%d", time.Now().UnixNano())

	return &Session{
		engine:    e,
		sessionID: sessionID,
		ctxSize:   ctxSize,
		messages:  []Message{},
	}, nil
}

// StreamingDisabled returns true if this engine uses non-streaming API.
func (e *LLMClient) StreamingDisabled() bool {
	return e.disableStreaming
}

// IsLoaded returns whether the engine is connected.
func (e *LLMClient) IsLoaded() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.loaded
}

// ModelName returns the model ID this engine is configured for.
func (e *LLMClient) ModelName() string {
	return e.modelName
}

// IsCloud returns true if this engine talks to a cloud provider (has an API key).
func (e *LLMClient) IsCloud() bool {
	return e.apiKey != ""
}

// HasVision returns true if the model supports image understanding.
// For local llama-server: checks /props for mmproj availability.
// For cloud providers: uses model name heuristics (Gemini = yes, DeepSeek = no).
// Results are cached after first check.
func (e *LLMClient) HasVision() bool {
	e.mu.RLock()
	if e.hasVision != nil {
		result := *e.hasVision
		e.mu.RUnlock()
		return result
	}
	e.mu.RUnlock()

	// Check and cache
	result := e.checkVision()
	e.mu.Lock()
	e.hasVision = &result
	e.mu.Unlock()
	return result
}

// checkVision determines if the model has vision capability.
func (e *LLMClient) checkVision() bool {
	// Cloud providers: model name heuristics
	if e.IsCloud() {
		name := strings.ToLower(e.modelName)
		if strings.Contains(name, "gemini") {
			return true
		}
		if strings.Contains(name, "claude") {
			return true
		}
		if strings.Contains(name, "vision") {
			return true
		}
		return false
	}

	// Self-hosted providers (local llama-server, cluster LLM):
	// Try /props first (llama-server specific), then /v1/models fallback.

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Phase 1: Check /props (llama-server mmproj detection)
	req, err := http.NewRequestWithContext(ctx, "GET", e.baseURL+"/props", nil)
	if err == nil {
		resp, err := e.client.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			defer resp.Body.Close()
			var props struct {
				MultiModalProjector struct {
					Architecture string `json:"architecture"`
				} `json:"mmproj"`
			}
			if json.NewDecoder(resp.Body).Decode(&props) == nil {
				if props.MultiModalProjector.Architecture != "" {
					e.logger.Info("HasVision: mmproj detected on llama-server")
					return true
				}
				e.logger.Info("HasVision: no mmproj on llama-server")
				return false
			}
		}
	}

	// Phase 2: Check /v1/models (OpenAI-compatible fallback).
	// Some servers (vLLM, TGI, SGLang) expose model metadata through this endpoint.
	return e.checkVisionViaModels(ctx)
}

// ContextWindow returns the model's maximum context length.
// Fetches from /v1/models (OpenAI-compatible) or /props (llama-server) on first call, then caches.
// Returns 0 if the value cannot be determined.
func (e *LLMClient) ContextWindow() int {
	e.mu.RLock()
	if e.contextWindow > 0 {
		cw := e.contextWindow
		e.mu.RUnlock()
		return cw
	}
	e.mu.RUnlock()

	cw := e.fetchContextWindow()
	if cw > 0 {
		e.mu.Lock()
		e.contextWindow = cw
		e.mu.Unlock()
	}
	return cw
}

// fetchContextWindow tries multiple endpoints to discover the model's context length.
func (e *LLMClient) fetchContextWindow() int {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Phase 1: Try /props (llama-server specific — has total_slots.context_length)
	req, err := http.NewRequestWithContext(ctx, "GET", e.baseURL+"/props", nil)
	if err == nil {
		if e.apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+e.apiKey)
		}
		resp, err := e.client.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			defer resp.Body.Close()
			var props struct {
				TotalSlots struct {
					ContextLength int `json:"context_length"`
				} `json:"total_slots"`
			}
			if json.NewDecoder(resp.Body).Decode(&props) == nil && props.TotalSlots.ContextLength > 0 {
				e.logger.Info("ContextWindow: from /props", zap.Int("ctx", props.TotalSlots.ContextLength))
				return props.TotalSlots.ContextLength
			}
		}
	}

	// Phase 2: Try /v1/models (OpenAI-compatible — returns context_length or max_model_len)
	return e.fetchContextViaModels(ctx)
}

// fetchContextViaModels probes /v1/models for context_length metadata.
func (e *LLMClient) fetchContextViaModels(ctx context.Context) int {
	req, err := http.NewRequestWithContext(ctx, "GET", e.baseURL+"/v1/models", nil)
	if err != nil {
		return 0
	}
	if e.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.apiKey)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		e.logger.Debug("ContextWindow: /v1/models not reachable", zap.Error(err))
		return 0
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0
	}

	// Parse response — context_length may be at top level or per-model.
	// OpenRouter: each model has "context_length"
	// vLLM/SGLang: top-level "max_model_len" or per-model "max_model_len"
	// llama-server: no context in /v1/models (only in /props)
	var raw json.RawMessage
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return 0
	}

	// Try per-model: find our model and extract context_length
	var result struct {
		Data []struct {
			ID            string `json:"id"`
			ContextLength int    `json:"context_length"`
			MaxModelLen   int    `json:"max_model_len"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err == nil {
		for _, m := range result.Data {
			if m.ID == e.modelName || m.ContextLength > 0 || m.MaxModelLen > 0 {
				cl := m.ContextLength
				if cl == 0 {
					cl = m.MaxModelLen
				}
				if cl > 0 {
					e.logger.Info("ContextWindow: from /v1/models", zap.String("model", m.ID), zap.Int("ctx", cl))
					return cl
				}
			}
		}
	}

	return 0
}

// checkVisionViaModels probes /v1/models for vision-capable model IDs.
func (e *LLMClient) checkVisionViaModels(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, "GET", e.baseURL+"/v1/models", nil)
	if err != nil {
		return false
	}
	resp, err := e.client.Do(req)
	if err != nil {
		e.logger.Debug("HasVision: /v1/models not reachable, assuming no vision", zap.Error(err))
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		e.logger.Debug("HasVision: /v1/models returned non-OK", zap.Int("status", resp.StatusCode))
		return false
	}

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		e.logger.Debug("HasVision: /v1/models decode failed", zap.Error(err))
		return false
	}

	for _, m := range result.Data {
		id := strings.ToLower(m.ID)
		if strings.Contains(id, "vision") || strings.Contains(id, "mmproj") || strings.Contains(id, "multimodal") {
			e.logger.Info("HasVision: vision model detected via /v1/models", zap.String("model_id", m.ID))
			return true
		}
	}
	e.logger.Debug("HasVision: no vision model found in /v1/models")
	return false
}

// WarmUp sends a single-token request to pre-compile CUDA kernels on the server side.
func (e *LLMClient) WarmUp() error {
	e.logger.Info("Warming up llama-server with single-token request...")

	req := ChatCompletionRequest{
		Messages:    []Message{{Role: "user", Content: "Hello"}},
		MaxTokens:   1,
		Temperature: 0.1,
		CachePrompt: true,
	}

	t0 := time.Now()
	_, err := e.chatComplete(req)
	if err != nil {
		e.logger.Warn("Warm-up request had error (non-fatal)", zap.Error(err))
	} else {
		e.logger.Info("llama-server warm-up complete", zap.Duration("duration", time.Since(t0)))
	}
	return nil
}

// Close is a no-op for HTTP engine.
func (e *LLMClient) Close() error {
	e.mu.Lock()
	e.loaded = false
	e.mu.Unlock()
	e.logger.Info("HTTP engine disconnected")
	return nil
}

// ── /v1/chat/completions types ─────────────────────────────────────

// ChatCompletionRequest maps to the /v1/chat/completions request body.
type ChatCompletionRequest struct {
	Model            string       `json:"model,omitempty"` // model ID for cloud providers
	Messages         []Message    `json:"messages"`
	Tools            []OpenAITool `json:"tools,omitempty"`
	MaxTokens        int          `json:"max_tokens,omitempty"`
	Temperature      float32      `json:"temperature,omitempty"`
	TopP             float32      `json:"top_p,omitempty"`
	TopK             int          `json:"top_k,omitempty"`
	RepeatPenalty    float32      `json:"repeat_penalty,omitempty"`
	PresencePenalty  float32      `json:"presence_penalty,omitempty"`
	MinP             float32      `json:"min_p,omitempty"`

	// KV cache persistence (llama-server extension)
	CachePrompt bool   `json:"cache_prompt,omitempty"`
	SessionID   string `json:"session_id,omitempty"`

	// Stream mode
	Stream bool `json:"stream,omitempty"`

	// Request usage data in streaming response (OpenAI stream_options)
	StreamOptions *StreamOptions `json:"stream_options,omitempty"`
}

// StreamOptions controls what additional data is included in streaming responses.
type StreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// sanitizeRequest strips llama.cpp-specific fields for cloud providers.
// Cloud APIs (Gemini, OpenRouter, OpenAI) reject unknown fields like
// top_k, repeat_penalty, presence_penalty, min_p, cache_prompt, and session_id.
func (e *LLMClient) sanitizeRequest(req ChatCompletionRequest) ChatCompletionRequest {
	if !e.IsCloud() {
		return req
	}
	// Zero out sampling params — let the server use its own defaults
	req.Temperature = 0
	req.TopP = 0
	req.TopK = 0
	req.RepeatPenalty = 0
	req.PresencePenalty = 0
	req.MinP = 0
	req.CachePrompt = false
	req.SessionID = ""

	// Gemini 3 requires thought_signature on tool calls in conversation history.
	// Inject a dummy signature for any that lack one.
	if strings.Contains(strings.ToLower(e.modelName), "gemini") {
		for i := range req.Messages {
			m := &req.Messages[i]
			if m.Role == "assistant" && len(m.ToolCalls) > 0 {
				for j := range m.ToolCalls {
					if m.ToolCalls[j].ThoughtSignature == "" {
						m.ToolCalls[j].ThoughtSignature = "c2tpcF90aG91Z2h0X3NpZ25hdHVyZV92YWxpZGF0b3I="
					}
				}
			}
		}
	}

	return req
}

// ChatCompletionResponse maps to the non-streaming /v1/chat/completions response.
type ChatCompletionResponse struct {
	Choices []ChatChoice `json:"choices"`
	Usage   struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// ChatChoice is a single choice in a chat completion response.
type ChatChoice struct {
	Message      ChatMessage  `json:"message"`
	FinishReason FinishReason `json:"finish_reason"`
}

// ChatMessage is the full message in a non-streaming response.
type ChatMessage struct {
	Role             string             `json:"role"`
	Content          string             `json:"content"`
	ReasoningContent string             `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCallResponse `json:"tool_calls,omitempty"`
}

// chatComplete sends a chat completion request (non-streaming).
func (e *LLMClient) chatComplete(req ChatCompletionRequest) (*ChatCompletionResponse, error) {
	body, err := json.Marshal(e.sanitizeRequest(req))
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", e.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if e.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+e.apiKey)
	}

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("chat request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("chat API HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var result ChatCompletionResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("decode response: %w (body: %s)", err, string(respBody))
	}

	return &result, nil
}

// chatCompleteStream sends a chat completion request and streams chunks via callback.
// SSE format: "data: {json}\n\n" with delta objects containing content, reasoning_content, or tool_calls.
// The final chunk includes a usage field with prompt_tokens and completion_tokens.
func (e *LLMClient) chatCompleteStream(req ChatCompletionRequest, onChunk func(delta StreamDelta, finish FinishReason, usage *StreamUsage) bool) error {
	req.Stream = true
	// Request usage data in streaming response (like Pi-Mono does)
	// This makes llama-server send a usage-only chunk at the end of the stream.
	req.StreamOptions = &StreamOptions{IncludeUsage: true}
	body, err := json.Marshal(e.sanitizeRequest(req))
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", e.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if e.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+e.apiKey)
	}

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("chat stream request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("chat API HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	chunkCount := 0
	for scanner.Scan() {
		line := scanner.Text()

		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}

		if line == "data: [DONE]" {
			break
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		jsonStr := line[6:]

		// Debug: log chunks with tool_calls or finish_reason for cloud providers
		chunkCount++
		if e.IsCloud() {
			hasToolCalls := strings.Contains(jsonStr, `"tool_calls"`)
			hasFinish := strings.Contains(jsonStr, `"finish_reason"`) && !strings.Contains(jsonStr, `"finish_reason":null`)
			if hasToolCalls || hasFinish || chunkCount <= 3 {
				e.logger.Debug("Cloud SSE chunk",
					zap.Int("chunk", chunkCount),
					zap.Bool("hasToolCalls", hasToolCalls),
					zap.Bool("hasFinish", hasFinish),
					zap.String("data", util.TruncateEllipsis(jsonStr, 500)),
				)
			}
		}

		var event struct {
			Choices []struct {
				Delta        StreamDelta `json:"delta"`
				FinishReason FinishReason `json:"finish_reason"`
			} `json:"choices"`
			Usage *StreamUsage `json:"usage,omitempty"`
		}
		if err := json.Unmarshal([]byte(jsonStr), &event); err != nil {
			e.logger.Debug("Cloud SSE parse error", zap.String("data", util.TruncateEllipsis(jsonStr, 200)), zap.Error(err))
			continue
		}

		if len(event.Choices) > 0 {
			choice := event.Choices[0]
			if !onChunk(choice.Delta, choice.FinishReason, event.Usage) {
				continue
			}
		} else if event.Usage != nil {
			// Usage-only chunk (empty choices) from some providers — pass through
			onChunk(StreamDelta{}, "", event.Usage)
		}
	}

	if e.IsCloud() {
		e.logger.Debug("Cloud stream complete",
			zap.Int("chunks", chunkCount),
			zap.String("model", e.modelName),
		)
	}

	return scanner.Err()
}

