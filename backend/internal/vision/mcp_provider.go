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
	client      *mcp.MultiClient
	imageServer *ImageServer // writes base64 to temp file, serves via HTTP
}

// NewMCPProvider creates a vision provider backed by the MCP media-analysis server.
func NewMCPProvider(client *mcp.MultiClient, imageServer *ImageServer) *MCPProvider {
	return &MCPProvider{client: client, imageServer: imageServer}
}

func (p *MCPProvider) Name() string { return "mcp" }

func (p *MCPProvider) IsAvailable(ctx context.Context) bool {
	if p.client == nil {
		return false
	}
	return p.client.HasTool("analyze_image")
}

func (p *MCPProvider) Describe(ctx context.Context, img ImageInput) (Description, error) {
	imageURL, err := uploadToMCP(ctx, p.client, img.Base64, img.MIMEType)
	if err != nil {
		return Description{}, fmt.Errorf("upload to MCP: %w", err)
	}

	prompt := img.Prompt
	if prompt == "" {
		prompt = "Describe what you see in this image in detail."
	}

	result, err := p.client.CallTool(ctx, "analyze_image", map[string]any{
		"imageSource": imageURL,
		"prompt":      prompt,
	})
	if err != nil {
		return Description{}, fmt.Errorf("MCP analyze_image: %w", err)
	}

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

// uploadToMCP uploads base64 image data to the MCP server via its "upload" tool
// and returns a local URL (accessible from the MCP server itself) for use with
// vision tools like ground_ui and analyze_image.
// This avoids needing the MCP server to reach back to our Tailscale IP.
func uploadToMCP(ctx context.Context, client *mcp.MultiClient, b64, mimeType string) (string, error) {
	result, err := client.CallTool(ctx, "upload", map[string]any{
		"data":      b64,
		"mime_type": mimeType,
	})
	if err != nil {
		return "", fmt.Errorf("MCP upload: %w", err)
	}

	// Parse JSON result to extract the URL
	var obj map[string]any
	if err := json.Unmarshal([]byte(result), &obj); err == nil {
		if url, ok := obj["url"].(string); ok && url != "" {
			return url, nil
		}
	}
	// Some MCP servers return plain URL text
	if result != "" {
		return result, nil
	}
	return "", fmt.Errorf("upload returned no URL")
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
