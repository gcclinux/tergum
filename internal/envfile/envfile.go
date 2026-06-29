// Package envfile provides a minimal .env file parser that loads KEY=VALUE
// pairs into the process environment.
package envfile

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Load reads a .env file from path and sets each KEY=VALUE pair into the
// process environment. Existing environment variables are NOT overwritten
// unless overwrite is true. Lines starting with # and empty lines are skipped.
// Values may optionally be quoted with single or double quotes.
func Load(path string, overwrite bool) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open env file: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments.
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, err := parseLine(line)
		if err != nil {
			return fmt.Errorf("line %d: %w", lineNum, err)
		}

		// Only set if not already defined (unless overwrite requested).
		if !overwrite {
			if _, exists := os.LookupEnv(key); exists {
				continue
			}
		}

		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("setting %s: %w", key, err)
		}
	}

	return scanner.Err()
}

// parseLine extracts KEY and VALUE from a line of the form KEY=VALUE.
func parseLine(line string) (string, string, error) {
	// Split on first '=' only.
	idx := strings.IndexByte(line, '=')
	if idx < 0 {
		return "", "", fmt.Errorf("invalid line (no '=' found): %s", line)
	}

	key := strings.TrimSpace(line[:idx])
	value := strings.TrimSpace(line[idx+1:])

	if key == "" {
		return "", "", fmt.Errorf("empty key")
	}

	// Strip surrounding quotes if present.
	value = unquote(value)

	return key, value, nil
}

// unquote removes matching surrounding single or double quotes from s.
func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
