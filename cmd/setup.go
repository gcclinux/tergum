package cmd

import (
	"bufio"
	"context"
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
	"github.com/ricardopadilha/tergum/internal/db"
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
	saltPath := filepath.Join(configDir, "salt")
	verifyPath := filepath.Join(configDir, "key_verify")
	existingConfigExists := false

	if _, err := os.Stat(saltPath); err == nil {
		if _, err := os.Stat(verifyPath); err == nil {
			existingConfigExists = true
		}
	}

	var salt []byte
	var masterKey []byte
	var err error
	enc := crypto.NewEncryptor()

	if existingConfigExists {
		fmt.Fprintln(wiz.writer, "\nAn existing encryption configuration was found.")
		fmt.Fprintln(wiz.writer, "WARNING: Overwriting it will make all past backups permanently undecryptable!")
		
		overwrite := wiz.promptYesNo("Do you want to overwrite the existing encryption configuration and start fresh?", false)
		if !overwrite {
			// Read existing salt
			saltHex, err := os.ReadFile(saltPath)
			if err != nil {
				return fmt.Errorf("cannot read existing salt file: %w", err)
			}
			salt, err = hex.DecodeString(strings.TrimSpace(string(saltHex)))
			if err != nil {
				return fmt.Errorf("invalid existing salt file: %w", err)
			}

			// Read existing verify token
			verifyData, err := os.ReadFile(verifyPath)
			if err != nil {
				return fmt.Errorf("cannot read existing verification file: %w", err)
			}

			// Prompt for existing passphrase to verify
			for {
				passphrase := wiz.prompt("Enter existing encryption passphrase to verify", "")
				if passphrase == "" {
					return fmt.Errorf("encryption passphrase is required")
				}
				
				masterKey, err = enc.DeriveKey(passphrase, salt)
				if err != nil {
					return fmt.Errorf("failed to derive master key: %w", err)
				}

				if ok, _ := enc.VerifyMasterKey(masterKey, string(verifyData)); ok {
					fmt.Fprintln(wiz.writer, "Passphrase verified successfully. Existing encryption configuration kept.")
					break
				}
				
				fmt.Fprintln(wiz.writer, "Incorrect passphrase for the existing configuration.")
				retry := wiz.promptYesNo("Do you want to try again?", true)
				if !retry {
					return fmt.Errorf("setup cancelled: incorrect passphrase")
				}
			}
		} else {
			existingConfigExists = false
		}
	}

	if !existingConfigExists {
		passphrase := wiz.prompt("Encryption passphrase (min 8 characters)", "")
		if passphrase == "" {
			return fmt.Errorf("encryption passphrase is required")
		}
		if len(passphrase) < 8 {
			return fmt.Errorf("passphrase must be at least 8 characters")
		}

		// Derive master key from passphrase
		salt = make([]byte, 16)
		if _, err := io.ReadFull(rand.Reader, salt); err != nil {
			return fmt.Errorf("failed to generate salt: %w", err)
		}

		masterKey, err = enc.DeriveKey(passphrase, salt)
		if err != nil {
			return fmt.Errorf("failed to derive master key: %w", err)
		}

		// Store salt and encrypted master key verification token in config dir
		if err := os.MkdirAll(configDir, 0700); err != nil {
			return fmt.Errorf("cannot create config directory: %w", err)
		}

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
		if err := os.WriteFile(verifyPath, []byte(verifyData), 0600); err != nil {
			return fmt.Errorf("cannot write verification file: %w", err)
		}
	}

	// 6. Backup paths — what to back up
	fmt.Fprintln(wiz.writer)
	fmt.Fprintln(wiz.writer, "--- Backup Paths ---")
	fmt.Fprintln(wiz.writer, "Which directories should be backed up?")
	fmt.Fprintln(wiz.writer, "Enter full paths, one per line. Leave empty when done.")

	var includePaths []string
	for {
		p := wiz.prompt("Include path (empty to finish)", "")
		if p == "" {
			break
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			fmt.Fprintf(wiz.writer, "  Invalid path: %v\n", err)
			continue
		}
		// Verify directory exists
		info, err := os.Stat(abs)
		if err != nil {
			fmt.Fprintf(wiz.writer, "  Warning: path does not exist: %s (adding anyway)\n", abs)
		} else if !info.IsDir() {
			fmt.Fprintf(wiz.writer, "  Warning: %s is a file, not a directory (adding anyway)\n", abs)
		}
		includePaths = append(includePaths, abs)
		fmt.Fprintf(wiz.writer, "  Added: %s\n", abs)
	}

	if len(includePaths) == 0 {
		// Offer to scan home directory
		if wiz.promptYesNo("No paths added. Scan home directory for top-level folders?", true) {
			home, err := os.UserHomeDir()
			if err == nil {
				entries, err := os.ReadDir(home)
				if err == nil {
					for _, entry := range entries {
						if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
							includePaths = append(includePaths, filepath.Join(home, entry.Name()))
						}
					}
					fmt.Fprintf(wiz.writer, "  Found %d directories in %s\n", len(includePaths), home)
					for _, p := range includePaths {
						fmt.Fprintf(wiz.writer, "    + %s\n", p)
					}
				}
			}
		}
	}

	// 7. Exclude patterns
	fmt.Fprintln(wiz.writer)
	fmt.Fprintln(wiz.writer, "--- Exclude Patterns ---")
	fmt.Fprintln(wiz.writer, "Glob patterns for files/directories to skip.")
	fmt.Fprintln(wiz.writer, "Common: *.tmp, node_modules/, .git/, __pycache__/, *.log")

	useDefaults := wiz.promptYesNo("Use default exclude patterns? (build artifacts, caches, VCS, logs)", true)
	var excludePatterns []string
	if useDefaults {
		excludePatterns = []string{
			"*.tmp",
			"*.log",
			"*.o",
			"*.class",
			".git/",
			".cache/",
			".nuget/",
			".npm/",
			".gradle/",
			"node_modules/",
			"__pycache__/",
			"bin/Debug/",
			"bin/Release/",
			"obj/",
			"target/",
			"dist/",
		}
		fmt.Fprintln(wiz.writer, "  Default patterns added.")
	}

	fmt.Fprintln(wiz.writer, "Add additional exclude patterns (empty to finish):")
	for {
		p := wiz.prompt("Exclude pattern (empty to finish)", "")
		if p == "" {
			break
		}
		excludePatterns = append(excludePatterns, p)
		fmt.Fprintf(wiz.writer, "  Added: %s\n", p)
	}

	// 8. Watcher configuration
	fmt.Fprintln(wiz.writer)
	fmt.Fprintln(wiz.writer, "--- File Watcher ---")
	watcherEnabled := wiz.promptYesNo("Enable file watcher for ongoing backups?", true)

	// 9. Scheduler
	fmt.Fprintln(wiz.writer)
	fmt.Fprintln(wiz.writer, "--- Backup Schedule ---")
	fmt.Fprintln(wiz.writer, "You can configure scheduled backups using cron expressions.")
	fmt.Fprintln(wiz.writer, "Examples: '0 2 * * *' (daily at 2 AM), '0 */6 * * *' (every 6 hours)")
	fullBackupCron := wiz.prompt("Full backup cron schedule (empty for manual only)", "")
	autoBackupCron := wiz.prompt("Auto/incremental backup cron (empty for none)", "")

	// 10. Write TOML configuration file
	cfg := buildConfig(role, serverAddress, storagePath, certsDir, configDir, generateCerts)
	cfg.Watcher.Enabled = watcherEnabled
	cfg.Scheduler.FullBackupCron = fullBackupCron
	cfg.Scheduler.AutoBackupCron = autoBackupCron
	cfg.Client.IncludePaths = includePaths
	cfg.Client.ExcludePatterns = excludePatterns

	configPath := config.DefaultConfigPath()

	if err := writeConfigTOML(configPath, cfg); err != nil {
		return fmt.Errorf("cannot write configuration file: %w", err)
	}

	// 11. Save include paths and exclude patterns to the database
	dbPath := cfg.Database.Path
	if err := os.MkdirAll(filepath.Dir(dbPath), 0700); err != nil {
		return fmt.Errorf("cannot create database directory: %w", err)
	}

	repo, err := db.NewRepository(dbPath, true)
	if err != nil {
		fmt.Fprintf(wiz.writer, "Warning: could not open database to save paths: %v\n", err)
	} else {
		defer repo.Close()
		ctx := context.Background()

		// Clear existing paths so setup is authoritative.
		existingIncludes, _ := repo.ListIncludePaths(ctx)
		for _, p := range existingIncludes {
			_ = repo.RemoveIncludePath(ctx, p)
		}
		existingExcludes, _ := repo.ListExcludePatterns(ctx)
		for _, p := range existingExcludes {
			_ = repo.RemoveExcludePattern(ctx, p)
		}

		for _, p := range includePaths {
			_ = repo.AddIncludePath(ctx, p)
		}
		for _, p := range excludePatterns {
			_ = repo.AddExcludePattern(ctx, p)
		}
	}

	fmt.Fprintln(wiz.writer)
	fmt.Fprintf(wiz.writer, "Configuration written to %s\n", configPath)
	fmt.Fprintln(wiz.writer)
	fmt.Fprintln(wiz.writer, "Setup complete! Next steps:")
	fmt.Fprintln(wiz.writer, "  tergum server    — start the server (required for backups)")
	fmt.Fprintln(wiz.writer, "  tergum backup    — run a manual backup")
	fmt.Fprintln(wiz.writer, "  tergum paths list — view configured paths")

	printOutput(
		map[string]interface{}{
			"status":           "success",
			"config_path":      configPath,
			"role":             role,
			"storage":          storagePath,
			"certs":            generateCerts,
			"include_paths":    includePaths,
			"exclude_patterns": excludePatterns,
			"watcher_enabled":  watcherEnabled,
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
	cfg.Backup.StoragePath = storagePath
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
