package grpc

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ricardopadilha/tergum/internal/backup"
	"github.com/ricardopadilha/tergum/internal/db"
	"github.com/ricardopadilha/tergum/internal/grpc/proto"
	"github.com/ricardopadilha/tergum/internal/model"
	"github.com/ricardopadilha/tergum/internal/storage"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// --- Error Mapping Tests ---

func TestMapError_Nil(t *testing.T) {
	if got := MapError(nil); got != nil {
		t.Fatalf("MapError(nil) = %v, want nil", got)
	}
}

func TestMapError_ConfigError(t *testing.T) {
	err := MapError(&model.ConfigError{Message: "bad config"})
	st, ok := status.FromError(err)
	if !ok {
		t.Fatal("expected gRPC status error")
	}
	if st.Code() != codes.InvalidArgument {
		t.Errorf("got code %v, want InvalidArgument", st.Code())
	}
}

func TestMapError_ConnectionError(t *testing.T) {
	err := MapError(&model.ConnectionError{Message: "no route"})
	st, _ := status.FromError(err)
	if st.Code() != codes.Unavailable {
		t.Errorf("got code %v, want Unavailable", st.Code())
	}
}

func TestMapError_AuthError(t *testing.T) {
	err := MapError(&model.AuthError{Message: "bad creds"})
	st, _ := status.FromError(err)
	if st.Code() != codes.Unauthenticated {
		t.Errorf("got code %v, want Unauthenticated", st.Code())
	}
}

func TestMapError_StorageError(t *testing.T) {
	err := MapError(&model.StorageError{Message: "disk full"})
	st, _ := status.FromError(err)
	if st.Code() != codes.ResourceExhausted {
		t.Errorf("got code %v, want ResourceExhausted", st.Code())
	}
}

func TestMapError_StoppedError(t *testing.T) {
	err := MapError(&model.StoppedError{Message: "user cancelled"})
	st, _ := status.FromError(err)
	if st.Code() != codes.Canceled {
		t.Errorf("got code %v, want Canceled", st.Code())
	}
}

func TestMapError_BackupFailedError(t *testing.T) {
	err := MapError(&model.BackupFailedError{Message: "scan failed"})
	st, _ := status.FromError(err)
	if st.Code() != codes.Internal {
		t.Errorf("got code %v, want Internal", st.Code())
	}
}

func TestMapError_StorageNotFound(t *testing.T) {
	err := MapError(storage.ErrNotFound)
	st, _ := status.FromError(err)
	if st.Code() != codes.NotFound {
		t.Errorf("got code %v, want NotFound", st.Code())
	}
}

func TestMapError_GenericError(t *testing.T) {
	err := MapError(errors.New("something went wrong"))
	st, _ := status.FromError(err)
	if st.Code() != codes.Internal {
		t.Errorf("got code %v, want Internal", st.Code())
	}
}

// --- Ping Handler Test ---

func TestCommandServer_Ping(t *testing.T) {
	srv := NewCommandServer(CommandServerConfig{
		BackupEngine: &mockBackupEngine{},
		Repo:         &mockRepo{},
		Version:      "3.0.0-test",
	})

	resp, err := srv.Ping(context.Background(), &proto.PingRequest{})
	if err != nil {
		t.Fatalf("Ping() error: %v", err)
	}
	if resp.Version != "3.0.0-test" {
		t.Errorf("Version = %q, want %q", resp.Version, "3.0.0-test")
	}
	if resp.Uptime == "" {
		t.Error("Uptime should not be empty")
	}
}

// --- ExchangeManifest Test ---

func TestDataServer_ExchangeManifest(t *testing.T) {
	cas := &mockStore{
		existing: map[string]bool{
			"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa": true,
			"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb": true,
		},
	}

	srv := NewDataServer(DataServerConfig{
		Store: cas,
		Repo:  &mockRepo{},
	})

	manifest := &proto.Manifest{
		Entries: []*proto.ManifestEntryProto{
			{Blake3Hash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", FilePath: "/a.txt", FileSize: 100},
			{Blake3Hash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", FilePath: "/b.txt", FileSize: 200},
			{Blake3Hash: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", FilePath: "/c.txt", FileSize: 300},
			{Blake3Hash: "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd", FilePath: "/d.txt", FileSize: 400},
		},
	}

	diff, err := srv.ExchangeManifest(context.Background(), manifest)
	if err != nil {
		t.Fatalf("ExchangeManifest() error: %v", err)
	}

	// 2 hashes exist (a, b), 2 don't (c, d).
	if diff.DedupCount != 2 {
		t.Errorf("DedupCount = %d, want 2", diff.DedupCount)
	}
	if len(diff.NeededHashes) != 2 {
		t.Errorf("NeededHashes count = %d, want 2", len(diff.NeededHashes))
	}
	if diff.TotalFiles != 4 {
		t.Errorf("TotalFiles = %d, want 4", diff.TotalFiles)
	}

	// Verify the needed hashes are correct.
	needed := make(map[string]bool)
	for _, h := range diff.NeededHashes {
		needed[h] = true
	}
	if !needed["cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"] {
		t.Error("expected hash 'cccc...' in needed list")
	}
	if !needed["dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"] {
		t.Error("expected hash 'dddd...' in needed list")
	}
}

func TestDataServer_ExchangeManifest_Empty(t *testing.T) {
	srv := NewDataServer(DataServerConfig{
		Store: &mockStore{existing: map[string]bool{}},
		Repo:  &mockRepo{},
	})

	diff, err := srv.ExchangeManifest(context.Background(), &proto.Manifest{})
	if err != nil {
		t.Fatalf("ExchangeManifest() error: %v", err)
	}
	if diff.DedupCount != 0 || diff.TotalFiles != 0 || len(diff.NeededHashes) != 0 {
		t.Errorf("empty manifest should produce empty diff, got %+v", diff)
	}
}

// --- Semaphore / Concurrency Tests ---

func TestSemaphore_BlocksWhenFull(t *testing.T) {
	sem := NewSemaphore(2)

	// Acquire both slots.
	if err := sem.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := sem.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Third acquire should block until context is cancelled.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := sem.Acquire(ctx)
	if err == nil {
		t.Fatal("expected error when semaphore is full and context times out")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded, got %v", err)
	}
}

func TestSemaphore_ReleaseMakesSlotAvailable(t *testing.T) {
	sem := NewSemaphore(1)

	if err := sem.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Release the slot.
	sem.Release()

	// Now acquire should succeed.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if err := sem.Acquire(ctx); err != nil {
		t.Fatalf("expected acquire to succeed after release, got %v", err)
	}
}

func TestSemaphore_ConcurrentAccess(t *testing.T) {
	const maxConcurrent = 3
	sem := NewSemaphore(maxConcurrent)

	var activeCount atomic.Int32
	var maxSeen atomic.Int32
	var wg sync.WaitGroup

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := sem.Acquire(context.Background()); err != nil {
				return
			}
			defer sem.Release()

			current := activeCount.Add(1)
			// Track max observed concurrency.
			for {
				old := maxSeen.Load()
				if current <= old || maxSeen.CompareAndSwap(old, current) {
					break
				}
			}

			time.Sleep(time.Millisecond)
			activeCount.Add(-1)
		}()
	}

	wg.Wait()

	if maxSeen.Load() > int32(maxConcurrent) {
		t.Errorf("max concurrent = %d, want <= %d", maxSeen.Load(), maxConcurrent)
	}
}

func TestSemaphore_TryAcquire(t *testing.T) {
	sem := NewSemaphore(1)

	if !sem.TryAcquire() {
		t.Fatal("first TryAcquire should succeed")
	}
	if sem.TryAcquire() {
		t.Fatal("second TryAcquire should fail when full")
	}

	sem.Release()

	if !sem.TryAcquire() {
		t.Fatal("TryAcquire should succeed after release")
	}
}

func TestSemaphore_Available(t *testing.T) {
	sem := NewSemaphore(3)
	if sem.Available() != 3 {
		t.Errorf("Available = %d, want 3", sem.Available())
	}

	sem.Acquire(context.Background())
	if sem.Available() != 2 {
		t.Errorf("Available = %d, want 2", sem.Available())
	}

	sem.Acquire(context.Background())
	sem.Acquire(context.Background())
	if sem.Available() != 0 {
		t.Errorf("Available = %d, want 0", sem.Available())
	}

	sem.Release()
	if sem.Available() != 1 {
		t.Errorf("Available = %d, want 1", sem.Available())
	}
}

// --- Mock Implementations ---

type mockBackupEngine struct {
	runErr  error
	stopErr error
}

func (m *mockBackupEngine) RunBackup(ctx context.Context, req backup.BackupRequest) (*backup.BackupResult, error) {
	if m.runErr != nil {
		return nil, m.runErr
	}
	return &backup.BackupResult{BackupID: "mock-backup-id"}, nil
}

func (m *mockBackupEngine) Stop(ctx context.Context) error {
	return m.stopErr
}

type mockStore struct {
	existing map[string]bool
	data     map[string][]byte
}

func (m *mockStore) Put(ctx context.Context, hash string, reader io.Reader) error {
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	if m.data == nil {
		m.data = make(map[string][]byte)
	}
	m.data[hash] = data
	m.existing[hash] = true
	return nil
}

func (m *mockStore) Get(ctx context.Context, hash string) (io.ReadCloser, error) {
	if m.data == nil {
		return nil, storage.ErrNotFound
	}
	data, ok := m.data[hash]
	if !ok {
		return nil, storage.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (m *mockStore) Exists(ctx context.Context, hash string) (bool, error) {
	return m.existing[hash], nil
}

func (m *mockStore) Delete(ctx context.Context, hash string) error {
	delete(m.existing, hash)
	delete(m.data, hash)
	return nil
}

func (m *mockStore) RefCount(ctx context.Context, hash string) (int64, error) {
	return 0, nil
}

type mockRepo struct {
	jobs []model.BackupJob
}

func (m *mockRepo) InsertBackupEntry(ctx context.Context, entry model.BackupEntry) error { return nil }
func (m *mockRepo) GetManifest(ctx context.Context, backupID string) ([]model.ManifestEntry, error) {
	return nil, nil
}
func (m *mockRepo) FindByHash(ctx context.Context, hash string) ([]model.BackupEntry, error) {
	return nil, nil
}
func (m *mockRepo) FindByPath(ctx context.Context, pattern string) ([]model.BackupEntry, error) {
	return nil, nil
}
func (m *mockRepo) CountHashReferences(ctx context.Context, hash string) (int64, error) {
	return 0, nil
}
func (m *mockRepo) DeleteEntries(ctx context.Context, filter db.DeleteFilter) (int64, error) {
	return 0, nil
}
func (m *mockRepo) QueryEntries(ctx context.Context, filter db.DeleteFilter) ([]model.BackupEntry, error) {
	return nil, nil
}
func (m *mockRepo) DeleteOrphanJobs(ctx context.Context) (int64, error) {
	return 0, nil
}
func (m *mockRepo) CreateJob(ctx context.Context, job model.BackupJob) error { return nil }
func (m *mockRepo) UpdateJob(ctx context.Context, jobID string, update db.JobUpdate) error {
	return nil
}
func (m *mockRepo) ListJobs(ctx context.Context, filter db.JobFilter) ([]model.BackupJob, error) {
	return m.jobs, nil
}
func (m *mockRepo) GetFileVersions(ctx context.Context, filePath string) ([]model.BackupEntry, error) {
	return nil, nil
}
func (m *mockRepo) GetExpiredEntries(ctx context.Context, now time.Time) ([]model.BackupEntry, error) {
	return nil, nil
}
func (m *mockRepo) GetAllFilePaths(ctx context.Context) ([]string, error) { return nil, nil }
func (m *mockRepo) DeleteEntryByID(ctx context.Context, id int64) error   { return nil }
func (m *mockRepo) InsertRetentionPolicy(ctx context.Context, policy model.RetentionPolicy) error {
	return nil
}
func (m *mockRepo) DeleteRetentionPolicy(ctx context.Context, name string) error { return nil }
func (m *mockRepo) ListRetentionPolicies(ctx context.Context) ([]model.RetentionPolicy, error) {
	return nil, nil
}
func (m *mockRepo) RecordRestore(ctx context.Context, entry db.RestoreRecord) error { return nil }
func (m *mockRepo) ListRestoreHistory(ctx context.Context, limit int) ([]db.RestoreRecord, error) {
	return nil, nil
}
func (m *mockRepo) DeleteAllActivity(ctx context.Context) (int64, error) { return 0, nil }
func (m *mockRepo) AddWatchExclude(ctx context.Context, path string) error          { return nil }
func (m *mockRepo) RemoveWatchExclude(ctx context.Context, path string) error       { return nil }
func (m *mockRepo) ListWatchExcludes(ctx context.Context) ([]string, error)         { return nil, nil }
func (m *mockRepo) AddIncludePath(ctx context.Context, path string) error           { return nil }
func (m *mockRepo) RemoveIncludePath(ctx context.Context, path string) error        { return nil }
func (m *mockRepo) ListIncludePaths(ctx context.Context) ([]string, error)          { return nil, nil }
func (m *mockRepo) AddExcludePattern(ctx context.Context, pattern string) error     { return nil }
func (m *mockRepo) RemoveExcludePattern(ctx context.Context, pattern string) error  { return nil }
func (m *mockRepo) ListExcludePatterns(ctx context.Context) ([]string, error)       { return nil, nil }
func (m *mockRepo) Close() error                                                    { return nil }
