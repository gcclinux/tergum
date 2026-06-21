package webui

import "testing"

func TestToggleSortDirection_AscToDesc(t *testing.T) {
	got := ToggleSortDirection("asc")
	if got != "desc" {
		t.Errorf("ToggleSortDirection(\"asc\") = %q, want \"desc\"", got)
	}
}

func TestToggleSortDirection_DescToAsc(t *testing.T) {
	got := ToggleSortDirection("desc")
	if got != "asc" {
		t.Errorf("ToggleSortDirection(\"desc\") = %q, want \"asc\"", got)
	}
}

func TestToggleSortDirection_EmptyDefaultsToAsc(t *testing.T) {
	got := ToggleSortDirection("")
	if got != "asc" {
		t.Errorf("ToggleSortDirection(\"\") = %q, want \"asc\"", got)
	}
}

func TestToggleSortDirection_InvalidDefaultsToAsc(t *testing.T) {
	cases := []string{"ASC", "DESC", "up", "down", "random", " asc", "desc "}
	for _, input := range cases {
		got := ToggleSortDirection(input)
		if got != "asc" {
			t.Errorf("ToggleSortDirection(%q) = %q, want \"asc\"", input, got)
		}
	}
}

func TestToggleSortDirection_DoubleToggleReturnsOriginal(t *testing.T) {
	first := ToggleSortDirection("asc")
	second := ToggleSortDirection(first)
	if second != "asc" {
		t.Errorf("double toggle from \"asc\" = %q, want \"asc\"", second)
	}

	first = ToggleSortDirection("desc")
	second = ToggleSortDirection(first)
	if second != "desc" {
		t.Errorf("double toggle from \"desc\" = %q, want \"desc\"", second)
	}
}

func TestSortDirectionConstants(t *testing.T) {
	if SortAsc != "asc" {
		t.Errorf("SortAsc = %q, want \"asc\"", SortAsc)
	}
	if SortDesc != "desc" {
		t.Errorf("SortDesc = %q, want \"desc\"", SortDesc)
	}
}
