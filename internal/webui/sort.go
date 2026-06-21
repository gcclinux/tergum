package webui

// SortDirection represents a table column sort direction.
type SortDirection string

const (
	SortAsc  SortDirection = "asc"
	SortDesc SortDirection = "desc"
)

// ToggleSortDirection returns the opposite sort direction.
// If current is "asc", it returns "desc".
// If current is "desc", it returns "asc".
// For any other value (including empty string), it defaults to "asc".
func ToggleSortDirection(current string) string {
	switch current {
	case "asc":
		return "desc"
	case "desc":
		return "asc"
	default:
		return "asc"
	}
}
