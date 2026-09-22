package cmd

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gcclinux/tergum/internal/config"
	"github.com/gcclinux/tergum/internal/crypto"
	"github.com/gcclinux/tergum/internal/db"
	grpcpkg "github.com/gcclinux/tergum/internal/grpc"
	"github.com/gcclinux/tergum/internal/grpc/proto"
	"github.com/gcclinux/tergum/internal/identity"
	tlspkg "github.com/gcclinux/tergum/internal/tls"
	"github.com/spf13/cobra"
)

func newRecoverCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "recover",
		Short: "Recover database, settings, and configuration for a rebuilt client",
		Long: `Recovers the client's backup database (tergum.db), include/exclude paths,
and encryption configuration from the server onto a freshly installed or rebuilt client.

This allows a client rebuilt with a fresh OS install or an updated distribution to resume
backups and restorations without losing history or re-uploading duplicate data.

Guards:
  - Enforces OS family compatibility: prevents recovering across incompatible OS families
    (e.g., Windows to Linux, where paths and file permissions cannot be reconciled), while
    permitting distribution switches (e.g., Ubuntu to Fedora) or newer OS releases.
  - Guards against multi-machine conflicts: only a single active machine can be bound
    to a client database at any time.

Examples:
  tergum recover                                # Interactive recovery wizard
  tergum recover --server 192.168.1.50          # Specify server address
  tergum recover --server 192.168.1.50 --client-id laptop-01
  tergum recover --server 192.168.1.50 --client-id laptop-01 --force`,
		RunE: runRecover,
	}

	cmd.Flags().String("server", "", "server address (hostname or IP)")
	cmd.Flags().String("client-id", "", "client ID to recover")
	cmd.Flags().Bool("force", false, "force rebind even if the client is currently marked active")

	return cmd
}

func runRecover(cmd *cobra.Command, args []string) error {
	serverAddr, _ := cmd.Flags().GetString("server")
	clientID, _ := cmd.Flags().GetString("client-id")
	force, _ := cmd.Flags().GetBool("force")

	wiz := newSetupWizard(os.Stdin, os.Stdout)
	return runRecoverInteractive(wiz, serverAddr, clientID, force)
}

func runRecoverInteractive(wiz *setupWizard, serverAddr, clientID string, force bool) error {
	fmt.Fprintln(wiz.writer, "Tergum Client Recovery Wizard")
	fmt.Fprintln(wiz.writer, "=============================")
	fmt.Fprintln(wiz.writer, "This wizard restores your client backup database, include/exclude paths,")
	fmt.Fprintln(wiz.writer, "encryption configuration, and rebinds this rebuilt machine to your backup set.")
	fmt.Fprintln(wiz.writer)

	// 1. Determine server address
	if serverAddr == "" {
		for {
			serverAddr = wiz.prompt("Server address (hostname or IP)", "")
			if serverAddr == "" {
				fmt.Fprintln(wiz.writer, "Error: Server address is required.")
				continue
			}
			addrLower := strings.ToLower(strings.TrimSpace(serverAddr))
			if addrLower == "localhost" || addrLower == "127.0.0.1" || addrLower == "::1" {
				fmt.Fprintln(wiz.writer, "Warning: Connecting to localhost. Ensure the server is listening locally.")
			}
			break
		}
	}

	configDir := config.DefaultConfigDir()
	certsDir := filepath.Join(configDir, "certs")
	caCertPath := filepath.Join(certsDir, "ca.crt")
	clientCertPath := filepath.Join(certsDir, "client.crt")
	clientKeyPath := filepath.Join(certsDir, "client.key")

	// 2. Ensure TLS certificates exist
	hasCerts := fileExists(caCertPath) && fileExists(clientCertPath) && fileExists(clientKeyPath)
	if !hasCerts {
		fmt.Fprintln(wiz.writer, "\nTLS certificates not found locally. Connecting to server bootstrap service (port 7402)...")
		fetched, err := fetchCertsFromServer(wiz, serverAddr, 7402, certsDir)
		if err != nil {
			fmt.Fprintf(wiz.writer, "ERROR: Could not fetch certificates from server: %v\n", err)
			printManualCertInstructions(wiz.writer, serverAddr, certsDir)
			return fmt.Errorf("certificate bootstrap failed: %w", err)
		}
		if !fetched {
			fmt.Fprintln(wiz.writer, "Certificate import cancelled.")
			return fmt.Errorf("certificate bootstrap declined by user")
		}
		fmt.Fprintf(wiz.writer, "Certificates imported into %s\n", certsDir)
	}

	// 3. Connect to server via mTLS
	tlsMgr := tlspkg.NewManager()
	tlsCfg, err := tlsMgr.LoadClientTLS(caCertPath, clientCertPath, clientKeyPath)
	if err != nil {
		return fmt.Errorf("loading client TLS certificates: %w", err)
	}

	ctx := context.Background()
	client, err := grpcpkg.Connect(ctx, serverAddr, 7400, 7401, tlsCfg)
	if err != nil {
		return fmt.Errorf("connecting to server at %s: %w", serverAddr, err)
	}

	// 4. Query recoverable clients from server
	recResp, err := client.ListRecoverableClients(ctx)
	if err != nil {
		return fmt.Errorf("listing recoverable clients from server: %w", err)
	}

	if len(recResp.Clients) == 0 {
		return fmt.Errorf("no recoverable client databases found on server %s", serverAddr)
	}

	// 5. Select client ID to recover
	var selectedClient *proto.RecoverableClientInfo
	if clientID != "" {
		for i := range recResp.Clients {
			if recResp.Clients[i].ClientId == clientID {
				selectedClient = &recResp.Clients[i]
				break
			}
		}
		if selectedClient == nil {
			return fmt.Errorf("client %q not found or has no database available for recovery on server", clientID)
		}
	} else {
		fmt.Fprintln(wiz.writer, "\nRecoverable clients available on server:")
		for i, c := range recResp.Clients {
			osFam := c.OsFamily
			if osFam == "" {
				osFam = "unknown"
			}
			host := c.Hostname
			if host == "" {
				host = "-"
			}
			lastBackup := c.LastBackup
			if lastBackup == "" {
				lastBackup = "never"
			}
			fmt.Fprintf(wiz.writer, "  [%d] %-18s (OS: %-7s, Host: %-15s, Status: %-7s, Last Backup: %s)\n",
				i+1, c.ClientId, osFam, host, c.Status, lastBackup)
		}

		choiceStr := wiz.prompt("\nSelect client to recover (number or client ID)", "1")
		choiceNum, parseErr := strconv.Atoi(choiceStr)
		if parseErr == nil && choiceNum >= 1 && choiceNum <= len(recResp.Clients) {
			selectedClient = &recResp.Clients[choiceNum-1]
		} else {
			for i := range recResp.Clients {
				if strings.EqualFold(recResp.Clients[i].ClientId, choiceStr) {
					selectedClient = &recResp.Clients[i]
					break
				}
			}
		}
		if selectedClient == nil {
			return fmt.Errorf("invalid client selection: %q", choiceStr)
		}
		clientID = selectedClient.ClientId
	}

	// 6. Gather local machine identity
	localIdent := identity.GetSystemIdentity()

	// 7. Check OS compatibility
	if selectedClient.OsFamily != "" {
		if err := identity.CheckOSCompatibility(selectedClient.OsFamily, localIdent.OSFamily); err != nil {
			return fmt.Errorf("INCOMPATIBLE OS DETECTED: client %q was registered with OS family %q, but this machine is running %q.\n"+
				"%w\n"+
				"Restoring across incompatible OS families (such as Windows to Linux) is prohibited because file path syntax and system permissions cannot be mapped correctly.\n"+
				"To configure backups for this new OS, run 'tergum setup' as a fresh client.",
				clientID, selectedClient.OsFamily, localIdent.OSFamily, err)
		}
	}

	// 8. Single active machine guard: check if client is currently online
	if selectedClient.Status == "online" && !force {
		fmt.Fprintln(wiz.writer)
		fmt.Fprintf(wiz.writer, "WARNING: Client %q is currently ONLINE on the server.\n", clientID)
		fmt.Fprintln(wiz.writer, "Another machine may already be actively connected with this client identity.")
		fmt.Fprintln(wiz.writer, "Rebinding will disconnect the existing active machine and take over this identity.")
		confirm := wiz.promptYesNo("Do you want to force rebind and disconnect the existing machine?", false)
		if !confirm {
			return fmt.Errorf("recovery cancelled: client %q is currently active on another machine", clientID)
		}
		force = true
	}

	// 9. Download the database from server
	dbPath := filepath.Join(configDir, "tergum.db")
	if fileExists(dbPath) {
		fi, _ := os.Stat(dbPath)
		if fi != nil && fi.Size() > 0 {
			overwrite := wiz.promptYesNo(fmt.Sprintf("Existing database found at %s. Overwrite with database from server?", dbPath), false)
			if !overwrite {
				return fmt.Errorf("recovery aborted: existing database preserved")
			}
		}
	}

	fmt.Fprintf(wiz.writer, "\nDownloading database for client %q from server...\n", clientID)
	if err := grpcpkg.DownloadDatabaseFromServer(ctx, client.DataClient(), clientID, dbPath); err != nil {
		return fmt.Errorf("downloading database from server: %w", err)
	}
	fmt.Fprintf(wiz.writer, "Database successfully restored to %s\n", dbPath)

	// 10. Open restored database and extract configuration
	repo, err := db.NewRepository(dbPath, true)
	if err != nil {
		return fmt.Errorf("opening restored database: %w", err)
	}
	defer repo.Close()

	includePaths, _ := repo.ListIncludePaths(ctx)
	excludePatterns, _ := repo.ListExcludePatterns(ctx)
	dbSaltHex, _ := repo.GetConfig(ctx, "encryption_salt")
	dbKeyVerify, _ := repo.GetConfig(ctx, "key_verify")

	// 11. Configure and verify encryption
	saltPath := filepath.Join(configDir, "salt")
	verifyPath := filepath.Join(configDir, "key_verify")
	enc := crypto.NewEncryptor()

	if dbSaltHex != "" {
		saltBytes, err := hex.DecodeString(strings.TrimSpace(dbSaltHex))
		if err != nil {
			return fmt.Errorf("corrupt encryption salt in restored database: %w", err)
		}

		fmt.Fprintln(wiz.writer, "\n--- Encryption Configuration ---")
		fmt.Fprintln(wiz.writer, "The restored database contains encrypted backup history.")

		for {
			passphrase := wiz.prompt("Enter your encryption passphrase to verify and unlock backups", "")
			if passphrase == "" {
				return fmt.Errorf("encryption passphrase is required")
			}

			masterKey, err := enc.DeriveKey(passphrase, saltBytes)
			if err != nil {
				return fmt.Errorf("deriving master key: %w", err)
			}

			if dbKeyVerify != "" {
				ok, _ := enc.VerifyMasterKey(masterKey, dbKeyVerify)
				if !ok {
					fmt.Fprintln(wiz.writer, "Incorrect passphrase for this backup set.")
					if !wiz.promptYesNo("Try again?", true) {
						return fmt.Errorf("recovery cancelled: incorrect encryption passphrase")
					}
					continue
				}
				fmt.Fprintln(wiz.writer, "Passphrase verified successfully!")
				if err := os.WriteFile(verifyPath, []byte(dbKeyVerify), 0600); err != nil {
					return fmt.Errorf("writing verification file: %w", err)
				}
			} else {
				// Database did not have key_verify stored; create and store it.
				verificationPlaintext := []byte("tergum-key-verification")
				ciphertext, wrappedDEK, nonce, err := enc.Encrypt(verificationPlaintext, masterKey)
				if err == nil {
					verifyData := fmt.Sprintf("%s:%s:%s",
						hex.EncodeToString(ciphertext),
						hex.EncodeToString(wrappedDEK),
						hex.EncodeToString(nonce),
					)
					_ = os.WriteFile(verifyPath, []byte(verifyData), 0600)
					_ = repo.SetConfig(ctx, "key_verify", verifyData)
				}
				fmt.Fprintln(wiz.writer, "Passphrase configured and verification token saved.")
			}

			if err := os.WriteFile(saltPath, []byte(dbSaltHex), 0600); err != nil {
				return fmt.Errorf("writing salt file: %w", err)
			}
			break
		}
	}

	// 12. Write rebuilt tergum.toml
	cfg := buildConfig("client", serverAddr, localIdent.Hostname, "", certsDir, configDir, true)
	cfg.Client.IncludePaths = includePaths
	cfg.Client.ExcludePatterns = excludePatterns

	configPath := config.DefaultConfigPath()
	if err := writeConfigTOML(configPath, cfg); err != nil {
		return fmt.Errorf("writing configuration file: %w", err)
	}
	fmt.Fprintf(wiz.writer, "Configuration file written to %s\n", configPath)

	// 13. Rebind client identity on server
	localIP := ""
	if ips, err := getLocalIPs(); err == nil && len(ips) > 0 {
		localIP = ips[0]
	}
	clientAddr := localIP
	if clientAddr != "" {
		clientAddr = fmt.Sprintf("%s:7400", clientAddr)
	}

	rebindResp, err := client.RebindClient(ctx, &proto.RebindClientRequest{
		ClientId:  clientID,
		Address:   clientAddr,
		OsFamily:  localIdent.OSFamily,
		MachineId: localIdent.MachineID,
		Hostname:  localIdent.Hostname,
		Force:     force,
	})
	if err != nil {
		return fmt.Errorf("rebinding client on server: %w", err)
	}

	// 14. Output recovery summary
	if jsonOut {
		printOutput(map[string]interface{}{
			"status":           "success",
			"client_id":        clientID,
			"os_family":        localIdent.OSFamily,
			"hostname":         localIdent.Hostname,
			"machine_id":       localIdent.MachineID,
			"include_paths":    includePaths,
			"exclude_patterns": excludePatterns,
			"database_path":    dbPath,
			"config_path":      configPath,
			"server_message":   rebindResp.Message,
		}, "")
		return nil
	}

	fmt.Fprintln(wiz.writer)
	fmt.Fprintln(wiz.writer, "==================================================================")
	fmt.Fprintln(wiz.writer, "                      RECOVERY COMPLETED                          ")
	fmt.Fprintln(wiz.writer, "==================================================================")
	fmt.Fprintf(wiz.writer, "Client ID:         %s\n", clientID)
	fmt.Fprintf(wiz.writer, "OS Family:         %s\n", localIdent.OSFamily)
	fmt.Fprintf(wiz.writer, "Machine ID:        %s\n", localIdent.MachineID)
	fmt.Fprintf(wiz.writer, "Hostname:          %s\n", localIdent.Hostname)
	fmt.Fprintf(wiz.writer, "Database:          %s\n", dbPath)
	fmt.Fprintf(wiz.writer, "Config File:       %s\n", configPath)
	fmt.Fprintf(wiz.writer, "Include Paths:     %d paths restored\n", len(includePaths))
	for _, p := range includePaths {
		fmt.Fprintf(wiz.writer, "  + %s\n", p)
	}
	fmt.Fprintf(wiz.writer, "Exclude Patterns:  %d patterns restored\n", len(excludePatterns))
	fmt.Fprintf(wiz.writer, "Server Status:     %s\n", rebindResp.Message)
	fmt.Fprintln(wiz.writer)
	fmt.Fprintln(wiz.writer, "Next steps:")
	fmt.Fprintln(wiz.writer, "  tergum client    — start the client daemon")
	fmt.Fprintln(wiz.writer, "  tergum backup    — run a manual or scheduled backup")
	fmt.Fprintln(wiz.writer, "  tergum restore   — search and restore files from past backups")
	fmt.Fprintln(wiz.writer, "==================================================================")

	return nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
