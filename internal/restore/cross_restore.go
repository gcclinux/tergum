package restore

import (
	"context"
	"fmt"
	"strings"

	"github.com/gcclinux/tergum/internal/crypto"
	"github.com/gcclinux/tergum/internal/db"
	"github.com/gcclinux/tergum/internal/grpc/proto"
	"github.com/gcclinux/tergum/internal/model"
)

// PushRestoreStream is the subset of proto.CommandService_PushRestoreClient the
// cross-client restorer needs. Defining it here keeps internal/restore free of a
// dependency on internal/grpc (only internal/grpc/proto, a leaf package), which
// avoids an import cycle: internal/grpc wires the concrete stream in.
type PushRestoreStream interface {
	Send(*proto.FileChunk) error
	CloseAndRecv() (*proto.PushRestoreResponse, error)
}

// StreamOpener opens a PushRestore stream to the target client identified by
// targetClientID. The grpc layer provides the concrete implementation (registry
// lookup + mTLS dial / tunnel); internal/restore stays transport-agnostic.
type StreamOpener func(ctx context.Context, targetClientID string) (PushRestoreStream, error)

// CrossRestoreRequest describes a cross-client restore: pull files from the
// source client's backup and push them to the target client.
type CrossRestoreRequest struct {
	SourceClientID string
	TargetClientID string
	Query          string
	BackupID       string
	File           string
	Dest           string
}

// CrossRestoreResult reports how many files were streamed to the target.
type CrossRestoreResult struct {
	FilesSent     int64
	FilesReceived int64
	FilesFailed   int64
}

// CrossClientRestorer orchestrates the server-side cross-client restore push:
// it searches the source client's backup database, reads each file from the CAS,
// decrypts it with the source client's master key, and streams it to the target
// client via a PushRestore stream. No key material is ever sent to or by an
// admin client — decryption happens entirely on the server.
//
// It depends only on internal/db, a CAS-backed DataSource, internal/crypto,
// internal/model and internal/grpc/proto, satisfying design approach 1.
type CrossClientRestorer struct {
	source    DataSource // reads bytes from the server's CAS
	repo      db.Repository
	encryptor *crypto.AESEncryptor
	masterKey []byte
	opener    StreamOpener
}

// NewCrossClientRestorer builds a CrossClientRestorer. masterKey is the source
// client's server-side master key (nil if the server cannot derive it, in which
// case RestoreToTarget fails closed with a ConfigError when encrypted data is
// encountered). opener supplies the transport to the target client.
func NewCrossClientRestorer(source DataSource, repo db.Repository, encryptor *crypto.AESEncryptor, masterKey []byte, opener StreamOpener) *CrossClientRestorer {
	return &CrossClientRestorer{
		source:    source,
		repo:      repo,
		encryptor: encryptor,
		masterKey: masterKey,
		opener:    opener,
	}
}

// chunkSize is the payload size for streamed file data (64KB).
const chunkSize = 64 * 1024

// RestoreToTarget collects the requested files from the source backup, decrypts
// them server-side, and streams them to the target client. It returns the
// per-file counts reported by the target plus any files that failed locally
// (download/decrypt).
func (r *CrossClientRestorer) RestoreToTarget(ctx context.Context, req CrossRestoreRequest) (CrossRestoreResult, error) {
	entries, err := r.collectEntries(ctx, req)
	if err != nil {
		return CrossRestoreResult{}, err
	}
	if len(entries) == 0 {
		return CrossRestoreResult{}, &model.ConfigError{Message: "no matching files found in source backup"}
	}

	stream, err := r.opener(ctx, req.TargetClientID)
	if err != nil {
		return CrossRestoreResult{}, fmt.Errorf("open push stream to target %q: %w", req.TargetClientID, err)
	}

	var sent, failedLocal int64
	first := true
	for _, entry := range entries {
		fileData, derr := r.loadDecrypted(ctx, entry)
		if derr != nil {
			failedLocal++
			continue
		}

		header := &proto.FileHeader{
			Blake3Hash: entry.Blake3Hash,
			FileName:   entry.FileName,
			FilePath:   entry.FilePath, // original path — target client resolves with its own OS
			FileSize:   int64(len(fileData)),
			Owner:      entry.Owner,
			FileGroup:  entry.FileGroup,
			Symlink:    entry.Symlink,
		}
		if entry.Permissions != nil {
			header.Permissions = uint32(*entry.Permissions)
		}
		header.SymlinkTarget = entry.SymlinkTarget
		// Pass the base dest path in the Os field on the first file.
		if first {
			header.Os = req.Dest
			first = false
		}

		if err := stream.Send(&proto.FileChunk{Payload: &proto.FileChunk_Header{Header: header}}); err != nil {
			return CrossRestoreResult{}, fmt.Errorf("send header for %s: %w", entry.FileName, err)
		}

		if !entry.Symlink {
			for offset := 0; offset < len(fileData); offset += chunkSize {
				end := offset + chunkSize
				if end > len(fileData) {
					end = len(fileData)
				}
				if err := stream.Send(&proto.FileChunk{Payload: &proto.FileChunk_Data{Data: fileData[offset:end]}}); err != nil {
					return CrossRestoreResult{}, fmt.Errorf("send data for %s: %w", entry.FileName, err)
				}
			}
		}

		if err := stream.Send(&proto.FileChunk{Payload: &proto.FileChunk_Trailer{Trailer: &proto.FileTrailer{
			Blake3Hash: entry.Blake3Hash,
			BytesTotal: int64(len(fileData)),
		}}}); err != nil {
			return CrossRestoreResult{}, fmt.Errorf("send trailer for %s: %w", entry.FileName, err)
		}

		sent++
	}

	resp, err := stream.CloseAndRecv()
	if err != nil {
		return CrossRestoreResult{}, fmt.Errorf("close push stream: %w", err)
	}

	return CrossRestoreResult{
		FilesSent:     sent,
		FilesReceived: resp.FilesReceived,
		FilesFailed:   resp.FilesFailed + failedLocal,
	}, nil
}

// collectEntries resolves the request into a deduplicated set of backup entries.
// Precedence mirrors the existing restore-push flows: explicit file/hash first,
// then whole-backup, then search query.
func (r *CrossClientRestorer) collectEntries(ctx context.Context, req CrossRestoreRequest) ([]model.BackupEntry, error) {
	var entries []model.BackupEntry
	seen := make(map[string]bool)
	add := func(e model.BackupEntry) {
		if !seen[e.Blake3Hash] {
			seen[e.Blake3Hash] = true
			entries = append(entries, e)
		}
	}

	switch {
	case req.File != "":
		found, err := r.searchEntries(ctx, req.File)
		if err != nil {
			return nil, err
		}
		for _, e := range found {
			add(e)
		}
	case req.BackupID != "" && req.Query == "":
		manifest, err := r.repo.GetManifest(ctx, req.BackupID)
		if err != nil {
			return nil, fmt.Errorf("load manifest for backup %q: %w", req.BackupID, err)
		}
		for _, m := range manifest {
			found, err := r.repo.FindByHash(ctx, m.Blake3Hash)
			if err != nil || len(found) == 0 {
				continue
			}
			add(found[0])
		}
	case req.Query != "":
		found, err := r.searchEntries(ctx, req.Query)
		if err != nil {
			return nil, err
		}
		for _, e := range found {
			add(e)
		}
	}

	return entries, nil
}

// searchEntries runs a name/path/pattern search against the source repository,
// matching the heuristic used by the existing restore flows.
func (r *CrossClientRestorer) searchEntries(ctx context.Context, query string) ([]model.BackupEntry, error) {
	engine := NewRestoreEngine(r.source, r.repo, r.encryptor, r.masterKey)
	searchQuery := SearchQuery{}
	switch {
	case strings.Contains(query, "/") || strings.Contains(query, "\\"):
		searchQuery.Path = "%" + query + "%"
	case strings.Contains(query, "*") || strings.Contains(query, "?"):
		searchQuery.Pattern = query
	default:
		searchQuery.Name = query
	}
	return engine.Search(ctx, searchQuery)
}

// loadDecrypted downloads an entry from the CAS and decrypts it if it carries
// encryption metadata. If the data is encrypted but the server holds no source
// master key, it fails closed with a ConfigError.
func (r *CrossClientRestorer) loadDecrypted(ctx context.Context, entry model.BackupEntry) ([]byte, error) {
	data, err := r.source.DownloadFile(ctx, entry.Blake3Hash)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", entry.Blake3Hash, err)
	}

	if len(entry.EncryptedDEK) > 0 && len(entry.Nonce) > 0 {
		if r.encryptor == nil || len(r.masterKey) == 0 {
			return nil, &model.ConfigError{Message: "source client encryption key unavailable on server"}
		}
		decrypted, err := r.encryptor.Decrypt(data, entry.EncryptedDEK, entry.Nonce, r.masterKey)
		if err != nil {
			return nil, fmt.Errorf("decrypt %s: %w", entry.Blake3Hash, err)
		}
		return decrypted, nil
	}
	return data, nil
}
