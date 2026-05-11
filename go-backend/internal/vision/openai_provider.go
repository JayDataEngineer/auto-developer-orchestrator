package vision

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// OpenAIProvider describes images via an OpenAI-compatible /v1/chat/completions endpoint.
// Used for remote vision models (e.g., qwen on the MCP hub).
type OpenAIProvider struct {
	baseURL string
	model   string
	client  *http.Client
}

// NewOpenAIProvider creates a vision provider backed by an OpenAI-compatible chat completions endpoint.
func NewOpenAIProvider(baseURL, model string) *OpenAIProvider {
	return &OpenAIProvider{
		baseURL: baseURL,
		model:   model,
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

func (p *OpenAIProvider) Name() string { return "openai-vision" }

func (p *OpenAIProvider) IsAvailable(ctx context.Context) bool {
	hcCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(hcCtx, "GET", p.baseURL+"/v1/models", nil)
	if err != nil {
		return false
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode < 500
}

func (p *OpenAIProvider) Describe(ctx context.Context, img ImageInput) (Description, error) {
	prompt := img.Prompt
	if prompt == "" {
		prompt = "Describe what you see in this image in detail."
	}

	dataURI := "data:" + img.MIMEType + ";base64," + img.Base64

	reqBody := map[string]any{
		"model": p.model,
		"messages": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{"type": "text", "text": prompt},
					{"type": "image_url", "image_url": map[string]string{"url": dataURI}},
				},
			},
		},
		"max_tokens": 500,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return Description{}, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Description{}, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return Description{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return Description{}, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return Description{}, fmt.Errorf("vision API HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return Description{}, fmt.Errorf("parse response: %w", err)
	}

	if len(result.Choices) == 0 || result.Choices[0].Message.Content == "" {
		return Description{}, fmt.Errorf("empty response from vision model")
	}

	return Description{
		Text:     result.Choices[0].Message.Content,
		Provider: "openai-vision",
	}, nil
}
