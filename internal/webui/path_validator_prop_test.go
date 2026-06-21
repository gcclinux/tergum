package webui

import (
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// Feature: webui-redesign, Property 12: Path input whitespace validation
// **Validates: Requirements 14.4**

// TestProperty_WhitespaceOnlyPathsRejected verifies that for any string composed
// entirely of whitespace characters (including the empty string), ValidatePath
// returns ErrPathEmpty.
func TestProperty_WhitespaceOnlyPathsRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate a whitespace-only string: choose a length (0 means empty string),
		// then fill with random whitespace characters.
		whitespaceChars := []rune{' ', '\t', '\n', '\r'}
		length := rapid.IntRange(0, 50).Draw(rt, "length")

		var builder strings.Builder
		for i := 0; i < length; i++ {
			ch := rapid.SampledFrom(whitespaceChars).Draw(rt, "char")
			builder.WriteRune(ch)
		}
		path := builder.String()

		err := ValidatePath(path)
		if err == nil {
			rt.Fatalf("expected ErrPathEmpty for whitespace-only path %q (len=%d), got nil", path, length)
		}
		if err != ErrPathEmpty {
			rt.Fatalf("expected ErrPathEmpty for whitespace-only path %q, got: %v", path, err)
		}
	})
}

// TestProperty_NonWhitespacePathsAccepted verifies that for any string containing
// at least one non-whitespace character, ValidatePath returns nil (accepts the path).
func TestProperty_NonWhitespacePathsAccepted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate a string that contains at least one non-whitespace character.
		// Strategy: generate a non-empty visible string core, then optionally
		// surround it with whitespace padding.
		core := rapid.StringMatching(`[a-zA-Z0-9/._-]{1,50}`).Draw(rt, "core")
		prefixLen := rapid.IntRange(0, 5).Draw(rt, "prefixLen")
		suffixLen := rapid.IntRange(0, 5).Draw(rt, "suffixLen")

		prefix := strings.Repeat(" ", prefixLen)
		suffix := strings.Repeat(" ", suffixLen)
		path := prefix + core + suffix

		err := ValidatePath(path)
		if err != nil {
			rt.Fatalf("expected nil for path with non-whitespace content %q, got: %v", path, err)
		}
	})
}
