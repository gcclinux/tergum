package webui

import (
	"testing"

	"pgregory.net/rapid"
)

// **Validates: Requirements 4.9, 4.10**

// TestProperty_PollingFailureCounterAndStaleness verifies that for any random sequence
// of success/failure polling results, the consecutive failure counter increments by 1
// on each failure, resets to 0 on success, and the staleness indicator becomes visible
// at exactly 3 consecutive failures and hidden on the next success.
// Feature: webui-redesign, Property 7: Polling failure counter and staleness indicator
func TestProperty_PollingFailureCounterAndStaleness(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate a random sequence length between 1 and 50.
		seqLen := rapid.IntRange(1, 50).Draw(rt, "sequenceLength")

		// Apply actions to a fresh PollingStalenessState.
		state := PollingStalenessState{}
		previousFailures := 0

		for i := 0; i < seqLen; i++ {
			// Generate a random action: true = failure, false = success.
			isFailure := rapid.Bool().Draw(rt, "isFailure")

			if isFailure {
				state.RecordFailure()
				expectedFailures := previousFailures + 1

				// Verify counter increments by 1 on failure.
				if state.Failures() != expectedFailures {
					rt.Fatalf("step %d (failure): expected failures=%d, got %d",
						i, expectedFailures, state.Failures())
				}

				// Verify staleness indicator: true iff counter >= 3.
				expectedStale := expectedFailures >= 3
				if state.IsStale() != expectedStale {
					rt.Fatalf("step %d (failure): expected IsStale()=%v with failures=%d, got %v",
						i, expectedStale, expectedFailures, state.IsStale())
				}

				previousFailures = expectedFailures
			} else {
				state.RecordSuccess()

				// Verify counter resets to 0 on success.
				if state.Failures() != 0 {
					rt.Fatalf("step %d (success): expected failures=0, got %d",
						i, state.Failures())
				}

				// Verify staleness clears on success.
				if state.IsStale() {
					rt.Fatalf("step %d (success): expected IsStale()=false, got true", i)
				}

				previousFailures = 0
			}
		}
	})
}
