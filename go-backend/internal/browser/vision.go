package browser

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// VisionClient sends screenshots to a vision model via LiteLLM
type VisionClient struct {
	litellmURL string
	litellmKey string
	httpClient *http.Client
}

// NewVisionClient creates a new vision client
func NewVisionClient(litellmURL, litellmKey string) *VisionClient {
	return &VisionClient{
		litellmURL: litellmURL,
		litellmKey: litellmKey,
		httpClient: &http.Client{Timeout: 60 * time.Second},
	}
}

// DescribePage sends a screenshot to the vision model and returns a text description
func (vc *VisionClient) DescribePage(ctx context.Context, screenshot []byte) (string, error) {
	if vc.litellmURL == "" {
		return "", fmt.Errorf("LITELLM_PROXY_URL not configured")
	}

	b64 := base64.StdEncoding.EncodeToString(screenshot)

	payload := map[string]any{
		"model": "qwen-cloud-vision",
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

	url := vc.litellmURL + "/v1/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if vc.litellmKey != "" {
		req.Header.Set("Authorization", "Bearer "+vc.litellmKey)
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
