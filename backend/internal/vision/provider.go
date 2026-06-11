package vision

import (
	"context"
	"log"
	"time"
)

// Description is the result of a vision provider analyzing an image.
type Description struct {
	Text      string
	Provider  string // "mcp", "native", "cloud"
	LatencyMs int64
}

// ImageInput is the image data to describe.
type ImageInput struct {
	Base64   string // raw base64 image data (no data: prefix)
	MIMEType string // "image/png", "image/jpeg"
	Source   string // tool name that produced the image
	Prompt   string // what to describe
}

// Provider describes images using a specific vision backend.
type Provider interface {
	Name() string
	Describe(ctx context.Context, img ImageInput) (Description, error)
	IsAvailable(ctx context.Context) bool
}

// FallbackChain tries providers in order, returning the first success.
type FallbackChain struct {
	providers []Provider
	timeout   time.Duration
	logger    *log.Logger
}

// NewFallbackChain creates a vision fallback chain.
func NewFallbackChain(providers ...Provider) *FallbackChain {
	return &FallbackChain{
		providers: providers,
		timeout:   30 * time.Second,
		logger:    log.Default(),
	}
}

// SetTimeout configures the per-provider timeout.
func (c *FallbackChain) SetTimeout(d time.Duration) {
	c.timeout = d
}

// Describe tries each provider in order until one succeeds.
func (c *FallbackChain) Describe(ctx context.Context, img ImageInput) (Description, error) {
	for _, p := range c.providers {
		provCtx, cancel := context.WithTimeout(ctx, c.timeout)
		start := time.Now()

		if !p.IsAvailable(provCtx) {
			cancel()
			c.logger.Printf("vision: provider %s not available, skipping", p.Name())
			continue
		}

		desc, err := p.Describe(provCtx, img)
		cancel()
		if err != nil {
			c.logger.Printf("vision: provider %s failed: %v", p.Name(), err)
			continue
		}

		desc.LatencyMs = time.Since(start).Milliseconds()
		c.logger.Printf("vision: %s described image from %s in %dms", p.Name(), img.Source, desc.LatencyMs)
		return desc, nil
	}

	return Description{}, ErrNoProvider
}

// ErrNoProvider is returned when no vision provider can describe the image.
var ErrNoProvider = &VisionError{Msg: "no vision provider available"}

// VisionError is a vision-specific error.
type VisionError struct {
	Msg string
}

func (e *VisionError) Error() string { return "vision: " + e.Msg }
