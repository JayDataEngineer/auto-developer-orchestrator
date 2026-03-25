package jules

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client handles communication with the Jules API
type Client struct {
	apiKey     string
	httpClient *http.Client
	baseURL    string
}

// NewClient creates a new Jules API client
func NewClient(apiKey string) *Client {
	return &Client{
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 120 * time.Second},
		baseURL:    "https://jules.googleapis.com/v1alpha",
	}
}

// CreateSessionRequest represents a request to create a Jules session
type CreateSessionRequest struct {
	Prompt         string        `json:"prompt"`
	SourceContext  SourceContext `json:"sourceContext"`
	AutomationMode string        `json:"automationMode"`
	Title          string        `json:"title"`
}

// SourceContext represents the source code context for Jules
type SourceContext struct {
	Source string `json:"source"`
}

// Session represents a Jules session
type Session struct {
	ID    string `json:"id"`
	State string `json:"state"` // PLANNING, IN_PROGRESS, COMPLETED
	Plan  *Plan  `json:"plan,omitempty"`
}

// Plan represents a Jules execution plan
type Plan struct {
	Files       []string `json:"files"`
	Description string   `json:"description"`
	Changes     string   `json:"changes"`
}

// CreateSession creates a new Jules session
func (c *Client) CreateSession(ctx context.Context, req CreateSessionRequest) (*Session, error) {
	// If no API key, return mock response
	if c.apiKey == "" {
		return &Session{
			ID:    "mock-session-" + fmt.Sprintf("%d", time.Now().UnixNano()),
			State: "PLANNING",
		}, nil
	}

	url := c.baseURL + "/sessions"

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-goog-api-key", c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Jules API error: %d %s", resp.StatusCode, string(body))
	}

	var session Session
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &session, nil
}

// GetSession retrieves a Jules session by ID
func (c *Client) GetSession(ctx context.Context, sessionID string) (*Session, error) {
	// If no API key, return mock response
	if c.apiKey == "" {
		return &Session{
			ID:    sessionID,
			State: "COMPLETED",
		}, nil
	}

	url := fmt.Sprintf("%s/sessions/%s", c.baseURL, sessionID)

	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("x-goog-api-key", c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Jules API error: %d %s", resp.StatusCode, string(body))
	}

	var session Session
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &session, nil
}

// ApprovePlan approves a Jules session plan
func (c *Client) ApprovePlan(ctx context.Context, sessionID string) error {
	if c.apiKey == "" {
		return nil // Mock success
	}

	url := fmt.Sprintf("%s/sessions/%s:approvePlan", c.baseURL, sessionID)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("x-goog-api-key", c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Jules API error: %d %s", resp.StatusCode, string(body))
	}

	return nil
}
