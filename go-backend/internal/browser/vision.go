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

	"github.com/auto-developer-orchestrator/backend/internal/models"
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
// Falls back to cluster LLM -> localhost:8001 (llama.cpp) when no explicit URL is provided.
// Uses toolModel from config when available, otherwise VISION_MODEL env or hardcoded default.
func NewVisionClient(url, apiKey string, modelCfg *models.ModelConfig) *VisionClient {
	if url == "" {
		// Prefer cluster LLM endpoint (may have vision), fall back to local llama.cpp
		if hub := os.Getenv("MCP_HUB_ENDPOINT"); hub != "" {
			url = hub + "/llm"
		} else {
			url = "http://localhost:8001"
		}
	}
	var model string
	if modelCfg != nil {
		model = modelCfg.ToolModel().ModelId
	}
	if model == "" {
		model = os.Getenv("VISION_MODEL")
	}
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

// CheckHealth verifies the vision server is reachable by hitting the models endpoint.
func (vc *VisionClient) CheckHealth(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, "GET", vc.baseURL+"/v1/models", nil)
	if err != nil {
		return false
	}
	resp, err := vc.httpClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// requestVision sends an image to the vision model and returns the text response.
func (vc *VisionClient) requestVision(ctx context.Context, imageBytes []byte, mimeType string, prompt string, maxTokens int) (string, error) {
	b64 := base64.StdEncoding.EncodeToString(imageBytes)
	if mimeType == "" {
		mimeType = "image/png"
	}

	payload := map[string]any{
		"model": vc.model,
		"messages": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{
						"type": "image_url",
						"image_url": map[string]string{
							"url": fmt.Sprintf("data:%s;base64,%s", mimeType, b64),
						},
					},
					{
						"type": "text",
						"text": prompt,
					},
				},
			},
		},
		"max_tokens": maxTokens,
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

// DescribePage sends a screenshot to the vision model and returns a text description.
func (vc *VisionClient) DescribePage(ctx context.Context, screenshot []byte) (string, error) {
	return vc.requestVision(ctx, screenshot, "image/png",
		"Describe the web page shown in this screenshot. Focus on: the page title/heading, main content sections, navigation elements, forms and interactive elements, and any important text or data visible. Be concise but thorough.",
		1024)
}

// DescribeImage sends an arbitrary image (as bytes) to the vision model with a custom prompt.
// Unlike DescribePage which is tuned for web screenshots, this accepts any image type.
func (vc *VisionClient) DescribeImage(ctx context.Context, imageBytes []byte, prompt, mimeType string) (string, error) {
	return vc.requestVision(ctx, imageBytes, mimeType, prompt, 2048)
}
