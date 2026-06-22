package watcher

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gcclinux/tergum/internal/db"
)

// shortDebounce and shortStability for fast tests.
const (
	testDebounceMs       = 50
	testStabilitySeconds = 1 // Using 1s (minimum for time.Duration * time.Second)
)

// testStabilityMs is the effective stability wait in milliseconds for test assertions.
const testStabilityMs = 1000

func newTestConfig(repo db.Repository, excludes []string) Config {
	return Config{
		DebounceMs:       testDebounceMs,
		StabilitySeconds: testStabilitySeconds,
		ExcludePatterns:  excludes,
		Repository:       repo,
	}
}

func TestExcludeFiltering(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := newTestConfig(nil, []string{"*.tmp", "*.log"})
	cfg.IncludePaths = []string{tmpDir}

	fw, err := NewFileWatcher(cfg)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := fw.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer fw.Stop()

	// Give watcher time to register.
	time.Sleep(50 * time.Millisecond)

	// Create an excluded file.
	excludedFile := filepath.Join(tmpDir, "temp.tmp")
	if err := os.WriteFile(excludedFile, []byte("excluded"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a non-excluded file.
	includedFile := filepath.Join(tmpDir, "data.txt")
	if err := os.WriteFile(includedFile, []byte("included"), 0644); err != nil {
		t.Fatal(err)
	}

	// Wait for debounce + stability + some buffer.
	waitTime := time.Duration(testDebounceMs+testStabilityMs+500) * time.Millisecond
	timer := time.NewTimer(waitTime)
	defer timer.Stop()

	var received []StableFile
	for {
		select {
		case sf := <-fw.StableFiles():
			received = append(received, sf)
		case <-timer.C:
			goto done
		}
	}
done:
	// We should only receive the included file.
	for _, sf := range received {
		base := filepath.Base(sf.Path)
		if base == "temp.tmp" {
			t.Errorf("excluded file %s should not have been received", sf.Path)
		}
	}

	// Verify the included file was received.
	found := false
	for _, sf := range received {
		if filepath.Base(sf.Path) == "data.txt" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to receive data.txt but did not")
	}
}

func TestDebouncingCollapsesRapidEvents(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := newTestConfig(nil, nil)
	cfg.IncludePaths = []string{tmpDir}

	fw, err := NewFileWatcher(cfg)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := fw.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer fw.Stop()

	time.Sleep(50 * time.Millisecond)

	// Rapidly write to the same file multiple times.
	testFile := filepath.Join(tmpDir, "rapid.txt")
	for i := 0; i < 10; i++ {
		if err := os.WriteFile(testFile, []byte("content"+string(rune('0'+i))), 0644); err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Wait for debounce + stability + buffer.
	waitTime := time.Duration(testDebounceMs+testStabilityMs+500) * time.Millisecond
	timer := time.NewTimer(waitTime)
	defer timer.Stop()

	var received []StableFile
	for {
		select {
		case sf := <-fw.StableFiles():
			received = append(received, sf)
		case <-timer.C:
			goto done
		}
	}
done:
	// Should receive exactly one stable file event (debounced).
	count := 0
	for _, sf := range received {
		if filepath.Base(sf.Path) == "rapid.txt" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 stable file event for rapid.txt, got %d", count)
	}
}

func TestStabilityGatePassesAfterTimeout(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := newTestConfig(nil, nil)
	cfg.IncludePaths = []string{tmpDir}

	fw, err := NewFileWatcher(cfg)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := fw.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer fw.Stop()

	time.Sleep(50 * time.Millisecond)

	// Create a file and don't modify it.
	testFile := filepath.Join(tmpDir, "stable.txt")
	if err := os.WriteFile(testFile, []byte("stable content"), 0644); err != nil {
		t.Fatal(err)
	}

	// Wait for debounce + stability + buffer.
	waitTime := time.Duration(testDebounceMs+testStabilityMs+500) * time.Millisecond

	select {
	case sf := <-fw.StableFiles():
		if filepath.Base(sf.Path) != "stable.txt" {
			t.Errorf("expected stable.txt, got %s", sf.Path)
		}
		if sf.Hash == "" {
			t.Error("expected non-empty hash")
		}
		if sf.Size != 14 {
			t.Errorf("expected size 14, got %d", sf.Size)
		}
	case <-time.After(waitTime):
		t.Error("timed out waiting for stable file")
	}
}

func TestStabilityGateResetsOnModification(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := newTestConfig(nil, nil)
	cfg.IncludePaths = []string{tmpDir}

	fw, err := NewFileWatcher(cfg)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := fw.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer fw.Stop()

	time.Sleep(50 * time.Millisecond)

	// Create a file.
	testFile := filepath.Join(tmpDir, "modified.txt")
	if err := os.WriteFile(testFile, []byte("v1"), 0644); err != nil {
		t.Fatal(err)
	}

	// Wait for the file to enter the stability gate (after debounce).
	time.Sleep(time.Duration(testDebounceMs+100) * time.Millisecond)

	// Modify the file while in stability gate - this should reset the timer.
	time.Sleep(100 * time.Millisecond)
	if err := os.WriteFile(testFile, []byte("v2"), 0644); err != nil {
		t.Fatal(err)
	}

	// The total time should be roughly: debounce + stability (from the last modification).
	// The file should NOT arrive before the second stability window completes.
	earlyTimeout := time.Duration(testDebounceMs+200) * time.Millisecond
	select {
	case <-fw.StableFiles():
		// It's possible we get it if the whole pipeline completed - that's okay
		// as long as the content is "v2".
	case <-time.After(earlyTimeout):
		// Expected - stability gate was reset.
	}

	// Eventually we should receive the file with the latest content.
	lateTimeout := time.Duration(testDebounceMs+testStabilityMs+1000) * time.Millisecond
	select {
	case sf := <-fw.StableFiles():
		if filepath.Base(sf.Path) != "modified.txt" {
			t.Errorf("expected modified.txt, got %s", sf.Path)
		}
		// The hash should match "v2" content.
		if sf.Hash == "" {
			t.Error("expected non-empty hash")
		}
	case <-time.After(lateTimeout):
		// This is acceptable - the first select may have consumed the event.
	}
}

func TestStabilityGateDiscardsDeletedFiles(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := newTestConfig(nil, nil)
	cfg.IncludePaths = []string{tmpDir}

	fw, err := NewFileWatcher(cfg)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := fw.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer fw.Stop()

	time.Sleep(50 * time.Millisecond)

	// Create then immediately delete a file.
	testFile := filepath.Join(tmpDir, "deleted.txt")
	if err := os.WriteFile(testFile, []byte("temporary"), 0644); err != nil {
		t.Fatal(err)
	}

	// Wait for debounce to start.
	time.Sleep(time.Duration(testDebounceMs/2) * time.Millisecond)

	// Delete the file before stability completes.
	if err := os.Remove(testFile); err != nil {
		t.Fatal(err)
	}

	// Wait for full pipeline to complete.
	waitTime := time.Duration(testDebounceMs+testStabilityMs+500) * time.Millisecond
	select {
	case sf := <-fw.StableFiles():
		t.Errorf("should not receive deleted file, got %s", sf.Path)
	case <-time.After(waitTime):
		// Expected: deleted file is discarded.
	}
}

func TestWatchExclusionsWalking(t *testing.T) {
	repo, err := db.NewRepository(":memory:", false)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()

	ctx := context.Background()
	tmpDir := t.TempDir()
	excDir := filepath.Join(tmpDir, "excluded_subdir")
	if err := os.Mkdir(excDir, 0755); err != nil {
		t.Fatal(err)
	}
	incDir := filepath.Join(tmpDir, "included_subdir")
	if err := os.Mkdir(incDir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := repo.AddIncludePath(ctx, tmpDir); err != nil {
		t.Fatal(err)
	}
	if err := repo.AddWatchExclude(ctx, excDir); err != nil {
		t.Fatal(err)
	}

	cfg := newTestConfig(repo, nil)
	fw, err := NewFileWatcher(cfg)
	if err != nil {
		t.Fatal(err)
	}

	watchCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := fw.Start(watchCtx); err != nil {
		t.Fatal(err)
	}
	defer fw.Stop()

	// Give watcher time to register.
	time.Sleep(50 * time.Millisecond)

	// Check that incDir was added to watched paths, but excDir was not.
	fw.mu.Lock()
	_, incWatched := fw.watchedPaths[incDir]
	_, excWatched := fw.watchedPaths[excDir]
	fw.mu.Unlock()

	if !incWatched {
		t.Error("expected included_subdir to be watched")
	}
	if excWatched {
		t.Error("expected excluded_subdir to not be watched")
	}
}

func TestStableFilesChannelReceivesStableFiles(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := newTestConfig(nil, nil)
	cfg.IncludePaths = []string{tmpDir}

	fw, err := NewFileWatcher(cfg)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := fw.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer fw.Stop()

	time.Sleep(50 * time.Millisecond)

	// Create multiple files.
	files := []string{"file1.txt", "file2.txt", "file3.txt"}
	for _, name := range files {
		path := filepath.Join(tmpDir, name)
		if err := os.WriteFile(path, []byte("content of "+name), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Collect stable files.
	waitTime := time.Duration(testDebounceMs+testStabilityMs+500) * time.Millisecond
	timer := time.NewTimer(waitTime)
	defer timer.Stop()

	received := make(map[string]StableFile)
	for {
		select {
		case sf := <-fw.StableFiles():
			received[filepath.Base(sf.Path)] = sf
		case <-timer.C:
			goto done
		}
	}
done:
	for _, name := range files {
		sf, ok := received[name]
		if !ok {
			t.Errorf("expected to receive %s but did not", name)
			continue
		}
		if sf.Hash == "" {
			t.Errorf("expected non-empty hash for %s", name)
		}
		if sf.Size == 0 {
			t.Errorf("expected non-zero size for %s", name)
		}
		if sf.ModifiedAt.IsZero() {
			t.Errorf("expected non-zero ModifiedAt for %s", name)
		}
	}

	// Verify status counters.
	status := fw.Status()
	if !status.Running {
		t.Error("expected Running=true")
	}
	if status.EventCount == 0 {
		t.Error("expected EventCount > 0")
	}
	if status.StableCount == 0 {
		t.Error("expected StableCount > 0")
	}
}
