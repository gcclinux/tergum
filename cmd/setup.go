package cmd

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/ricardopadilha/tergum/internal/config"
	"github.com/ricardopadilha/tergum/internal/crypto"
	"github.com/ricardopadilha/tergum/internal/tls"
	"github.com/spf13/cobra"
)

func newSetupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Interactive first-time configuration wizard",
		Long: `Guides you through initial Tergum configuration including role selection,
storage path, certificate generation, and encryption passphrase setup.

Use --generate-certs to skip the interactive wizard and only generate TLS certificates
in the default certs directory.`,
		RunE: runSetup,
	}

	cmd.Flags().Bool("generate-certs", false, "generate TLS certificates without interactive prompts")

	return cmd
}

// setupWizard encapsulates state for the setup process, enabling testability
// by accepting an io.Reader for input.
type setupWizard struct {
	reader  io.Reader
	writer  io.Writer
	scanner *bufio.Scanner
}

func newSetupWizard(reader io.Reader, writer io.Writer) *setupWizard {
	return &setupWizard{
		reader:  reader,
		writer:  writer,
		scanner: bufio.NewScanner(reader),
	}
}

func (w *setupWizard) prompt(question, defaultVal string) string {
	if defaultVal != "" {
		fmt.Fprintf(w.writer, "%s [%s]: ", question, defaultVal)
	} else {
		fmt.Fprintf(w.writer, "%s: ", question)
	}
	if w.scanner.Scan() {
		answer := strings.TrimSpace(w.scanner.Text())
		if answer == "" {
			return defaultVal
		}
		return answer
	}
	return defaultVal
}

func (w *setupWizard) promptChoice(question string, options []string, defaultVal string) string {
	fmt.Fprintf(w.writer, "%s (%s) [%s]: ", question, strings.Join(options, "/"), defaultVal)
	if w.scanner.Scan() {
		answer := strings.TrimSpace(w.scanner.Text())
		if answer == "" {
			return defaultVal
		}
		// Validate choice
		for _, opt := range options {
			if strings.EqualFold(answer, opt) {
				return strings.ToLower(answer)
			}
		}
		// Invalid choice, use default
		fmt.Fprintf(w.writer, "Invalid choice %q, using default %q\n", answer, defaultVal)
		return defaultVal
	}
	return defaultVal
}

func (w *setupWizard) promptYesNo(question string, defaultYes bool) bool {
	def := "y/N"
	if defaultYes {
		def = "Y/n"
	}
	fmt.Fprintf(w.writer, "%s [%s]: ", question, def)
	if w.scanner.Scan() {
		answer := strings.TrimSpace(strings.ToLower(w.scanner.Text()))
		if answer == "" {
			return defaultYes
		}
		return answer == "y" || answer == "yes"
	}
	return defaultYes
}

func runSetup(cmd *cobra.Command, args []string) error {
	generateCerts, _ := cmd.Flags().GetBool("generate-certs")

	// Non-interactive mode: just generate certificates
	if generateCerts {
		return runGenerateCertsOnly()
	}

	// Interactive wizard
	wiz := newSetupWizard(os.Stdin, os.Stdout)
	return runInteractiveSetup(wiz)
}

// runGenerateCertsOnly generates TLS certificates in the default certs directory
// without prompting for any interactive input.
func runGenerateCertsOnly() error {
	configDir := config.DefaultConfigDir()
	certsDir := filepath.Join(configDir, "certs")

	mgr := tls.NewManager()
	if err := mgr.GenerateCerts(certsDir); err != nil {
		return fmt.Errorf("certificate generation failed: %w", err)
	}

	printOutput(
		map[string]interface{}{
			"status":    "success",
			"certs_dir": certsDir,
			"files":     []string{"ca.crt", "ca.key", "server.crt", "server.key", "client.crt", "client.key"},
		},
		fmt.Sprintf("TLS certificates generated in %s", certsDir),
	)
	return nil
}

// runInteractiveSetup guides the user through the full configuration wizard.
func runInteractiveSetup(wiz *setupWizard) error {
	fmt.Fprintln(wiz.writer, "Tergum Setup Wizard")
	fmt.Fprintln(wiz.writer, "===================")
	fmt.Fprintln(wiz.writer)

	// 1. Role selection
	role := wiz.promptChoice("Node role", []string{"client", "server", "both"}, "client")

	// 2. Server address (if client or both)
	var serverAddress string
	if role == "client" || role == "both" {
		serverAddress = wiz.prompt("Server address (hostname or IP)", "localhost")
	}

	// 3. Storage path
	defaultStorage := defaultStoragePath(role)
	storagePath := wiz.prompt("Storage path", defaultStorage)

	// Ensure storage path exists
	if err := os.MkdirAll(storagePath, 0700); err != nil {
		return fmt.Errorf("cannot create storage path %q: %w", storagePath, err)
	}

	// 4. Certificate generation
	configDir := config.DefaultConfigDir()
	certsDir := filepath.Join(configDir, "certs")
	generateCerts := wiz.promptYesNo("Generate TLS certificates?", true)

	if generateCerts {
		mgr := tls.NewManager()
		if err := mgr.GenerateCerts(certsDir); err != nil {
			return fmt.Errorf("certificate generation failed: %w", err)
		}
		fmt.Fprintf(wiz.writer, "Certificates generated in %s\n", certsDir)
	}

	// 5. Encryption passphrase
	passphrase := wiz.prompt("Encryption passphrase (min 8 characters)", "")
	if passphrase == "" {
		return fmt.Errorf("encryption passphrase is required")
	}
	if len(passphrase) < 8 {
		return fmt.Errorf("passphrase must be at least 8 characters")
	}

	// Derive master key from passphrase
	salt := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return fmt.Errorf("failed to generate salt: %w", err)
	}

	enc := crypto.NewEncryptor()
	masterKey, err := enc.DeriveKey(passphrase, salt)
	if err != nil {
		return fmt.Errorf("failed to derive master key: %w", err)
	}

	// Store salt and encrypted master key verification token in config dir
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return fmt.Errorf("cannot create config directory: %w", err)
	}

	saltPath := filepath.Join(configDir, "salt")
	if err := os.WriteFile(saltPath, []byte(hex.EncodeToString(salt)), 0600); err != nil {
		return fmt.Errorf("cannot write salt file: %w", err)
	}

	// Store a verification token (encrypt known plaintext to verify passphrase later)
	verificationPlaintext := []byte("tergum-key-verification")
	ciphertext, wrappedDEK, nonce, err := enc.Encrypt(verificationPlaintext, masterKey)
	if err != nil {
		return fmt.Errorf("failed to create verification token: %w", err)
	}
	verifyData := fmt.Sprintf("%s:%s:%s",
		hex.EncodeToString(ciphertext),
		hex.EncodeToString(wrappedDEK),
		hex.EncodeToString(nonce),
	)
	verifyPath := filepath.Join(configDir, "key_verify")
	if err := os.WriteFile(verifyPath, []byte(verifyData), 0600); err != nil {
		return fmt.Errorf("cannot write verification file: %w", err)
	}

	// 6. Write TOML configuration file
	cfg := buildConfig(role, serverAddress, storagePath, certsDir, configDir, generateCerts)
	configPath := config.DefaultConfigPath()

	if err := writeConfigTOML(configPath, cfg); err != nil {
		return fmt.Errorf("cannot write configuration file: %w", err)
	}

	fmt.Fprintln(wiz.writer)
	fmt.Fprintf(wiz.writer, "Configuration written to %s\n", configPath)
	fmt.Fprintln(wiz.writer, "Setup complete! You can now run 'tergum server' or 'tergum backup'.")

	printOutput(
		map[string]interface{}{
			"status":      "success",
			"config_path": configPath,
			"role":        role,
			"storage":     storagePath,
			"certs":       generateCerts,
		},
		"",
	)

	return nil
}

// defaultStoragePath returns a reasonable default storage path based on role and platform.
func defaultStoragePath(role string) string {
	if role == "server" || role == "both" {
		switch {
		case isWindows():
			return filepath.Join(os.Getenv("PROGRAMDATA"), "tergum", "storage")
		default:
			return "/var/lib/tergum/storage"
		}
	}
	// For client role, store in user config directory
	return filepath.Join(config.DefaultConfigDir(), "storage")
}

func isWindows() bool {
	return os.PathSeparator == '\\' && os.PathListSeparator == ';'
}

// buildConfig constructs a Config struct from wizard answers.
func buildConfig(role, serverAddress, storagePath, certsDir, configDir string, hasCerts bool) *config.Config {
	cfg := &config.Config{}

	// Node
	cfg.Node.Role = role
	cfg.Node.Hostname = ""

	// Server
	if serverAddress != "" {
		cfg.Server.Address = serverAddress
	}
	cfg.Server.CommandPort = 7400
	cfg.Server.DataPort = 7401

	// Client defaults
	cfg.Client.MaxFileSize = "10GB"

	// TLS
	if hasCerts {
		cfg.TLS.CACert = filepath.Join(certsDir, "ca.crt")
		if role == "server" || role == "both" {
			cfg.TLS.Cert = filepath.Join(certsDir, "server.crt")
			cfg.TLS.Key = filepath.Join(certsDir, "server.key")
		} else {
			cfg.TLS.Cert = filepath.Join(certsDir, "client.crt")
			cfg.TLS.Key = filepath.Join(certsDir, "client.key")
		}
	}

	// Encryption
	cfg.Encryption.Enabled = true

	// Database
	cfg.Database.Path = filepath.Join(configDir, "tergum.db")
	cfg.Database.WALMode = true

	// Backup
	cfg.Backup.ChunkSize = 65536
	cfg.Backup.MaxConcurrentUploads = 4
	cfg.Backup.MaxConcurrentDownloads = 8

	// Watcher
	cfg.Watcher.Enabled = true
	cfg.Watcher.DebounceMs = 500
	cfg.Watcher.StabilitySeconds = 60
	cfg.Watcher.OngoingBackup = true
	cfg.Watcher.BatchIntervalMinutes = 5

	// WebUI (server/both only)
	if role == "server" || role == "both" {
		cfg.WebUI.Enabled = true
	}
	cfg.WebUI.Port = 7480
	cfg.WebUI.SessionTimeoutHours = 24
	cfg.WebUI.Username = "admin"
	cfg.WebUI.Password = "admin"

	// Metrics
	cfg.Metrics.Enabled = true
	cfg.Metrics.Port = 7490

	// Logging
	cfg.Logging.Level = "info"
	cfg.Logging.Format = "text"

	return cfg
}

// writeConfigTOML writes a Config struct to a TOML file.
func writeConfigTOML(path string, cfg *config.Config) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("cannot create config directory: %w", err)
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("cannot open config file: %w", err)
	}
	defer f.Close()

	encoder := toml.NewEncoder(f)
	return encoder.Encode(cfg)
}
