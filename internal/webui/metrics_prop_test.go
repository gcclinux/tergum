package webui

import (
	"testing"

	"pgregory.net/rapid"
)

// Feature: webui-redesign, Property 13: Storage threshold color scheme
// **Validates: Requirements 15.4, 15.5**

// TestProperty_StorageThresholdColorScheme verifies that for any storage usage
// percentage value in [0, 100], StorageColorScheme returns the correct color:
// "blue" when <80%, "amber" when 80-94%, "red" when ≥95%.
func TestProperty_StorageThresholdColorScheme(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate a random percentage in [0, 100].
		percent := rapid.Float64Range(0, 100).Draw(rt, "percent")

		result := StorageColorScheme(percent)

		switch {
		case percent < 80:
			if result != "blue" {
				rt.Fatalf("expected blue for percent=%.4f, got %q", percent, result)
			}
		case percent >= 80 && percent < 95:
			if result != "amber" {
				rt.Fatalf("expected amber for percent=%.4f, got %q", percent, result)
			}
		case percent >= 95:
			if result != "red" {
				rt.Fatalf("expected red for percent=%.4f, got %q", percent, result)
			}
		}
	})
}
