package browser

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// VisionClient sends screenshots to a vision model for page description.
// Routes directly to llama.cpp at localhost:8001 — no LiteLLM dependency.
type VisionClient struct {
	baseURL    string
	apiKey     string
	model      string
	httpClient *http.Client
}

// NewVisionClient creates a new vision client.
// Falls back to localhost:8001 (llama.cpp) when no explicit URL is provided.
func NewVisionClient(url, apiKey string) *VisionClient {
	if url == "" {
		url = "http://localhost:8001"
	}
	model := os.Getenv("VISION_MODEL")
	if model == "" {
		model = "gemma-4-26b"
	}
	return &VisionClient{
		baseURL:    url,
		apiKey:     apiKey,
		model:      model,
		httpClient: &http.Client{Timeout: 60 * time.Second},
	}
}

// DescribePage sends a screenshot to the vision model and returns a text description.
func (vc *VisionClient) DescribePage(ctx context.Context, screenshot []byte) (string, error) {
	b64 := base64.StdEncoding.EncodeToString(screenshot)

	payload := map[string]any{
		"model": vc.model,
		"messages": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{
						"type": "image_url",
						"image_url": map[string]string{
							"url": "data:image/png;base64," + b64,
						},
					},
					{
						"type": "text",
						"text": "Describe the web page shown in this screenshot. Focus on: the page title/heading, main content sections, navigation elements, forms and interactive elements, and any important text or data visible. Be concise but thorough.",
					},
				},
			},
		},
		"max_tokens": 1024,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal payload: %w", err)
	}

	url := vc.baseURL + "/v1/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if vc.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+vc.apiKey)
	}

	resp, err := vc.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("vision API request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read vision response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("vision API returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("failed to parse vision response: %w", err)
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("vision API returned no choices")
	}

	return result.Choices[0].Message.Content, nil
}
