package framestream

import (
	"bytes"
	"context"
	"image"
	"log"
	"sync"
	"time"
)

// Frame is a captured screenshot with change metadata.
type Frame struct {
	Data        []byte    // raw PNG bytes
	CapturedAt  time.Time
	Width       int
	Height      int
	ChangeScore float64 // 0 = identical to previous, 1 = completely different
}

// Config controls frame streaming behavior.
type Config struct {
	FPS             float64 // capture rate (default: 1.0)
	BufferSize      int     // frames to keep (default: 30)
	ChangeThreshold float64 // 0-1, "significant change" (default: 0.05)
	ThumbnailSize   int     // NxN downscale for diff (default: 64)
}

func DefaultConfig() Config {
	return Config{
		FPS:             1.0,
		BufferSize:      30,
		ChangeThreshold: 0.05,
		ThumbnailSize:   64,
	}
}

// CaptureFunc captures a screenshot and returns raw PNG bytes.
type CaptureFunc func(ctx context.Context) ([]byte, error)

// Streamer captures frames at a fixed rate, detects changes, and buffers them.
type Streamer struct {
	config   Config
	capture  CaptureFunc
	buffer   *ringBuffer
	detector *changeDetector

	stopCh  chan struct{}
	doneCh  chan struct{}
	mu      sync.Mutex
	running bool

	onChangeMu sync.RWMutex
	onChange   func(Frame)
}

// NewStreamer creates a frame streamer with the given config and capture function.
func NewStreamer(cfg Config, capture CaptureFunc) *Streamer {
	if cfg.FPS <= 0 {
		cfg.FPS = 1.0
	}
	if cfg.BufferSize <= 0 {
		cfg.BufferSize = 30
	}
	if cfg.ChangeThreshold <= 0 {
		cfg.ChangeThreshold = 0.05
	}
	if cfg.ThumbnailSize <= 0 {
		cfg.ThumbnailSize = 64
	}
	return &Streamer{
		config:   cfg,
		capture:  capture,
		buffer:   newRingBuffer(cfg.BufferSize),
		detector: newChangeDetector(cfg.ThumbnailSize),
	}
}

// Start begins capturing frames in a background goroutine.
func (s *Streamer) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = true
	s.stopCh = make(chan struct{})
	s.doneCh = make(chan struct{})
	s.mu.Unlock()

	go s.run(ctx)
	return nil
}

// Stop signals the capture goroutine to stop and waits for it to finish.
func (s *Streamer) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	close(s.stopCh)
	s.mu.Unlock()
	<-s.doneCh

	s.mu.Lock()
	s.running = false
	s.mu.Unlock()
}

// IsRunning returns whether the streamer is actively capturing.
func (s *Streamer) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// RecentFrames returns frames captured after the given time.
func (s *Streamer) RecentFrames(since time.Time) []Frame {
	return s.buffer.Since(since)
}

// LastFrame returns the most recent frame, or nil if empty.
func (s *Streamer) LastFrame() *Frame {
	return s.buffer.Last()
}

// Len returns the number of frames in the buffer.
func (s *Streamer) Len() int {
	return s.buffer.Len()
}

// OnChange registers a callback for significant frame changes.
func (s *Streamer) OnChange(fn func(Frame)) {
	s.onChangeMu.Lock()
	s.onChange = fn
	s.onChangeMu.Unlock()
}

func (s *Streamer) run(ctx context.Context) {
	defer close(s.doneCh)

	interval := time.Duration(float64(time.Second) / s.config.FPS)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-ticker.C:
			captureCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			pngData, err := s.capture(captureCtx)
			cancel()
			if err != nil {
				continue
			}

			score := s.detector.Score(pngData)

			frame := Frame{
				Data:        pngData,
				CapturedAt:  time.Now(),
				ChangeScore: score,
			}

			if img, _, err := image.Decode(bytes.NewReader(pngData)); err == nil {
				b := img.Bounds()
				frame.Width = b.Dx()
				frame.Height = b.Dy()
			}

			s.buffer.Push(frame)

			if score > s.config.ChangeThreshold {
				s.onChangeMu.RLock()
				fn := s.onChange
				s.onChangeMu.RUnlock()
				if fn != nil {
					fn(frame)
				}
			}
		}
	}
}

// ringBuffer is a thread-safe circular buffer of frames.
type ringBuffer struct {
	frames []Frame
	cap    int
	head   int // next write position
	count  int
	mu     sync.RWMutex
}

func newRingBuffer(cap int) *ringBuffer {
	return &ringBuffer{
		frames: make([]Frame, cap),
		cap:    cap,
	}
}

func (rb *ringBuffer) Push(f Frame) {
	rb.mu.Lock()
	rb.frames[rb.head] = f
	rb.head = (rb.head + 1) % rb.cap
	if rb.count < rb.cap {
		rb.count++
	}
	rb.mu.Unlock()
}

func (rb *ringBuffer) Last() *Frame {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	if rb.count == 0 {
		return nil
	}
	idx := (rb.head - 1 + rb.cap) % rb.cap
	f := rb.frames[idx]
	return &f
}

func (rb *ringBuffer) Since(t time.Time) []Frame {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	var result []Frame
	start := (rb.head - rb.count + rb.cap) % rb.cap
	for i := 0; i < rb.count; i++ {
		idx := (start + i) % rb.cap
		if rb.frames[idx].CapturedAt.After(t) {
			result = append(result, rb.frames[idx])
		}
	}
	return result
}

func (rb *ringBuffer) Len() int {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	return rb.count
}

// Silences unused import warning — detector.go uses image/png via init().
var _ = log.Output
