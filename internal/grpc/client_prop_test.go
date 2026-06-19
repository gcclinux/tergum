package grpc

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"pgregory.net/rapid"
)

// **Validates: Requirements 22.1**

// computeBackoffSequence computes the sequence of backoff durations that withRetry
// would use for each retry attempt. This extracts the deterministic backoff progression
// logic (without jitter) to enable property-based testing of the algorithm.
func computeBackoffSequence(cfg ClientConfig) []time.Duration {
	cfg = cfg.applyDefaults()
	seq := make([]time.Duration, 0, cfg.MaxRetries)
	backoff := cfg.InitialBackoff
	for i := 0; i < cfg.MaxRetries; i++ {
		seq = append(seq, backoff)
		backoff = time.Duration(float64(backoff) * cfg.BackoffFactor)
		if backoff > cfg.MaxBackoff {
			backoff = cfg.MaxBackoff
		}
	}
	return seq
}

// genClientConfig generates a random ClientConfig with reasonable bounds for property testing.
func genClientConfig(t *rapid.T) ClientConfig {
	initialMs := rapid.IntRange(10, 500).Draw(t, "initialBackoffMs")
	maxMs := rapid.IntRange(1000, 30000).Draw(t, "maxBackoffMs")
	factor := rapid.Float64Range(1.5, 4.0).Draw(t, "backoffFactor")
	retries := rapid.IntRange(1, 10).Draw(t, "maxRetries")

	return ClientConfig{
		InitialBackoff: time.Duration(initialMs) * time.Millisecond,
		MaxBackoff:     time.Duration(maxMs) * time.Millisecond,
		BackoffFactor:  factor,
		MaxRetries:     retries,
	}
}

func TestProperty_BackoffSequenceLength(t *testing.T) {
	// Property: The backoff sequence has exactly MaxRetries elements.
	rapid.Check(t, func(t *rapid.T) {
		cfg := genClientConfig(t)
		seq := computeBackoffSequence(cfg)
		if len(seq) != cfg.MaxRetries {
			t.Fatalf("backoff sequence length = %d, want %d (MaxRetries)", len(seq), cfg.MaxRetries)
		}
	})
}

func TestProperty_BackoffSequenceFirstElement(t *testing.T) {
	// Property: The first backoff value equals InitialBackoff.
	rapid.Check(t, func(t *rapid.T) {
		cfg := genClientConfig(t)
		seq := computeBackoffSequence(cfg)
		if len(seq) == 0 {
			t.Fatal("empty backoff sequence")
		}
		if seq[0] != cfg.InitialBackoff {
			t.Fatalf("first backoff = %v, want %v (InitialBackoff)", seq[0], cfg.InitialBackoff)
		}
	})
}

func TestProperty_BackoffSequenceMonotonicallyIncreasing(t *testing.T) {
	// Property: Each successive backoff >= previous * backoffFactor (within 1% tolerance
	// for floating-point math), until capped at MaxBackoff.
	rapid.Check(t, func(t *rapid.T) {
		cfg := genClientConfig(t)
		seq := computeBackoffSequence(cfg)
		for i := 1; i < len(seq); i++ {
			expectedMin := time.Duration(float64(seq[i-1]) * cfg.BackoffFactor * 0.99)
			if expectedMin > cfg.MaxBackoff {
				expectedMin = cfg.MaxBackoff
			}
			// Once capped, all subsequent values should be at MaxBackoff
			if seq[i-1] >= cfg.MaxBackoff {
				if seq[i] != cfg.MaxBackoff {
					t.Fatalf("backoff[%d] = %v, want %v (should be capped at MaxBackoff)", i, seq[i], cfg.MaxBackoff)
				}
			} else {
				// seq[i] should be at least previous * factor (within tolerance), or capped at MaxBackoff
				if seq[i] < expectedMin && seq[i] != cfg.MaxBackoff {
					t.Fatalf("backoff[%d] = %v < expected minimum %v (previous=%v * factor=%v * 0.99)",
						i, seq[i], expectedMin, seq[i-1], cfg.BackoffFactor)
				}
			}
		}
	})
}

func TestProperty_BackoffSequenceNeverExceedsMax(t *testing.T) {
	// Property: No backoff value exceeds MaxBackoff.
	rapid.Check(t, func(t *rapid.T) {
		cfg := genClientConfig(t)
		seq := computeBackoffSequence(cfg)
		for i, b := range seq {
			if b > cfg.MaxBackoff {
				t.Fatalf("backoff[%d] = %v exceeds MaxBackoff %v", i, b, cfg.MaxBackoff)
			}
		}
	})
}

func TestProperty_NonRetryableErrorNoRetries(t *testing.T) {
	// Property: For any ClientConfig, a non-retryable error always results in exactly 1 call.
	nonRetryableCodes := []codes.Code{
		codes.Unauthenticated,
		codes.InvalidArgument,
		codes.ResourceExhausted,
		codes.PermissionDenied,
		codes.Unimplemented,
		codes.NotFound,
		codes.AlreadyExists,
		codes.FailedPrecondition,
		codes.Canceled,
	}

	rapid.Check(t, func(t *rapid.T) {
		cfg := genClientConfig(t)
		// Use minimal backoff to keep tests fast
		cfg.InitialBackoff = 1 * time.Millisecond
		cfg.MaxBackoff = 10 * time.Millisecond

		codeIdx := rapid.IntRange(0, len(nonRetryableCodes)-1).Draw(t, "codeIdx")
		code := nonRetryableCodes[codeIdx]

		client := &TergumClient{config: cfg.applyDefaults()}
		calls := 0
		err := client.withRetry(context.Background(), func() error {
			calls++
			return status.Error(code, "non-retryable error")
		})

		if err == nil {
			t.Fatal("expected error for non-retryable code")
		}
		if calls != 1 {
			t.Fatalf("non-retryable error with code %v: called %d times, want exactly 1", code, calls)
		}
	})
}

func TestProperty_RetryableErrorExhaustsRetries(t *testing.T) {
	// Property: For any ClientConfig with MaxRetries=N and always-failing retryable errors,
	// the operation is called exactly N+1 times (1 initial + N retries).
	retryableCodes := []codes.Code{
		codes.Unavailable,
		codes.DeadlineExceeded,
		codes.Internal,
		codes.Aborted,
	}

	rapid.Check(t, func(t *rapid.T) {
		cfg := genClientConfig(t)
		// Use minimal backoff to keep tests fast
		cfg.InitialBackoff = 1 * time.Millisecond
		cfg.MaxBackoff = 10 * time.Millisecond

		codeIdx := rapid.IntRange(0, len(retryableCodes)-1).Draw(t, "codeIdx")
		code := retryableCodes[codeIdx]

		client := &TergumClient{config: cfg.applyDefaults()}
		calls := 0
		err := client.withRetry(context.Background(), func() error {
			calls++
			return status.Error(code, "always failing retryable error")
		})

		if err == nil {
			t.Fatal("expected error after exhausting retries")
		}
		expectedCalls := cfg.MaxRetries + 1
		if calls != expectedCalls {
			t.Fatalf("retryable error with code %v: called %d times, want %d (MaxRetries=%d + 1)",
				code, calls, expectedCalls, cfg.MaxRetries)
		}
	})
}
