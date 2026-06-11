// Package retry provides exponential backoff retry utilities.
package retry

import (
	"context"
	"math"
	"math/rand"
	"time"
)

// Config holds retry configuration.
type Config struct {
	MaxAttempts int           // total attempts including the first try
	BaseDelay   time.Duration // initial backoff delay
	MaxDelay    time.Duration // upper cap on delay
	Jitter      bool          // randomize delay +/- 25%
}

// Defaults for common scenarios.
var (
	// Short is for quick operations (CDP readiness, tool retries).
	Short = Config{MaxAttempts: 3, BaseDelay: 500 * time.Millisecond, MaxDelay: 10 * time.Second, Jitter: true}

	// Medium is for network-bound operations (provider calls, browser actions).
	Medium = Config{MaxAttempts: 5, BaseDelay: 1 * time.Second, MaxDelay: 30 * time.Second, Jitter: true}

	// Long is for startup polling (Chrome CDP, container services).
	Long = Config{MaxAttempts: 10, BaseDelay: 1 * time.Second, MaxDelay: 30 * time.Second, Jitter: true}
)

// Delay returns the backoff duration for a 0-indexed attempt.
func (c Config) Delay(attempt int) time.Duration {
	d := min(time.Duration(float64(c.BaseDelay)*math.Pow(2, float64(attempt))), c.MaxDelay)
	if c.Jitter {
		// +/- 25% randomization to avoid thundering herd
		d = time.Duration(float64(d) * (0.75 + rand.Float64()*0.5))
	}
	return d
}

// Do executes fn with exponential backoff on error.
// Stops early if the context is cancelled.
// Returns nil on the first successful attempt, or the last error.
func Do(ctx context.Context, cfg Config, fn func() error) error {
	var lastErr error
	for i := 0; i < cfg.MaxAttempts; i++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if lastErr = fn(); lastErr == nil {
			return nil
		}
		if i < cfg.MaxAttempts-1 {
			delay := cfg.Delay(i)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}
	}
	return lastErr
}

// DoWithResult executes fn with exponential backoff, returning a result and error.
func DoWithResult[T any](ctx context.Context, cfg Config, fn func() (T, error)) (T, error) {
	var (
		lastErr error
		zero    T
	)
	for i := 0; i < cfg.MaxAttempts; i++ {
		if ctx.Err() != nil {
			return zero, ctx.Err()
		}
		result, err := fn()
		if err == nil {
			return result, nil
		}
		lastErr = err
		if i < cfg.MaxAttempts-1 {
			delay := cfg.Delay(i)
			select {
			case <-ctx.Done():
				return zero, ctx.Err()
			case <-time.After(delay):
			}
		}
	}
	return zero, lastErr
}
