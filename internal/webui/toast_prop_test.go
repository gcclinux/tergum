package webui

import (
	"testing"

	"pgregory.net/rapid"
)

// **Validates: Requirements 7.3**

// TestProperty_ToastMessageTruncation verifies that for any message string,
// the truncated output never exceeds 200 characters, and messages at or below
// 200 characters are returned unchanged.
// Feature: webui-redesign, Property 16: Toast message truncation
func TestProperty_ToastMessageTruncation(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate a random string length between 0 and 500.
		length := rapid.IntRange(0, 500).Draw(rt, "length")

		// Build a random message of the desired length using printable ASCII.
		var msg string
		if length > 0 {
			chars := rapid.SliceOfN(rapid.ByteRange(32, 126), length, length).Draw(rt, "chars")
			msg = string(chars)
		}

		result := TruncateToastMessage(msg)

		// Property: output never exceeds 200 characters.
		if len(result) > 200 {
			rt.Fatalf("truncated message length %d exceeds 200: %q", len(result), result)
		}

		// Property: strings ≤200 chars are returned unchanged.
		if len(msg) <= 200 {
			if result != msg {
				rt.Fatalf("message of length %d was modified: got %q, want %q", len(msg), result, msg)
			}
		}

		// Property: strings >200 chars are truncated to exactly 200.
		if len(msg) > 200 {
			if len(result) != 200 {
				rt.Fatalf("expected truncated length 200, got %d", len(result))
			}
			// Verify the truncation preserves the prefix.
			if result != msg[:200] {
				rt.Fatalf("truncated message is not a prefix of original")
			}
		}
	})
}
