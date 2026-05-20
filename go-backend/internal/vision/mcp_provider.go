package vision

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/mcp"
)

// MCPProvider describes images via the MCP analyze_image tool on the Ray cluster.
type MCPProvider struct {
	client *mcp.MultiClient
}

// NewMCPProvider creates a vision provider backed by the MCP media-analysis server.
func NewMCPProvider(client *mcp.MultiClient) *MCPProvider {
	return &MCPProvider{client: client}
}

func (p *MCPProvider) Name() string { return "mcp" }

func (p *MCPProvider) IsAvailable(ctx context.Context) bool {
	if p.client == nil {
		return false
	}
	return p.client.HasTool("analyze_image")
}

func (p *MCPProvider) Describe(ctx context.Context, img ImageInput) (Description, error) {
	dataURI := "data:" + img.MIMEType + ";base64," + img.Base64

	prompt := img.Prompt
	if prompt == "" {
		prompt = "Describe what you see in this image in detail."
	}

	result, err := p.client.CallTool(ctx, "analyze_image", map[string]any{
		"imageSource": dataURI,
		"prompt":      prompt,
	})
	if err != nil {
		return Description{}, fmt.Errorf("MCP analyze_image: %w", err)
	}

	// The MCP tool returns text content. Try to parse as JSON with a description field.
	text := extractMCPText(result)

	return Description{
		Text:     text,
		Provider: "mcp",
	}, nil
}

// extractMCPText tries to pull a text description from the MCP result string.
// The result may be plain text, or JSON with various structures.
func extractMCPText(raw string) string {
	// Try JSON parse
	var obj map[string]any
	if err := json.Unmarshal([]byte(raw), &obj); err == nil {
		// Common patterns: {"description": "..."}, {"text": "..."}, {"content": "..."}
		for _, key := range []string{"description", "text", "content", "caption"} {
			if s, ok := obj[key].(string); ok && s != "" {
				return s
			}
		}
		// Fallback: return the whole thing as JSON
		return raw
	}
	return raw
}

// Phi4Provider describes images via the MCP phi4_vision tool on the media server.
// Uses Gemma-based phi4 model for high-quality scene understanding — better for
// browser/desktop screenshots where you need layout, text, and button understanding.
type Phi4Provider struct {
	client *mcp.MultiClient
}

// NewPhi4Provider creates a vision provider backed by the phi4_vision MCP tool.
func NewPhi4Provider(client *mcp.MultiClient) *Phi4Provider {
	return &Phi4Provider{client: client}
}

func (p *Phi4Provider) Name() string { return "phi4" }

func (p *Phi4Provider) IsAvailable(ctx context.Context) bool {
	if p.client == nil {
		return false
	}
	return p.client.HasTool("phi4_vision")
}

func (p *Phi4Provider) Describe(ctx context.Context, img ImageInput) (Description, error) {
	dataURI := "data:" + img.MIMEType + ";base64," + img.Base64

	prompt := img.Prompt
	if prompt == "" {
		prompt = "Describe what you see in this image in detail."
	}

	result, err := p.client.CallTool(ctx, "phi4_vision", map[string]any{
		"imageSource": dataURI,
		"prompt":      prompt,
	})
	if err != nil {
		return Description{}, fmt.Errorf("MCP phi4_vision: %w", err)
	}

	text := extractMCPText(result)

	return Description{
		Text:     text,
		Provider: "phi4",
	}, nil
}

// NativeProvider wraps the browser.VisionClient for llama.cpp-based vision.
// It health-checks the server before use.
type NativeProvider struct {
	describeFunc func(ctx context.Context, b64, mimeType, prompt string) (string, error)
	healthCheck  func(ctx context.Context) bool
}

// NativeProviderOpt configures the native provider.
type NativeProviderOpt struct {
	// Describe calls a vision model with base64 image data.
	DescribeFunc func(ctx context.Context, b64, mimeType, prompt string) (string, error)
	// HealthCheck returns true if the vision server is reachable.
	HealthCheck func(ctx context.Context) bool
}

// NewNativeProvider creates a vision provider backed by the local llama.cpp server.
func NewNativeProvider(opt NativeProviderOpt) *NativeProvider {
	return &NativeProvider{
		describeFunc: opt.DescribeFunc,
		healthCheck:  opt.HealthCheck,
	}
}

func (p *NativeProvider) Name() string { return "native" }

func (p *NativeProvider) IsAvailable(ctx context.Context) bool {
	if p.healthCheck == nil {
		return false
	}
	hcCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return p.healthCheck(hcCtx)
}

func (p *NativeProvider) Describe(ctx context.Context, img ImageInput) (Description, error) {
	if p.describeFunc == nil {
		return Description{}, fmt.Errorf("native: no describe function configured")
	}

	prompt := img.Prompt
	if prompt == "" {
		prompt = "Describe what you see in this image in detail."
	}

	text, err := p.describeFunc(ctx, img.Base64, img.MIMEType, prompt)
	if err != nil {
		return Description{}, fmt.Errorf("native vision: %w", err)
	}

	return Description{
		Text:     text,
		Provider: "native",
	}, nil
}
