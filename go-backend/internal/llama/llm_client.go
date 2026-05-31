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

	"github.com/auto-developer-orchestrator/backend/internal/core"
	"github.com/auto-developer-orchestrator/backend/internal/util"
)

// ChatProvider is the interface that LLMClient implements.
// Used by AgentLoop, OrchestratorLoop, and related subsystems.
// Mocks can implement this for testing.
type ChatProvider interface {
	chatComplete(req ChatCompletionRequest) (*ChatCompletionResponse, error)
	chatCompleteStream(ctx context.Context, req ChatCompletionRequest, onChunk func(delta StreamDelta, finish FinishReason, usage *StreamUsage) bool) error
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
	client           *http.Client // non-streaming requests (has timeout)
	streamClient     *http.Client // SSE streaming (no timeout — context handles cancellation)
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
		streamClient:     &http.Client{}, // no timeout — SSE streams can run for minutes; context handles cancellation
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
		Messages:    []APIChatMessage{{Role: "user", Content: []byte(`"Hello"`)}},
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
	Model            string             `json:"model,omitempty"` // model ID for cloud providers
	Messages         []APIChatMessage   `json:"messages"`
	Tools            []OpenAITool       `json:"tools,omitempty"`
	MaxTokens        int                `json:"max_tokens,omitempty"`
	Temperature      float32            `json:"temperature,omitempty"`
	TopP             float32            `json:"top_p,omitempty"`
	TopK             int                `json:"top_k,omitempty"`
	RepeatPenalty    float32            `json:"repeat_penalty,omitempty"`
	PresencePenalty  float32            `json:"presence_penalty,omitempty"`
	MinP             float32            `json:"min_p,omitempty"`

	// KV cache persistence (llama-server extension)
	CachePrompt bool   `json:"cache_prompt,omitempty"`
	SessionID   string `json:"session_id,omitempty"`

	// Stream mode
	Stream bool `json:"stream,omitempty"`

	// Request usage data in streaming response (OpenAI stream_options)
	StreamOptions *StreamOptions `json:"stream_options,omitempty"`

	// Structured output format (OpenAI response_format)
	ResponseFormat *core.ResponseFormat `json:"response_format,omitempty"`

	// OpenRouter provider routing (ignored by other providers)
	Provider *ProviderRouting `json:"provider,omitempty"`
}

// ProviderRouting controls OpenRouter's provider selection.
type ProviderRouting struct {
	Order           []string `json:"order,omitempty"`
	AllowFallbacks  *bool    `json:"allow_fallbacks,omitempty"`
	Ignore          []string `json:"ignore,omitempty"`
	Only            []string `json:"only,omitempty"`
}

// APIChatMessage is the wire format for chat messages sent to /v1/chat/completions.
// Supports both plain text content (string) and multimodal content (array of parts).
type APIChatMessage struct {
	Role             string          `json:"role"`
	Content          json.RawMessage `json:"content"` // string or []ContentPart
	ToolCalls        []ToolCallResponse `json:"tool_calls,omitempty"`
	ToolCallID       string          `json:"tool_call_id,omitempty"`
	Name             string          `json:"name,omitempty"`
	ReasoningContent string          `json:"reasoning_content,omitempty"`
}

// ContentPart represents a single part in a multimodal content array.
type ContentPart struct {
	Type     string       `json:"type"`
	Text     string       `json:"text,omitempty"`
	ImageURL *ImageURLPart `json:"image_url,omitempty"`
}

// ImageURLPart holds the image URL for multimodal content.
type ImageURLPart struct {
	URL string `json:"url"`
}

// toAPIMessage converts a core.Message to APIChatMessage.
// When the message has images, content is serialized as a multimodal array.
// Otherwise it's a plain string.
func toAPIMessage(m Message) APIChatMessage {
	// Assistant messages with tool calls and empty content should send
	// content:null (not content:"") — required by OpenAI format and some
	// providers (e.g. GMICloud) silently fail with content:"".
	if m.Role == "assistant" && len(m.ToolCalls) > 0 && m.Content == "" {
		return APIChatMessage{
			Role:      m.Role,
			Content:   json.RawMessage("null"),
			ToolCalls: m.ToolCalls,
			// NOTE: ReasoningContent intentionally omitted — it's an
			// output-only field. Sending it in input messages causes
			// issues with some providers (GMICloud returns prompt_tokens=0).
		}
	}

	if len(m.Images) > 0 {
		parts := make([]ContentPart, 0, 1+len(m.Images))
		if m.Content != "" {
			parts = append(parts, ContentPart{Type: "text", Text: m.Content})
		}
		for _, img := range m.Images {
			parts = append(parts, ContentPart{
				Type:     "image_url",
				ImageURL: &ImageURLPart{URL: img.DataURL},
			})
		}
		content, _ := json.Marshal(parts)
		return APIChatMessage{
			Role:       m.Role,
			Content:    content,
			ToolCalls:  m.ToolCalls,
			ToolCallID: m.ToolCallID,
			Name:       m.Name,
		}
	}
	content, _ := json.Marshal(m.Content)
	return APIChatMessage{
		Role:       m.Role,
		Content:    content,
		ToolCalls:  m.ToolCalls,
		ToolCallID: m.ToolCallID,
		Name:       m.Name,
	}
}

// StreamOptions controls what additional data is included in streaming responses.
type StreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// sanitizeRequest strips llama.cpp-specific fields for cloud providers.
// Cloud APIs (Gemini, OpenRouter, OpenAI) reject unknown fields like
// top_k, repeat_penalty, presence_penalty, min_p, cache_prompt, and session_id.
// Builds a clean request with only fields cloud providers understand.
func (e *LLMClient) sanitizeRequest(req ChatCompletionRequest) ChatCompletionRequest {
	if !e.IsCloud() {
		return req
	}
	// Build a clean request — only include fields cloud providers accept.
	// Do NOT zero fields (which relies on omitempty); construct explicitly.
	clean := ChatCompletionRequest{
		Model:    req.Model,
		Messages: req.Messages,
		Stream:   req.Stream,
	}
	if len(req.Tools) > 0 {
		clean.Tools = req.Tools
	}
	if req.MaxTokens > 0 {
		clean.MaxTokens = req.MaxTokens
	}
	if req.Temperature > 0 {
		clean.Temperature = req.Temperature
	}
	if req.TopP > 0 {
		clean.TopP = req.TopP
	}
	if req.StreamOptions != nil {
		clean.StreamOptions = req.StreamOptions
	}
	if req.ResponseFormat != nil {
		clean.ResponseFormat = req.ResponseFormat
	}

	// OpenRouter provider routing: avoid GMICloud which silently fails
	// (returns prompt_tokens=0, completion_tokens=0) on large requests
	// with tool schemas.
	if e.isOpenRouter() {
		clean.Provider = &ProviderRouting{
			Ignore: []string{"GMICloud"},
		}
	}

	return clean
}

// prepareMessages applies cloud sanitization, cache breakpoints, Gemini
// thought_signature injection, and provider-specific role conversions
// to messages BEFORE converting to API wire format.
func (e *LLMClient) prepareMessages(messages []Message) []Message {
	if !e.IsCloud() {
		return messages
	}

	// Inject prompt caching on system messages for providers that support it.
	// Skip for DeepSeek — it does not accept system role messages at all,
	// so cache breakpoints (which create multiple system messages) are not applicable.
	if e.supportsCaching() && !e.isDeepSeek() {
		messages = injectCacheBreakpoints(messages, e.wantsCacheControl())
	}

	// Gemini 3 requires thought_signature on tool calls in conversation history.
	if strings.Contains(strings.ToLower(e.modelName), "gemini") {
		for i := range messages {
			m := &messages[i]
			if m.Role == "assistant" && len(m.ToolCalls) > 0 {
				for j := range m.ToolCalls {
					if m.ToolCalls[j].ThoughtSignature == "" {
						m.ToolCalls[j].ThoughtSignature = "c2tpcF90aG91Z2h0X3NpZ25hdHVyZV92YWxpZGF0b3I="
					}
				}
			}
		}
	}

	// DeepSeek (via OpenRouter) only accepts user and assistant roles.
	// Convert any system messages to user messages, prefixing the content
	// so the model still receives the instructions.
	if e.isDeepSeek() {
		for i := range messages {
			if messages[i].Role == "system" {
				messages[i].Content = "[System instruction: " + messages[i].Content + "]"
				messages[i].Role = "user"
			}
		}
		// Merge adjacent user messages — DeepSeek requires alternating
		// user/assistant roles. Converting system→user can create runs
		// of consecutive user messages.
		messages = mergeAdjacentSameRole(messages)
	}

	return messages
}

// isDeepSeek returns true if the model is a DeepSeek variant.
func (e *LLMClient) isDeepSeek() bool {
	return strings.Contains(strings.ToLower(e.modelName), "deepseek")
}

// isOpenRouter returns true if the engine connects to OpenRouter.
func (e *LLMClient) isOpenRouter() bool {
	return strings.Contains(e.baseURL, "openrouter.ai")
}

// mergeAdjacentSameRole merges consecutive messages with the same role
// by concatenating their content. This is needed after converting system→user
// for providers like DeepSeek that require strictly alternating roles.
func mergeAdjacentSameRole(msgs []Message) []Message {
	if len(msgs) <= 1 {
		return msgs
	}
	merged := make([]Message, 0, len(msgs))
	cur := msgs[0]
	for i := 1; i < len(msgs); i++ {
		if msgs[i].Role == cur.Role {
			// Same role — merge content
			cur.Content = cur.Content + "\n" + msgs[i].Content
		} else {
			merged = append(merged, cur)
			cur = msgs[i]
		}
	}
	merged = append(merged, cur)
	return merged
}

// supportsCaching returns true for providers that support prompt caching.
// Anthropic (via OpenRouter) and DeepSeek support cache_control: ephemeral.
// Gemini uses automatic prefix caching — split the message but skip cache_control.
func (e *LLMClient) supportsCaching() bool {
	name := strings.ToLower(e.modelName)
	return strings.Contains(name, "claude") ||
		strings.Contains(name, "deepseek") ||
		strings.Contains(name, "anthropic") ||
		strings.Contains(name, "gemini")
}

// wantsCacheControl returns true for providers that use the explicit cache_control field.
// Gemini does automatic prefix caching and doesn't support the cache_control parameter.
func (e *LLMClient) wantsCacheControl() bool {
	name := strings.ToLower(e.modelName)
	return strings.Contains(name, "claude") ||
		strings.Contains(name, "deepseek") ||
		strings.Contains(name, "anthropic")
}

// dynamicBoundary is the marker that splits stable from dynamic prompt content.
const dynamicBoundary = "<system_prompt_dynamic_boundary>"

// injectCacheBreakpoints splits system messages at the dynamic boundary.
// If withCacheControl is true, marks the stable portion with cache_control: ephemeral
// (Anthropic/DeepSeek). If false, just splits the message for automatic prefix caching (Gemini).
func injectCacheBreakpoints(messages []Message, withCacheControl bool) []Message {
	for i := range messages {
		m := &messages[i]
		if m.Role != "system" {
			continue
		}
		idx := strings.Index(m.Content, dynamicBoundary)
		if idx == -1 {
			continue
		}

		// Split: stable part (before boundary) + dynamic part (after boundary)
		stable := strings.TrimSpace(m.Content[:idx])
		dynamic := strings.TrimSpace(m.Content[idx+len(dynamicBoundary):])

		// Replace the single system message with two: stable (cached) + dynamic
		var newMsgs []Message
		if i > 0 {
			newMsgs = append(newMsgs, messages[:i]...)
		}
		if stable != "" {
			msg := Message{
				Role:    "system",
				Content: stable,
			}
			if withCacheControl {
				msg.CacheControl = &CacheControl{Type: "ephemeral"}
			}
			newMsgs = append(newMsgs, msg)
		}
		if dynamic != "" {
			newMsgs = append(newMsgs, Message{
				Role:    "system",
				Content: dynamic,
			})
		}
		if i+1 < len(messages) {
			newMsgs = append(newMsgs, messages[i+1:]...)
		}
		return newMsgs
	}
	return messages
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
		e.logger.Error("chat non-stream failed",
			zap.String("url", e.baseURL),
			zap.Int("status", resp.StatusCode),
			zap.String("response", string(respBody)),
		)
		return nil, fmt.Errorf("chat API HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var result ChatCompletionResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("decode response: %w (body: %s)", err, string(respBody))
	}

	// Unwrap MCP Hub response envelope: {"status":"success","data":{...}}
	// The hub wraps OpenAI-format responses; direct parse finds no choices.
	if len(result.Choices) == 0 {
		var wrapper struct {
			Data json.RawMessage `json:"data"`
		}
		if json.Unmarshal(respBody, &wrapper) == nil && len(wrapper.Data) > 0 {
			var inner ChatCompletionResponse
			if json.Unmarshal(wrapper.Data, &inner) == nil && len(inner.Choices) > 0 {
				result = inner
			}
		}
	}

	return &result, nil
}

// chatCompleteStream sends a chat completion request and streams chunks via callback.
// SSE format: "data: {json}\n\n" with delta objects containing content, reasoning_content, or tool_calls.
// The final chunk includes a usage field with prompt_tokens and completion_tokens.
func (e *LLMClient) chatCompleteStream(ctx context.Context, req ChatCompletionRequest, onChunk func(delta StreamDelta, finish FinishReason, usage *StreamUsage) bool) error {
	req.Stream = true
	// Request usage data in streaming response (like Pi-Mono does)
	// This makes llama-server send a usage-only chunk at the end of the stream.
	req.StreamOptions = &StreamOptions{IncludeUsage: true}
	sanitized := e.sanitizeRequest(req)
	body, err := json.Marshal(sanitized)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	// Log request structure for cloud providers
	if e.IsCloud() {
		msgSummary := make([]string, len(sanitized.Messages))
		for i, m := range sanitized.Messages {
			content := string(m.Content)
			if len(content) > 80 {
				content = content[:80] + "..."
			}
			summary := m.Role
			if m.ToolCallID != "" {
				summary += " tool_call_id=" + m.ToolCallID
			}
			if len(m.ToolCalls) > 0 {
				summary += fmt.Sprintf(" tool_calls=%d", len(m.ToolCalls))
			}
			if m.Name != "" {
				summary += " name=" + m.Name
			}
			summary += " " + content
			msgSummary[i] = summary
		}
		e.logger.Debug("Cloud request",
			zap.String("model", sanitized.Model),
			zap.Int("messages", len(sanitized.Messages)),
			zap.Strings("msg_roles", msgSummary),
			zap.Int("tools", len(sanitized.Tools)),
		)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", e.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if e.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+e.apiKey)
	}

	resp, err := e.streamClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("chat stream request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		e.logger.Error("chat stream failed",
			zap.String("url", e.baseURL),
			zap.Int("status", resp.StatusCode),
			zap.String("response", string(respBody)),
			zap.String("request", string(body[:min(len(body), 2000)])),
		)
		return fmt.Errorf("chat API HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	chunkCount := 0
	gotFinish := false
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

		// Debug: log special chunks for cloud providers
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
			if choice.FinishReason != "" {
				gotFinish = true
			}
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
			zap.Bool("gotFinish", gotFinish),
			zap.String("model", e.modelName),
		)
	}

	// Detect aborted streams — cloud providers sometimes drop the connection
	// after a few chunks without sending finish_reason or [DONE].
	// Treat this as a transient error so the caller can retry.
	if !gotFinish && chunkCount < 10 {
		return fmt.Errorf("stream aborted after %d chunks (no finish_reason)", chunkCount)
	}

	return scanner.Err()
}

