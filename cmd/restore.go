package cmd

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gcclinux/tergum/internal/config"
	"github.com/gcclinux/tergum/internal/connection"
	"github.com/gcclinux/tergum/internal/crypto"
	"github.com/gcclinux/tergum/internal/db"
	grpcpkg "github.com/gcclinux/tergum/internal/grpc"
	"github.com/gcclinux/tergum/internal/grpc/proto"
	"github.com/gcclinux/tergum/internal/restore"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

func newRestoreCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restore [query]",
		Short: "Restore files from backup",
		Long: `Search for and restore files by name, path, or glob pattern.

Examples:
  tergum restore "*.go"                                        # restore all .go files
  tergum restore "/home/user/Documents/"                       # restore a folder
  tergum restore "report.pdf"                                  # restore by file name
  tergum restore "*.go" --dest /tmp/restored                   # custom destination
  tergum restore --backup-id abc123 --dest ./out               # restore entire backup
  tergum restore --backup-id abc123 -f report.pdf --dest ./out # restore specific file from specific backup
  tergum restore "*.log" --list                                # search without restoring`,
		RunE: runRestore,
	}

	cmd.Flags().StringP("dest", "d", "", "destination directory (default: restore to original paths)")
	cmd.Flags().IntP("concurrency", "c", 4, "number of parallel restore streams")
	cmd.Flags().String("backup-id", "", "restore from a specific backup set")
	cmd.Flags().Bool("list", false, "search and list matching files without restoring")
	cmd.Flags().StringP("file", "f", "", "specific file path, name, or pattern to restore")
	cmd.Flags().StringP("path", "p", "", "specific file path, name, or pattern to restore")
	cmd.Flags().String("client", "", "client ID to restore from (server-side only)")
	cmd.Flags().String("target", "", "target client ID to push restored files to (requires --client)")

	return cmd
}

func runRestore(cmd *cobra.Command, args []string) error {
	dest, _ := cmd.Flags().GetString("dest")
	concurrency, _ := cmd.Flags().GetInt("concurrency")
	backupID, _ := cmd.Flags().GetString("backup-id")
	listOnly, _ := cmd.Flags().GetBool("list")
	fileQuery, _ := cmd.Flags().GetString("file")
	pathQuery, _ := cmd.Flags().GetString("path")
	clientID, _ := cmd.Flags().GetString("client")
	targetClient, _ := cmd.Flags().GetString("target")

	if len(args) == 0 && backupID == "" && fileQuery == "" && pathQuery == "" {
		return fmt.Errorf("provide a search query, --file, --path, or --backup-id")
	}

	if targetClient != "" && clientID == "" {
		return fmt.Errorf("--target requires --client to specify the source client")
	}

	if fileQuery != "" && pathQuery != "" {
		return fmt.Errorf("cannot specify both --file and --path flags")
	}

	if len(args) > 0 && fileQuery != "" {
		return fmt.Errorf("cannot specify both a positional search query and the --file flag")
	}

	if len(args) > 0 && pathQuery != "" {
		return fmt.Errorf("cannot specify both a positional search query and the --path flag")
	}

	query := ""
	if fileQuery != "" {
		query = fileQuery
	} else if pathQuery != "" {
		query = pathQuery
	} else if len(args) > 0 {
		query = args[0]
	}

	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Pre-flight check: ask the server if this client is disabled.
	if err := connection.CheckClientEnabled(cfg); err != nil {
		return err
	}

	dbPath := cfg.Database.Path
	if clientID != "" {
		if cfg.Node.Role == "client" {
			return fmt.Errorf("the --client flag cannot be used on a client node")
		}
		dbPath = filepath.Join(filepath.Dir(cfg.Database.Path), "clients", clientID+".db")
		if _, err := os.Stat(dbPath); os.IsNotExist(err) {
			return fmt.Errorf("client %q database copy not found on server", clientID)
		}
	}

	repo, err := db.NewRepository(dbPath, cfg.Database.WALMode)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer repo.Close()

	ctx := context.Background()

	// Load master key for decryption if encryption is enabled.
	var masterKey []byte
	var encryptor *crypto.AESEncryptor
	if cfg.Encryption.Enabled && !listOnly {
		var dbSalt []byte
		if saltHex, err := repo.GetConfig(ctx, "encryption_salt"); err == nil && saltHex != "" {
			if s, err := hex.DecodeString(saltHex); err == nil {
				dbSalt = s
			}
		}
		key, err := loadRestoreMasterKey(cfg, clientID, dbSalt)
		if err != nil {
			return fmt.Errorf("loading encryption key: %w", err)
		}
		masterKey = key
		encryptor = crypto.NewEncryptor()
	}

	// Create data source based on node role (local or remote).
	var source restore.DataSource
	if clientID != "" {
		source = &restore.LocalDataSource{
			StorageDir: cfg.StorageDir(),
		}
	} else {
		var err error
		source, err = connection.NewDataSource(cfg)
		if err != nil {
			return fmt.Errorf("creating data source: %w", err)
		}
	}

	// Create restore engine.
	engine := restore.NewRestoreEngine(source, repo, encryptor, masterKey)

	// Find files to restore.
	var entries []restore.RestoreEntry

	if backupID != "" && query == "" {
		// Restore entire backup set.
		manifest, err := repo.GetManifest(ctx, backupID)
		if err != nil {
			return fmt.Errorf("getting manifest: %w", err)
		}
		if len(manifest) == 0 {
			return fmt.Errorf("no files found in backup %s", backupID)
		}

		for _, m := range manifest {
			// Look up the full entry for metadata.
			found, err := repo.FindByHash(ctx, m.Blake3Hash)
			if err != nil || len(found) == 0 {
				continue
			}
			entry := found[0]
			destination := resolveDestination(dest, entry.FilePath)
			entries = append(entries, restore.RestoreEntry{
				Hash:        entry.Blake3Hash,
				FileName:    entry.FileName,
				Destination: destination,
				BackupID:    backupID,
				Metadata:    &entry,
			})
		}
	} else {
		// Search by query.
		searchQuery := restore.SearchQuery{}

		if strings.Contains(query, "/") || strings.Contains(query, "\\") {
			// Looks like a path pattern.
			searchQuery.Path = "%" + query + "%"
		} else if strings.Contains(query, "*") || strings.Contains(query, "?") {
			// Glob pattern.
			searchQuery.Pattern = query
		} else {
			// Exact file name.
			searchQuery.Name = query
		}

		results, err := engine.Search(ctx, searchQuery)
		if err != nil {
			return fmt.Errorf("search failed: %w", err)
		}

		if len(results) == 0 {
			printOutput(
				map[string]interface{}{"query": query, "results": 0},
				fmt.Sprintf("No files found matching %q", query),
			)
			return nil
		}

		// Filter to specific backup if requested.
		if backupID != "" {
			var filtered []restore.RestoreEntry
			for _, entry := range results {
				if entry.BackupID == backupID {
					destination := resolveDestination(dest, entry.FilePath)
					filtered = append(filtered, restore.RestoreEntry{
						Hash:        entry.Blake3Hash,
						FileName:    entry.FileName,
						Destination: destination,
						BackupID:    entry.BackupID,
						Metadata:    &entry,
					})
				}
			}
			entries = filtered
		} else {
			// Deduplicate by file path (prefer valid encryption metadata, then latest backup_date).
			seen := make(map[string]restore.RestoreEntry)
			for _, entry := range results {
				destination := resolveDestination(dest, entry.FilePath)
				re := restore.RestoreEntry{
					Hash:        entry.Blake3Hash,
					FileName:    entry.FileName,
					Destination: destination,
					BackupID:    entry.BackupID,
					Metadata:    &entry,
				}
				existing, exists := seen[entry.FilePath]
				if !exists {
					seen[entry.FilePath] = re
				} else {
					existingHasDEK := len(existing.Metadata.EncryptedDEK) > 0 && len(existing.Metadata.Nonce) > 0
					newHasDEK := len(entry.EncryptedDEK) > 0 && len(entry.Nonce) > 0
					if newHasDEK && !existingHasDEK {
						// Prefer entry with valid encryption metadata
						seen[entry.FilePath] = re
					} else if !newHasDEK && existingHasDEK {
						// Keep existing which has valid metadata
					} else if entry.BackupDate.After(existing.Metadata.BackupDate) {
						// Both have same metadata status, prefer latest
						seen[entry.FilePath] = re
					}
				}
			}
			for _, e := range seen {
				entries = append(entries, e)
			}
		}
	}

	if len(entries) == 0 {
		fmt.Println("No files to restore.")
		return nil
	}

	// List-only mode: just show what would be restored.
	if listOnly {
		if jsonOut {
			type fileJSON struct {
				Hash     string `json:"hash"`
				Path     string `json:"path"`
				FileName string `json:"file_name"`
				BackupID string `json:"backup_id"`
				Size     int64  `json:"size"`
			}
			var out []fileJSON
			for _, e := range entries {
				f := fileJSON{
					Hash:     e.Hash,
					Path:     e.Destination,
					FileName: e.FileName,
					BackupID: e.BackupID,
				}
				if e.Metadata != nil {
					f.Size = e.Metadata.FileSize
				}
				out = append(out, f)
			}
			printOutput(map[string]interface{}{"files": out, "count": len(out)}, "")
		} else {
			fmt.Printf("Found %d file(s):\n\n", len(entries))
			for _, e := range entries {
				size := int64(0)
				if e.Metadata != nil {
					size = e.Metadata.FileSize
				}
				fmt.Printf("  %s  %8d  %s\n", e.Hash[:12], size, e.Destination)
			}
		}
		return nil
	}

	// Confirm restore.
	fmt.Printf("Restoring %d file(s)...\n", len(entries))
	if targetClient != "" {
		fmt.Printf("Target: client %s\n", targetClient)
	}
	if dest != "" {
		fmt.Printf("Destination: %s\n", dest)
	} else {
		fmt.Println("Destination: original paths")
	}
	fmt.Println()

	// If targeting a remote client, decrypt and push via gRPC.
	if targetClient != "" {
		return runRestorePushToTarget(ctx, cfg, source, repo, encryptor, masterKey, entries, dest, clientID, targetClient)
	}

	// Run batch restore.
	result, err := engine.RestoreBatch(ctx, entries, concurrency)
	if err != nil {
		return fmt.Errorf("restore failed: %w", err)
	}

	printOutput(
		map[string]interface{}{
			"restored": result.Restored,
			"failed":   result.Failed,
			"errors":   len(result.Errors),
		},
		fmt.Sprintf("Restore complete: %d restored, %d failed", result.Restored, result.Failed),
	)

	if result.Failed > 0 && !jsonOut {
		fmt.Println("\nErrors:")
		for _, e := range result.Errors {
			fmt.Printf("  - %v\n", e)
		}
	}

	return nil
}

// resolveDestination determines where to write a restored file.
// If dest is set, files are placed under dest preserving their relative structure.
// If dest is empty, files are restored to their original paths.
func resolveDestination(dest, originalPath string) string {
	if dest == "" {
		return originalPath
	}
	// Strip volume name (e.g. "C:") on Windows or UNC prefixes
	vol := filepath.VolumeName(originalPath)
	rel := originalPath[len(vol):]
	// Strip any leading slashes or backslashes to make it a relative path component
	for len(rel) > 0 && (rel[0] == '/' || rel[0] == '\\') {
		rel = rel[1:]
	}
	return filepath.Join(dest, rel)
}

// loadRestoreMasterKey is the same key loading logic used for backup.
func loadRestoreMasterKey(cfg *config.Config, clientID string, dbSalt []byte) ([]byte, error) {
	configDir := filepath.Dir(cfg.Database.Path)
	var salt []byte
	if len(dbSalt) > 0 {
		salt = dbSalt
	} else {
		saltPath := filepath.Join(configDir, "salt")
		if clientID != "" {
			clientSaltPath := filepath.Join(configDir, "clients", clientID+".salt")
			if _, err := os.Stat(clientSaltPath); err == nil {
				saltPath = clientSaltPath
			}
		}

		saltHex, err := os.ReadFile(saltPath)
		if err != nil {
			return nil, fmt.Errorf("cannot read salt file %s: %w (run 'tergum setup' first)", saltPath, err)
		}

		s, err := hex.DecodeString(strings.TrimSpace(string(saltHex)))
		if err != nil {
			return nil, fmt.Errorf("invalid salt file: %w", err)
		}
		salt = s
	}

	passphrase := os.Getenv("TERGUM_PASSPHRASE")
	if passphrase == "" {
		fmt.Print("Encryption passphrase: ")
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			passphrase = strings.TrimSpace(scanner.Text())
		}
		if passphrase == "" {
			return nil, fmt.Errorf("passphrase is required: set TERGUM_PASSPHRASE env var")
		}
	}

	enc := crypto.NewEncryptor()
	masterKey, err := enc.DeriveKey(passphrase, salt)
	if err != nil {
		return nil, fmt.Errorf("key derivation failed: %w", err)
	}

	// Verify derived master key against key_verify if it exists
	if clientID == "" {
		verifyPath := filepath.Join(configDir, "key_verify")
		if verifyData, err := os.ReadFile(verifyPath); err == nil {
			if ok, err := enc.VerifyMasterKey(masterKey, string(verifyData)); err != nil || !ok {
				return nil, fmt.Errorf("invalid passphrase: key verification failed")
			}
		}
	}

	return masterKey, nil
}

// runRestorePushToTarget decrypts files from the source client's backup and pushes
// them to a target client via the PushRestore gRPC stream.
func runRestorePushToTarget(ctx context.Context, cfg *config.Config, source restore.DataSource, repo db.Repository, encryptor *crypto.AESEncryptor, masterKey []byte, entries []restore.RestoreEntry, dest, sourceClient, targetClient string) error {
	// Look up target client address from registry in the database.
	targetAddress, err := lookupClientAddress(cfg, targetClient)
	if err != nil {
		return fmt.Errorf("looking up target client %q: %w", targetClient, err)
	}

	fmt.Printf("Connecting to target client %s at %s...\n", targetClient, targetAddress)

	// Load TLS for connecting to the target client.
	tlsCfg, _, err := connection.LoadClientTLS(cfg)
	if err != nil {
		return fmt.Errorf("loading TLS config: %w", err)
	}

	// Skip hostname verification (same pattern as RemoteClientConnector) but verify CA signature.
	tlsCfgClone := tlsCfg.Clone()
	tlsCfgClone.InsecureSkipVerify = true
	tlsCfgClone.VerifyConnection = func(cs tls.ConnectionState) error {
		opts := x509.VerifyOptions{
			Roots:         tlsCfg.RootCAs,
			CurrentTime:   time.Now(),
			Intermediates: x509.NewCertPool(),
		}
		for _, cert := range cs.PeerCertificates[1:] {
			opts.Intermediates.AddCert(cert)
		}
		_, err := cs.PeerCertificates[0].Verify(opts)
		return err
	}

	creds := credentials.NewTLS(tlsCfgClone)
	conn, err := grpc.NewClient(targetAddress, grpc.WithTransportCredentials(creds))
	if err != nil {
		return fmt.Errorf("connecting to target client %s: %w", targetClient, err)
	}

	client := grpcpkg.NewTergumClient(conn, conn, grpcpkg.ClientConfig{MaxRetries: 2})

	// Open PushRestore stream.
	stream, err := client.PushRestore(ctx)
	if err != nil {
		return fmt.Errorf("opening PushRestore stream to %s: %w", targetClient, err)
	}

	const chunkSize = 64 * 1024
	var pushed, failed int

	for i, entry := range entries {
		// Download from CAS.
		data, err := source.DownloadFile(ctx, entry.Hash)
		if err != nil {
			fmt.Printf("  SKIP %s: download failed: %v\n", entry.FileName, err)
			failed++
			continue
		}

		// Decrypt if encrypted.
		fileData := data
		if entry.Metadata != nil && len(entry.Metadata.EncryptedDEK) > 0 && len(entry.Metadata.Nonce) > 0 {
			decrypted, err := encryptor.Decrypt(data, entry.Metadata.EncryptedDEK, entry.Metadata.Nonce, masterKey)
			if err != nil {
				fmt.Printf("  SKIP %s: decrypt failed: %v\n", entry.FileName, err)
				failed++
				continue
			}
			fileData = decrypted
		}

		// Build header.
		header := &proto.FileHeader{
			Blake3Hash: entry.Hash,
			FileName:   entry.FileName,
			FilePath:   entry.Metadata.FilePath, // original path — target client resolves with its own OS
			FileSize:   int64(len(fileData)),
		}
		if entry.Metadata != nil {
			if entry.Metadata.Permissions != nil {
				header.Permissions = uint32(*entry.Metadata.Permissions)
			}
			header.Owner = entry.Metadata.Owner
			header.FileGroup = entry.Metadata.FileGroup
			header.Symlink = entry.Metadata.Symlink
			header.SymlinkTarget = entry.Metadata.SymlinkTarget
		}
		// Pass dest base in Os field on first file.
		if i == 0 {
			header.Os = dest
		}

		// Send header.
		if err := stream.Send(&proto.FileChunk{
			Payload: &proto.FileChunk_Header{Header: header},
		}); err != nil {
			return fmt.Errorf("send header for %s: %w", entry.FileName, err)
		}

		// Send data in chunks (skip for symlinks).
		if !(entry.Metadata != nil && entry.Metadata.Symlink) {
			for offset := 0; offset < len(fileData); offset += chunkSize {
				end := offset + chunkSize
				if end > len(fileData) {
					end = len(fileData)
				}
				if err := stream.Send(&proto.FileChunk{
					Payload: &proto.FileChunk_Data{Data: fileData[offset:end]},
				}); err != nil {
					return fmt.Errorf("send data for %s: %w", entry.FileName, err)
				}
			}
		}

		// Send trailer.
		if err := stream.Send(&proto.FileChunk{
			Payload: &proto.FileChunk_Trailer{Trailer: &proto.FileTrailer{
				Blake3Hash: entry.Hash,
				BytesTotal: int64(len(fileData)),
			}},
		}); err != nil {
			return fmt.Errorf("send trailer for %s: %w", entry.FileName, err)
		}

		pushed++
	}

	// Close and get response.
	resp, err := stream.CloseAndRecv()
	if err != nil {
		return fmt.Errorf("close PushRestore stream: %w", err)
	}

	printOutput(
		map[string]interface{}{
			"target":         targetClient,
			"files_sent":     pushed,
			"files_received": resp.FilesReceived,
			"files_failed":   resp.FilesFailed + int64(failed),
		},
		fmt.Sprintf("Push restore complete: %d files sent to %s (%d received, %d failed)", pushed, targetClient, resp.FilesReceived, resp.FilesFailed+int64(failed)),
	)

	return nil
}

// lookupClientAddress reads the client_registry table from the server's database
// to find the address for a given client ID.
func lookupClientAddress(cfg *config.Config, clientID string) (string, error) {
	dbPath := cfg.Database.Path
	registryDB, err := db.NewRepository(dbPath, false)
	if err != nil {
		return "", fmt.Errorf("open database: %w", err)
	}
	defer registryDB.Close()

	// Query the client_registry table directly.
	ctx := context.Background()
	address, err := registryDB.GetClientAddress(ctx, clientID)
	if err != nil {
		return "", fmt.Errorf("client %q not found in registry: %w", clientID, err)
	}
	if address == "" {
		return "", fmt.Errorf("client %q has no registered address", clientID)
	}

	return address, nil
}
