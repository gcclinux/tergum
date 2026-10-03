package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gcclinux/tergum/internal/config"
)

// envFilePath returns the platform-specific path to the .env file.
// Uses config.DefaultConfigDir() to ensure consistency with other config files.
func envFilePath() string {
	return filepath.Join(config.DefaultConfigDir(), ".env")
}

// envFileExists returns true if the .env file exists at the platform path.
func envFileExists() bool {
	_, err := os.Stat(envFilePath())
	return err == nil
}

// writeEnvFile writes the passphrase to the .env file.
// Creates parent directories if needed. Sets permissions to 0600 on Unix.
// Returns the absolute path written to.
func writeEnvFile(passphrase string) (string, error) {
	path := envFilePath()
	dir := filepath.Dir(path)

	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("creating config directory: %w", err)
	}

	content := fmt.Sprintf("TERGUM_PASSPHRASE=%s\n", passphrase)

	// Use 0600 for owner-read-write only on all platforms.
	// On Windows, Go's os.WriteFile applies the closest equivalent.
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		return "", fmt.Errorf("writing env file: %w", err)
	}

	return path, nil
}

// readEnvFilePassphrase reads TERGUM_PASSPHRASE from the .env file.
// Returns empty string if file doesn't exist or doesn't contain the key.
func readEnvFilePassphrase() (string, error) {
	path := envFilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "TERGUM_PASSPHRASE=") {
			return strings.TrimPrefix(line, "TERGUM_PASSPHRASE="), nil
		}
	}

	return "", fmt.Errorf("TERGUM_PASSPHRASE not found in env file")
}
