package webui

import (
	"testing"

	"pgregory.net/rapid"
)

// **Validates: Requirements 2.4, 12.5**

// TestProperty_SortDirectionCycling verifies that for any random number of
// sequential clicks on the same sortable column, the direction follows a
// deterministic alternating pattern: starting from "asc", each toggle switches
// between "asc" and "desc". After N toggles, even N yields "asc" and odd N
// yields "desc".
// Feature: webui-redesign, Property 14: Sortable column direction cycling
func TestProperty_SortDirectionCycling(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate a random number of clicks (1-20) simulating sequential
		// clicks on the same column.
		numClicks := rapid.IntRange(1, 20).Draw(rt, "numClicks")

		// A new column starts at "asc" (the first click sets direction to ascending).
		direction := "asc"

		// Apply ToggleSortDirection repeatedly, simulating sequential clicks.
		for i := 0; i < numClicks; i++ {
			direction = ToggleSortDirection(direction)
		}

		// After N toggles from "asc":
		// - Even number of toggles: direction should be "asc"
		// - Odd number of toggles: direction should be "desc"
		if numClicks%2 == 0 {
			if direction != "asc" {
				rt.Fatalf("after %d toggles from \"asc\", expected \"asc\" but got %q", numClicks, direction)
			}
		} else {
			if direction != "desc" {
				rt.Fatalf("after %d toggles from \"asc\", expected \"desc\" but got %q", numClicks, direction)
			}
		}
	})
}

// TestProperty_SortDirectionDoubleToggleIdentity verifies that for any starting
// direction value, applying ToggleSortDirection twice returns to the original
// direction. This ensures the toggle operation is its own inverse.
// Feature: webui-redesign, Property 14: Sortable column direction cycling
func TestProperty_SortDirectionDoubleToggleIdentity(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate a random starting direction from the valid set.
		startValues := []string{"asc", "desc"}
		idx := rapid.IntRange(0, len(startValues)-1).Draw(rt, "directionIndex")
		start := startValues[idx]

		// Double toggle should return to the original value.
		first := ToggleSortDirection(start)
		second := ToggleSortDirection(first)

		if second != start {
			rt.Fatalf("double toggle of %q: got %q after first toggle %q, expected %q", start, second, first, start)
		}
	})
}
