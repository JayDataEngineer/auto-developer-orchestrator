package file

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/auto-developer-orchestrator/backend/internal/mcp"
)

// MediaDescriber provides descriptions of non-text files (images, audio, documents)
// via external analysis services. Implementations typically call MCP media tools.
type MediaDescriber interface {
	Describe(ctx context.Context, absPath string, toolName string) (string, error)
}

// MCPMediaDescriber implements MediaDescriber using the MCP media server.
// It reads the file, base64-encodes it, and passes it as a data URI.
type MCPMediaDescriber struct {
	Client *mcp.MultiClient
}

func (d *MCPMediaDescriber) Describe(ctx context.Context, absPath string, toolName string) (string, error) {
	data, err := os.ReadFile(absPath)
	if err != nil {
		return "", fmt.Errorf("read media file: %w", err)
	}

	ext := strings.ToLower(filepath.Ext(absPath))
	mime := extToMime(ext)
	b64 := base64.StdEncoding.EncodeToString(data)
	dataURI := "data:" + mime + ";base64," + b64

	var args map[string]any
	switch toolName {
	case "transcribe_audio":
		args = map[string]any{"audioSource": dataURI}
	case "kosmos_ocr":
		args = map[string]any{"imageSource": dataURI, "mode": "markdown"}
	default: // phi4_vision, analyze_image
		args = map[string]any{
			"imageSource": dataURI,
			"prompt":      "Describe the contents of this image in detail.",
		}
	}

	return d.Client.CallTool(ctx, toolName, args)
}

// isMultimodalExt checks if a file extension should be handled by the media pipeline.
// Returns the MCP tool name and true if it's a supported non-text format.
func isMultimodalExt(ext string) (string, bool) {
	e := strings.ToLower(ext)
	switch e {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".heic", ".heif", ".bmp":
		return "phi4_vision", true
	case ".pdf", ".ppt", ".pptx":
		return "kosmos_ocr", true
	case ".wav", ".mp3", ".aiff", ".aac", ".ogg", ".flac":
		return "transcribe_audio", true
	}
	return "", false
}

// extToMime maps file extensions to MIME types for data URI construction.
func extToMime(ext string) string {
	switch strings.ToLower(ext) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".heic", ".heif":
		return "image/heic"
	case ".bmp":
		return "image/bmp"
	case ".pdf":
		return "application/pdf"
	case ".ppt":
		return "application/vnd.ms-powerpoint"
	case ".pptx":
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	case ".wav":
		return "audio/wav"
	case ".mp3":
		return "audio/mpeg"
	case ".aiff":
		return "audio/aiff"
	case ".aac":
		return "audio/aac"
	case ".ogg":
		return "audio/ogg"
	case ".flac":
		return "audio/flac"
	default:
		return "application/octet-stream"
	}
}
