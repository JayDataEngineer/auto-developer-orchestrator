package retry

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestConfig_Delay_ExponentialGrowthWithoutJitter(t *testing.T) {
	cfg := Config{BaseDelay: 100 * time.Millisecond, MaxDelay: 10 * time.Second, Jitter: false}
	// attempt 0 -> BaseDelay, each subsequent doubles until MaxDelay.
	want := []time.Duration{
		100 * time.Millisecond,
		200 * time.Millisecond,
		400 * time.Millisecond,
		800 * time.Millisecond,
		1600 * time.Millisecond,
	}
	for i, w := range want {
		if got := cfg.Delay(i); got != w {
			t.Errorf("Delay(%d) = %v, want %v", i, got, w)
		}
	}
}

func TestConfig_Delay_MaxDelayCap(t *testing.T) {
	cfg := Config{BaseDelay: 1 * time.Second, MaxDelay: 4 * time.Second, Jitter: false}
	// attempt 2 = 4s (cap), attempt 3 would be 8s but capped at 4s.
	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 1 * time.Second},
		{1, 2 * time.Second},
		{2, 4 * time.Second},
		{3, 4 * time.Second},
		{10, 4 * time.Second},
	}
	for _, tc := range tests {
		if got := cfg.Delay(tc.attempt); got != tc.want {
			t.Errorf("Delay(%d) = %v, want %v (cap)", tc.attempt, got, tc.want)
		}
	}
}

func TestConfig_Delay_JitterStaysWithinBounds(t *testing.T) {
	cfg := Config{BaseDelay: 1 * time.Second, MaxDelay: 10 * time.Second, Jitter: true}
	base := cfg.Delay(0)
	// Without jitter Delay(0) == BaseDelay; jitter multiplies 0.75x..1.25x.
	if base < 750*time.Millisecond || base > 1250*time.Millisecond {
		t.Errorf("jittered Delay(0) = %v, want within [%v, %v]", base, 750*time.Millisecond, 1250*time.Millisecond)
	}
	// Sample many attempts to confirm bounds always hold.
	for i := range 100 {
		d := cfg.Delay(0)
		if d < 750*time.Millisecond || d > 1250*time.Millisecond {
			t.Errorf("jittered Delay(0) sample %d = %v, out of bounds", i, d)
		}
	}
}

func TestDo_StopsOnFirstSuccess(t *testing.T) {
	calls := 0
	cfg := Config{MaxAttempts: 5, BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond}
	err := Do(context.Background(), cfg, func() error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call on first success, got %d", calls)
	}
}

func TestDo_RetriesUntilSuccess(t *testing.T) {
	calls := 0
	cfg := Config{MaxAttempts: 5, BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond}
	err := Do(context.Background(), cfg, func() error {
		calls++
		if calls < 3 {
			return errors.New("transient")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil error after retries, got %v", err)
	}
	if calls != 3 {
		t.Errorf("expected 3 calls before success, got %d", calls)
	}
}

func TestDo_AllFailReturnsLastError(t *testing.T) {
	cfg := Config{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond}
	lastErr := errors.New("permanent")
	err := Do(context.Background(), cfg, func() error {
		return lastErr
	})
	if !errors.Is(err, lastErr) {
		t.Errorf("expected last error %v, got %v", lastErr, err)
	}
}

func TestDo_ZeroAttemptsNoCalls(t *testing.T) {
	cfg := Config{MaxAttempts: 0, BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond}
	calls := 0
	err := Do(context.Background(), cfg, func() error {
		calls++
		return nil
	})
	if err != nil {
		t.Errorf("expected nil error with MaxAttempts=0, got %v", err)
	}
	if calls != 0 {
		t.Errorf("expected 0 calls with MaxAttempts=0, got %d", calls)
	}
}

func TestDo_ContextCancelledBetweenAttempts(t *testing.T) {
	cfg := Config{MaxAttempts: 10, BaseDelay: 50 * time.Millisecond, MaxDelay: 100 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	go func() {
		<-time.After(10 * time.Millisecond)
		cancel()
	}()
	err := Do(ctx, cfg, func() error {
		calls++
		return errors.New("keep retrying")
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestDoWithResult_Success(t *testing.T) {
	cfg := Config{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond}
	got, err := DoWithResult(context.Background(), cfg, func() (string, error) {
		return "ok", nil
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if got != "ok" {
		t.Errorf("expected result %q, got %q", "ok", got)
	}
}

func TestDoWithResult_ErrorReturnsZero(t *testing.T) {
	cfg := Config{MaxAttempts: 2, BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond}
	got, err := DoWithResult(context.Background(), cfg, func() (string, error) {
		return "ignored", errors.New("fail")
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got != "" {
		t.Errorf("expected zero value result on error, got %q", got)
	}
}

func TestDefaults_AreSensible(t *testing.T) {
	cfgs := []struct {
		name string
		cfg  Config
	}{
		{"Short", Short},
		{"Medium", Medium},
		{"Long", Long},
	}
	for _, c := range cfgs {
		if c.cfg.MaxAttempts < 1 {
			t.Errorf("%s.MaxAttempts = %d, want >= 1", c.name, c.cfg.MaxAttempts)
		}
		if c.cfg.BaseDelay <= 0 {
			t.Errorf("%s.BaseDelay = %v, want > 0", c.name, c.cfg.BaseDelay)
		}
		if c.cfg.MaxDelay < c.cfg.BaseDelay {
			t.Errorf("%s.MaxDelay (%v) < BaseDelay (%v)", c.name, c.cfg.MaxDelay, c.cfg.BaseDelay)
		}
		if !c.cfg.Jitter {
			t.Errorf("%s.Jitter = false, want true", c.name)
		}
	}
}
