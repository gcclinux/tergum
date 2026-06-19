package grpc

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestDefaultClientConfig(t *testing.T) {
	cfg := DefaultClientConfig()

	if cfg.InitialBackoff != 1*time.Second {
		t.Errorf("InitialBackoff = %v, want 1s", cfg.InitialBackoff)
	}
	if cfg.MaxBackoff != 30*time.Second {
		t.Errorf("MaxBackoff = %v, want 30s", cfg.MaxBackoff)
	}
	if cfg.BackoffFactor != 2.0 {
		t.Errorf("BackoffFactor = %v, want 2.0", cfg.BackoffFactor)
	}
	if cfg.MaxRetries != 5 {
		t.Errorf("MaxRetries = %v, want 5", cfg.MaxRetries)
	}
}

func TestClientConfig_ApplyDefaults(t *testing.T) {
	cfg := ClientConfig{} // all zero values
	cfg = cfg.applyDefaults()

	if cfg.InitialBackoff != 1*time.Second {
		t.Errorf("InitialBackoff = %v, want 1s", cfg.InitialBackoff)
	}
	if cfg.MaxBackoff != 30*time.Second {
		t.Errorf("MaxBackoff = %v, want 30s", cfg.MaxBackoff)
	}
	if cfg.BackoffFactor != 2.0 {
		t.Errorf("BackoffFactor = %v, want 2.0", cfg.BackoffFactor)
	}
	if cfg.MaxRetries != 5 {
		t.Errorf("MaxRetries = %v, want 5", cfg.MaxRetries)
	}

	// Custom values should be preserved
	custom := ClientConfig{
		InitialBackoff: 500 * time.Millisecond,
		MaxBackoff:     10 * time.Second,
		BackoffFactor:  3.0,
		MaxRetries:     10,
	}
	custom = custom.applyDefaults()
	if custom.InitialBackoff != 500*time.Millisecond {
		t.Errorf("custom InitialBackoff = %v, want 500ms", custom.InitialBackoff)
	}
	if custom.MaxBackoff != 10*time.Second {
		t.Errorf("custom MaxBackoff = %v, want 10s", custom.MaxBackoff)
	}
	if custom.BackoffFactor != 3.0 {
		t.Errorf("custom BackoffFactor = %v, want 3.0", custom.BackoffFactor)
	}
	if custom.MaxRetries != 10 {
		t.Errorf("custom MaxRetries = %v, want 10", custom.MaxRetries)
	}
}

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		name      string
		code      codes.Code
		retryable bool
	}{
		// Retryable codes
		{"Unavailable", codes.Unavailable, true},
		{"DeadlineExceeded", codes.DeadlineExceeded, true},
		{"Internal", codes.Internal, true},
		{"Aborted", codes.Aborted, true},
		// Non-retryable codes
		{"Unauthenticated", codes.Unauthenticated, false},
		{"InvalidArgument", codes.InvalidArgument, false},
		{"ResourceExhausted", codes.ResourceExhausted, false},
		{"PermissionDenied", codes.PermissionDenied, false},
		{"Unimplemented", codes.Unimplemented, false},
		{"NotFound", codes.NotFound, false},
		{"AlreadyExists", codes.AlreadyExists, false},
		{"FailedPrecondition", codes.FailedPrecondition, false},
		{"Canceled", codes.Canceled, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := status.Error(tt.code, "test error")
			got := isRetryable(err)
			if got != tt.retryable {
				t.Errorf("isRetryable(%v) = %v, want %v", tt.code, got, tt.retryable)
			}
		})
	}
}

func TestWithRetry_Success(t *testing.T) {
	client := &TergumClient{
		config: ClientConfig{
			InitialBackoff: 10 * time.Millisecond,
			MaxBackoff:     100 * time.Millisecond,
			BackoffFactor:  2.0,
			MaxRetries:     3,
		},
	}

	calls := 0
	err := client.withRetry(context.Background(), func() error {
		calls++
		return nil
	})

	if err != nil {
		t.Fatalf("withRetry returned error: %v", err)
	}
	if calls != 1 {
		t.Errorf("operation called %d times, want 1", calls)
	}
}

func TestWithRetry_RetriesOnTransientError(t *testing.T) {
	client := &TergumClient{
		config: ClientConfig{
			InitialBackoff: 10 * time.Millisecond,
			MaxBackoff:     100 * time.Millisecond,
			BackoffFactor:  2.0,
			MaxRetries:     3,
		},
	}

	calls := 0
	err := client.withRetry(context.Background(), func() error {
		calls++
		if calls < 3 {
			return status.Error(codes.Unavailable, "transient")
		}
		return nil
	})

	if err != nil {
		t.Fatalf("withRetry returned error: %v", err)
	}
	if calls != 3 {
		t.Errorf("operation called %d times, want 3", calls)
	}
}

func TestWithRetry_RespectsMaxRetries(t *testing.T) {
	client := &TergumClient{
		config: ClientConfig{
			InitialBackoff: 10 * time.Millisecond,
			MaxBackoff:     100 * time.Millisecond,
			BackoffFactor:  2.0,
			MaxRetries:     3,
		},
	}

	calls := 0
	err := client.withRetry(context.Background(), func() error {
		calls++
		return status.Error(codes.Unavailable, "always failing")
	})

	if err == nil {
		t.Fatal("withRetry should have returned an error")
	}
	// MaxRetries=3 means: initial attempt + 3 retries = 4 total calls
	if calls != 4 {
		t.Errorf("operation called %d times, want 4 (initial + 3 retries)", calls)
	}
}

func TestWithRetry_NonRetryableFailsImmediately(t *testing.T) {
	client := &TergumClient{
		config: ClientConfig{
			InitialBackoff: 10 * time.Millisecond,
			MaxBackoff:     100 * time.Millisecond,
			BackoffFactor:  2.0,
			MaxRetries:     5,
		},
	}

	nonRetryableCodes := []codes.Code{
		codes.Unauthenticated,
		codes.InvalidArgument,
		codes.ResourceExhausted,
		codes.PermissionDenied,
		codes.Unimplemented,
	}

	for _, code := range nonRetryableCodes {
		t.Run(code.String(), func(t *testing.T) {
			calls := 0
			err := client.withRetry(context.Background(), func() error {
				calls++
				return status.Error(code, "non-retryable")
			})

			if err == nil {
				t.Fatal("withRetry should have returned an error")
			}
			if calls != 1 {
				t.Errorf("operation called %d times for %v, want 1 (immediate failure)", calls, code)
			}
		})
	}
}

func TestWithRetry_BackoffIncreasesExponentially(t *testing.T) {
	client := &TergumClient{
		config: ClientConfig{
			InitialBackoff: 50 * time.Millisecond,
			MaxBackoff:     1 * time.Second,
			BackoffFactor:  2.0,
			MaxRetries:     3,
		},
	}

	var timestamps []time.Time
	err := client.withRetry(context.Background(), func() error {
		timestamps = append(timestamps, time.Now())
		return status.Error(codes.Unavailable, "transient")
	})

	if err == nil {
		t.Fatal("expected error")
	}

	// We should have 4 timestamps (initial + 3 retries)
	if len(timestamps) != 4 {
		t.Fatalf("got %d timestamps, want 4", len(timestamps))
	}

	// Verify that delays increase (with some tolerance for jitter and scheduling)
	// Expected: ~50ms, ~100ms, ~200ms (with ±25% jitter)
	for i := 1; i < len(timestamps); i++ {
		gap := timestamps[i].Sub(timestamps[i-1])
		if gap < 0 {
			t.Errorf("gap %d is negative: %v", i, gap)
		}
	}

	// First gap should be around 50ms (±25% jitter = 37.5ms to 62.5ms, plus scheduling)
	firstGap := timestamps[1].Sub(timestamps[0])
	if firstGap < 30*time.Millisecond {
		t.Errorf("first gap too short: %v (expected ~50ms with jitter)", firstGap)
	}

	// Second gap should be around 100ms, so should be larger than first
	secondGap := timestamps[2].Sub(timestamps[1])
	if secondGap < firstGap/2 {
		t.Errorf("backoff did not increase: first=%v, second=%v", firstGap, secondGap)
	}
}

func TestWithRetry_ContextCancellation(t *testing.T) {
	client := &TergumClient{
		config: ClientConfig{
			InitialBackoff: 1 * time.Second, // long backoff to ensure context cancellation fires first
			MaxBackoff:     30 * time.Second,
			BackoffFactor:  2.0,
			MaxRetries:     5,
		},
	}

	ctx, cancel := context.WithCancel(context.Background())

	calls := 0
	go func() {
		// Cancel context after a short delay
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := client.withRetry(ctx, func() error {
		calls++
		return status.Error(codes.Unavailable, "transient")
	})

	if err != context.Canceled {
		t.Errorf("withRetry returned %v, want context.Canceled", err)
	}
	// Should have been called at least once but not exhausted all retries
	if calls < 1 {
		t.Error("operation was never called")
	}
	if calls > 2 {
		t.Errorf("operation called %d times, expected <=2 due to context cancellation", calls)
	}
}

func TestWithRetry_BackoffCappedAtMax(t *testing.T) {
	client := &TergumClient{
		config: ClientConfig{
			InitialBackoff: 50 * time.Millisecond,
			MaxBackoff:     80 * time.Millisecond, // cap below what 2nd retry would be (100ms)
			BackoffFactor:  2.0,
			MaxRetries:     3,
		},
	}

	var timestamps []time.Time
	err := client.withRetry(context.Background(), func() error {
		timestamps = append(timestamps, time.Now())
		return status.Error(codes.Unavailable, "transient")
	})

	if err == nil {
		t.Fatal("expected error")
	}

	if len(timestamps) != 4 {
		t.Fatalf("got %d timestamps, want 4", len(timestamps))
	}

	// The third gap should not be significantly larger than the second
	// since backoff is capped at 80ms
	secondGap := timestamps[2].Sub(timestamps[1])
	thirdGap := timestamps[3].Sub(timestamps[2])

	// Third gap should be capped and not significantly larger than second gap
	// Both should be around 60-100ms (80ms cap with jitter)
	if thirdGap > secondGap*2 {
		t.Errorf("backoff not capped: second=%v, third=%v (max should be 80ms)", secondGap, thirdGap)
	}
}
