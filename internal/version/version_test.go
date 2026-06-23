package version

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVersionJSONSync(t *testing.T) {
	// Read root version.json
	rootBytes, err := os.ReadFile(filepath.Join("..", "..", "version.json"))
	if err != nil {
		t.Fatalf("failed to read root version.json: %v", err)
	}

	// Compare string content directly (ignoring whitespace and line endings)
	rootStr := string(rootBytes)
	localStr := string(versionJSON)

	normalize := func(s string) string {
		var res []rune
		for _, r := range s {
			if r != ' ' && r != '\t' && r != '\n' && r != '\r' {
				res = append(res, r)
			}
		}
		return string(res)
	}

	if normalize(rootStr) != normalize(localStr) {
		t.Errorf("root version.json does not match internal/version/version.json\nRoot:\n%s\nLocal:\n%s", rootStr, localStr)
	}
}
