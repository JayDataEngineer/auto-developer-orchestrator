package framestream

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"sync/atomic"
	"testing"
	"time"
)

func createTestPNG(w, h int, r, g, b uint8) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	c := color.RGBA{r, g, b, 255}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

func TestChangeDetector_IdenticalFrames(t *testing.T) {
	d := newChangeDetector(16)
	png := createTestPNG(100, 100, 128, 128, 128)

	first := d.Score(png)
	if first != 1.0 {
		t.Fatalf("first frame score = %f, want 1.0", first)
	}

	second := d.Score(png)
	if second > 0.01 {
		t.Fatalf("identical frame score = %f, want < 0.01", second)
	}
}

func TestChangeDetector_DifferentFrames(t *testing.T) {
	d := newChangeDetector(16)
	black := createTestPNG(100, 100, 0, 0, 0)
	white := createTestPNG(100, 100, 255, 255, 255)

	d.Score(black) // first frame
	score := d.Score(white)

	if score < 0.1 {
		t.Fatalf("black→white score = %f, want > 0.1", score)
	}
}

func TestChangeDetector_SimilarFrames(t *testing.T) {
	d := newChangeDetector(16)
	a := createTestPNG(100, 100, 100, 100, 100)
	b := createTestPNG(100, 100, 110, 110, 110)

	d.Score(a)
	score := d.Score(b)

	if score > 0.2 {
		t.Fatalf("similar frames score = %f, want < 0.2", score)
	}
	if score < 0.001 {
		t.Fatalf("similar frames score = %f, want > 0.001", score)
	}
}

func TestRingBuffer_PushLast(t *testing.T) {
	rb := newRingBuffer(3)
	if rb.Last() != nil {
		t.Fatal("empty buffer should return nil")
	}

	rb.Push(Frame{Width: 1})
	rb.Push(Frame{Width: 2})
	rb.Push(Frame{Width: 3})

	last := rb.Last()
	if last == nil || last.Width != 3 {
		t.Fatalf("last = %+v, want Width=3", last)
	}

	// Wrap around
	rb.Push(Frame{Width: 4})
	last = rb.Last()
	if last == nil || last.Width != 4 {
		t.Fatalf("after wrap, last = %+v, want Width=4", last)
	}
	if rb.Len() != 3 {
		t.Fatalf("len = %d, want 3", rb.Len())
	}
}

func TestRingBuffer_Since(t *testing.T) {
	rb := newRingBuffer(10)

	t0 := time.Now()
	rb.Push(Frame{CapturedAt: t0})
	rb.Push(Frame{CapturedAt: t0.Add(1 * time.Second)})
	rb.Push(Frame{CapturedAt: t0.Add(2 * time.Second)})

	frames := rb.Since(t0.Add(1500 * time.Millisecond))
	if len(frames) != 1 {
		t.Fatalf("Since(t0+1.5s) = %d frames, want 1", len(frames))
	}
	if len(frames) > 0 && frames[0].CapturedAt != t0.Add(2*time.Second) {
		t.Fatalf("wrong frame returned")
	}

	// All frames
	all := rb.Since(time.Time{})
	if len(all) != 3 {
		t.Fatalf("Since(zero) = %d frames, want 3", len(all))
	}
}

func TestStreamer_StartStop(t *testing.T) {
	var captures atomic.Int32
	cfg := Config{FPS: 10, BufferSize: 5, ThumbnailSize: 8}

	png := createTestPNG(50, 50, 128, 128, 128)
	capture := func(ctx context.Context) ([]byte, error) {
		captures.Add(1)
		return png, nil
	}

	s := NewStreamer(cfg, capture)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s.Start(ctx)
	time.Sleep(500 * time.Millisecond)
	s.Stop()

	if !s.IsRunning() {
		// Good, stopped
	}

	if s.Len() == 0 {
		t.Fatal("streamer captured 0 frames in 500ms at 10fps")
	}

	c := captures.Load()
	if c < 2 {
		t.Fatalf("only %d captures in 500ms at 10fps, want >= 2", c)
	}
}

func TestStreamer_OnChange(t *testing.T) {
	var changes atomic.Int32
	cfg := Config{FPS: 20, BufferSize: 10, ChangeThreshold: 0.1, ThumbnailSize: 8}

	black := createTestPNG(50, 50, 0, 0, 0)
	white := createTestPNG(50, 50, 255, 255, 255)

	var frameNum atomic.Int32
	capture := func(ctx context.Context) ([]byte, error) {
		n := frameNum.Add(1)
		if n%2 == 0 {
			return black, nil
		}
		return white, nil
	}

	s := NewStreamer(cfg, capture)
	s.OnChange(func(f Frame) {
		changes.Add(1)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s.Start(ctx)
	time.Sleep(600 * time.Millisecond)
	s.Stop()

	c := changes.Load()
	if c == 0 {
		t.Fatal("OnChange never fired despite alternating black/white frames")
	}
}

func TestStreamer_RecentFrames(t *testing.T) {
	cfg := Config{FPS: 10, BufferSize: 20, ThumbnailSize: 8}

	png := createTestPNG(50, 50, 100, 100, 100)
	capture := func(ctx context.Context) ([]byte, error) { return png, nil }

	s := NewStreamer(cfg, capture)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s.Start(ctx)
	time.Sleep(500 * time.Millisecond)
	s.Stop()

	recent := s.RecentFrames(time.Now().Add(-1 * time.Hour))
	if len(recent) == 0 {
		t.Fatal("RecentFrames returned nothing")
	}

	// All frames should have valid dimensions
	for _, f := range recent {
		if f.Width != 50 || f.Height != 50 {
			t.Fatalf("frame dimensions = %dx%d, want 50x50", f.Width, f.Height)
		}
		if f.ChangeScore < 0 || f.ChangeScore > 1 {
			t.Fatalf("change score = %f, out of range [0,1]", f.ChangeScore)
		}
	}
}
