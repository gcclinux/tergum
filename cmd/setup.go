package cmd

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/gcclinux/tergum/internal/config"
	"github.com/gcclinux/tergum/internal/crypto"
	"github.com/gcclinux/tergum/internal/db"
	"github.com/gcclinux/tergum/internal/grpc/proto"
	tlspkg "github.com/gcclinux/tergum/internal/tls"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
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

	var hosts []string
	if ips, err := getLocalIPs(); err == nil {
		hosts = append(hosts, ips...)
	}
	if h, err := os.Hostname(); err == nil && h != "" {
		hosts = append(hosts, h)
	}

	mgr := tlspkg.NewManager()
	if err := mgr.GenerateCerts(certsDir, hosts...); err != nil {
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
	role := wiz.promptChoice("Node role", []string{"client", "server", "hybrid"}, "client")

	// 2. Client IP/Hostname (if role is client)
	var clientHostname string
	if role == "client" {
		ips, err := getLocalIPs()
		defaultHost, _ := os.Hostname()
		if defaultHost == "" {
			defaultHost = "localhost"
		}
		if err == nil && len(ips) > 0 {
			fmt.Fprintln(wiz.writer, "\nLocal IP addresses found:")
			for i, ip := range ips {
				fmt.Fprintf(wiz.writer, "  [%d] %s\n", i+1, ip)
			}
			fmt.Fprintf(wiz.writer, "  [%d] Use hostname: %s\n", len(ips)+1, defaultHost)

			choiceStr := wiz.prompt("Select the client IP address/hostname to advertise to the server", "1")
			choice, err := strconv.Atoi(choiceStr)
			if err == nil && choice >= 1 && choice <= len(ips) {
				clientHostname = ips[choice-1]
			} else if err == nil && choice == len(ips)+1 {
				clientHostname = defaultHost
			} else {
				clientHostname = choiceStr
			}
		} else {
			clientHostname = wiz.prompt("Client IP address or hostname to advertise to the server", defaultHost)
		}
	}

	// 2b. Server address (if client or hybrid)
	var serverAddress string
	if role == "client" || role == "hybrid" {
		if role == "client" {
			for {
				serverAddress = wiz.prompt("Server address (hostname or IP)", "")
				if serverAddress == "" {
					fmt.Fprintln(wiz.writer, "Error: Server address is required for client setup.")
					continue
				}
				addrLower := strings.ToLower(strings.TrimSpace(serverAddress))
				if addrLower == "localhost" || addrLower == "127.0.0.1" || addrLower == "::1" {
					fmt.Fprintln(wiz.writer, "Error: Server address cannot be localhost for client setup. Please specify the server's actual IP address or hostname.")
					continue
				}
				break
			}
		} else {
			serverAddress = wiz.prompt("Server address (hostname or IP)", "localhost")
		}
	}

	// 3. Storage path
	var storagePath string
	if role == "server" || role == "hybrid" {
		defaultStorage := defaultStoragePath(role)
		storagePath = wiz.prompt("Storage path", defaultStorage)

		// Ensure storage path exists
		if err := os.MkdirAll(storagePath, 0700); err != nil {
			return fmt.Errorf("cannot create storage path %q: %w", storagePath, err)
		}
	}

	// 4. Certificate generation
	configDir := config.DefaultConfigDir()
	certsDir := filepath.Join(configDir, "certs")
	generateCerts := wiz.promptYesNo("Configure TLS certificates?", true)

	if generateCerts {
		if role == "client" && serverAddress != "" {
			// For client nodes: the ONLY valid certs are ones signed by the server's CA.
			// There is no useful fallback — locally-generated certs will be rejected by the server.
			fetched, fetchErr := fetchCertsFromServer(wiz, serverAddress, 7402, certsDir)
			if fetchErr != nil {
				fmt.Fprintln(wiz.writer)
				fmt.Fprintf(wiz.writer, "ERROR: Could not fetch certificates from server: %v\n", fetchErr)
				printManualCertInstructions(wiz.writer, serverAddress, certsDir)
				return fmt.Errorf("certificate bootstrap failed: manually copy server certs to proceed")
			}
			if !fetched {
				// User declined the fingerprint confirmation.
				fmt.Fprintln(wiz.writer)
				fmt.Fprintln(wiz.writer, "Certificate import cancelled.")
				printManualCertInstructions(wiz.writer, serverAddress, certsDir)
				return fmt.Errorf("certificate bootstrap cancelled: manually copy server certs to proceed")
			}
			fmt.Fprintf(wiz.writer, "Certificates imported from server into %s\n", certsDir)
		} else {
			// Server/hybrid role: generate a full local CA + certs.
			var hosts []string
			if ips, err := getLocalIPs(); err == nil {
				hosts = append(hosts, ips...)
			}
			if h, err := os.Hostname(); err == nil && h != "" {
				hosts = append(hosts, h)
			}

			mgr := tlspkg.NewManager()
			if err := mgr.GenerateCerts(certsDir, hosts...); err != nil {
				return fmt.Errorf("certificate generation failed: %w", err)
			}
			fmt.Fprintf(wiz.writer, "Certificates generated in %s\n", certsDir)
		}
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
			"**/go/bin/",
			"**/go/pkg/",
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
	cfg := buildConfig(role, serverAddress, clientHostname, storagePath, certsDir, configDir, generateCerts)
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
		if len(salt) > 0 {
			_ = repo.SetConfig(ctx, "encryption_salt", hex.EncodeToString(salt))
		}
	}

	fmt.Fprintln(wiz.writer)
	fmt.Fprintf(wiz.writer, "Configuration written to %s\n", configPath)
	fmt.Fprintln(wiz.writer)
	fmt.Fprintln(wiz.writer, "Setup complete! Next steps:")
	if role == "client" {
		fmt.Fprintln(wiz.writer, "  tergum client    — start the client daemon (required for backups)")
	} else {
		fmt.Fprintln(wiz.writer, "  tergum server    — start the server (required for backups)")
	}
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
	if role == "server" || role == "hybrid" {
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
func buildConfig(role, serverAddress, clientHostname, storagePath, certsDir, configDir string, hasCerts bool) *config.Config {
	cfg := &config.Config{}

	// Node
	cfg.Node.Role = role
	cfg.Node.Hostname = clientHostname

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
		if role == "server" || role == "hybrid" {
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

	// WebUI (server/hybrid only)
	if role == "server" || role == "hybrid" {
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

// getLocalIPs returns all non-loopback IPv4 addresses found on local network interfaces.
var getLocalIPs = func() ([]string, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}
	var ips []string
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				ips = append(ips, ipnet.IP.String())
			}
		}
	}
	return ips, nil
}

// printManualCertInstructions prints the steps the user must follow to manually
// copy TLS certificates from the server when automatic bootstrap is unavailable.
func printManualCertInstructions(w io.Writer, serverAddress, certsDir string) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, "┌─────────────────────────────────────────────────────────────────┐")
	fmt.Fprintln(w, "│           MANUAL CERTIFICATE SETUP REQUIRED                     │")
	fmt.Fprintln(w, "└─────────────────────────────────────────────────────────────────┘")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "A Tergum client MUST use certificates signed by the server's CA.")
	fmt.Fprintln(w, "Locally-generated certificates will be rejected by the server.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "On your SERVER machine, find the certificate files:")
	fmt.Fprintln(w, "  Linux/macOS : ~/.config/tergum/certs/")
	fmt.Fprintln(w, "  Windows     : %APPDATA%\\tergum\\certs\\")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Copy these three files to THIS machine:")
	fmt.Fprintf(w, "  ca.crt      → %s/ca.crt\n", certsDir)
	fmt.Fprintf(w, "  client.crt  → %s/client.crt\n", certsDir)
	fmt.Fprintf(w, "  client.key  → %s/client.key\n", certsDir)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Example using scp (run on this machine):")
	fmt.Fprintf(w, "  scp user@%s:~/.config/tergum/certs/ca.crt     %s/ca.crt\n", serverAddress, certsDir)
	fmt.Fprintf(w, "  scp user@%s:~/.config/tergum/certs/client.crt %s/client.crt\n", serverAddress, certsDir)
	fmt.Fprintf(w, "  scp user@%s:~/.config/tergum/certs/client.key %s/client.key\n", serverAddress, certsDir)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Once copied, re-run: tergum setup")
	fmt.Fprintln(w)
}

// fetchCertsFromServer connects to the server's BootstrapService, fetches the CA
// certificate and a freshly-issued client certificate, shows the CA fingerprint to
// the user for verification, and writes the cert files to certsDir on confirmation.
//
// Returns (true, nil) on success, (false, nil) if the user declined the fingerprint,
// or (false, err) if the connection or cert issuance failed.
func fetchCertsFromServer(wiz *setupWizard, serverAddr string, bootstrapPort int, certsDir string) (bool, error) {

	addr := fmt.Sprintf("%s:%d", serverAddr, bootstrapPort)
	fmt.Fprintf(wiz.writer, "Connecting to server bootstrap service at %s...\n", addr)

	// Connect with InsecureSkipVerify — we have no CA cert yet.
	// The fingerprint confirmation below is the trust anchor.
	tlsCfg := &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // intentional during bootstrap
	}
	creds := credentials.NewTLS(tlsCfg)
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(creds))
	if err != nil {
		return false, fmt.Errorf("dial bootstrap server: %w", err)
	}
	defer conn.Close()

	client := proto.NewBootstrapServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := client.FetchClientCerts(ctx, &proto.BootstrapRequest{})
	if err != nil {
		return false, fmt.Errorf("fetch certs from server: %w", err)
	}

	if len(resp.CACertPEM) == 0 || len(resp.ClientCertPEM) == 0 || len(resp.ClientKeyPEM) == 0 {
		return false, fmt.Errorf("server returned empty certificate data")
	}

	// Compute and display the SHA-256 fingerprint of the CA cert (DER bytes).
	block, _ := pem.Decode(resp.CACertPEM)
	if block == nil {
		return false, fmt.Errorf("failed to decode CA certificate PEM")
	}
	fingerprint := sha256.Sum256(block.Bytes)
	fingerprintHex := hex.EncodeToString(fingerprint[:])
	// Format as colon-separated pairs for readability (like SSH).
	var pairs []string
	for i := 0; i < len(fingerprintHex)-1; i += 2 {
		pairs = append(pairs, fingerprintHex[i:i+2])
	}

	fmt.Fprintln(wiz.writer)
	fmt.Fprintln(wiz.writer, "Server CA certificate fingerprint (SHA-256):")
	fmt.Fprintf(wiz.writer, "  %s\n", strings.Join(pairs, ":"))
	fmt.Fprintln(wiz.writer)
	fmt.Fprintln(wiz.writer, "Cross check: Compare this fingerprint with the one shown on your server.")
	fmt.Fprintln(wiz.writer, "SERVER CMD LINUX : tergum server --get-certs")
	fmt.Fprintln(wiz.writer, "SERVER CMD WINDOW : tergum.exe server --get-certs")
	fmt.Fprintln(wiz.writer)

	confirmed := wiz.promptYesNo("Does the fingerprint match your server's CA?", false)
	if !confirmed {
		return false, nil
	}

	// Write the cert files.
	if err := os.MkdirAll(certsDir, 0700); err != nil {
		return false, fmt.Errorf("create certs directory: %w", err)
	}

	files := []struct {
		name string
		data []byte
	}{
		{"ca.crt", resp.CACertPEM},
		{"client.crt", resp.ClientCertPEM},
		{"client.key", resp.ClientKeyPEM},
	}
	for _, f := range files {
		path := filepath.Join(certsDir, f.name)
		if err := os.WriteFile(path, f.data, 0600); err != nil {
			return false, fmt.Errorf("write %s: %w", f.name, err)
		}
	}

	return true, nil
}
