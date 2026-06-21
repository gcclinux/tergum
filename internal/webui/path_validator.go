package webui

import (
	"errors"
	"strings"
)

// ErrPathEmpty is returned when a path is empty or contains only whitespace.
var ErrPathEmpty = errors.New("path must not be blank")

// ValidatePath checks that the given path is not empty and not composed entirely
// of whitespace characters. It returns ErrPathEmpty if the path is invalid.
func ValidatePath(path string) error {
	if strings.TrimSpace(path) == "" {
		return ErrPathEmpty
	}
	return nil
}
