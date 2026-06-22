// Package watcher monitors filesystem events with debouncing and stability gating.
package watcher

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/ricardopadilha/tergum/internal/crypto"
	"github.com/ricardopadilha/tergum/internal/db"
)

// StableFile represents a file that has passed the stability gate.
type StableFile struct {
	Path       string
	Hash       string // BLAKE3
	ModifiedAt time.Time
	Size       int64
}

// WatcherStatus reports the current state of the watcher.
type WatcherStatus struct {
	Running      bool
	WatchedPaths int
	EventCount   int64
	StableCount  int64
}

// Watcher defines the interface for filesystem monitoring.
type Watcher interface {
	Start(ctx context.Context) error
	Stop() error
	StableFiles() <-chan StableFile
	Status() WatcherStatus
}

// Config holds watcher configuration parameters.
type Config struct {
	DebounceMs       int
	StabilitySeconds int
	ExcludePatterns  []string
	Repository       db.Repository
	IncludePaths     []string
}

// stabilityEntry tracks a file going through the stability gate.
type stabilityEntry struct {
	modTime time.Time
	timer   *time.Timer
}

// FileWatcher implements the Watcher interface using fsnotify.
type FileWatcher struct {
	cfg         Config
	fsWatcher   *fsnotify.Watcher
	stableCh    chan StableFile
	ctx         context.Context
	cancel      context.CancelFunc
	running     atomic.Bool
	eventCount  atomic.Int64
	stableCount atomic.Int64

	mu             sync.Mutex
	watchedPaths   map[string]bool // path -> true (directories currently added)
	watchExcludes  []string
	debounceTimers map[string]*time.Timer
	stabilityMap   map[string]*stabilityEntry
}

// NewFileWatcher creates a new FileWatcher with the given configuration.
func NewFileWatcher(cfg Config) (*FileWatcher, error) {
	if cfg.DebounceMs <= 0 {
		cfg.DebounceMs = 500
	}
	if cfg.StabilitySeconds <= 0 {
		cfg.StabilitySeconds = 60
	}

	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	return &FileWatcher{
		cfg:            cfg,
		fsWatcher:      fsw,
		stableCh:       make(chan StableFile, 64),
		watchedPaths:   make(map[string]bool),
		debounceTimers: make(map[string]*time.Timer),
		stabilityMap:   make(map[string]*stabilityEntry),
	}, nil
}

// Start begins watching registered paths and processing events.
func (fw *FileWatcher) Start(ctx context.Context) error {
	fw.ctx, fw.cancel = context.WithCancel(ctx)
	fw.running.Store(true)

	// Start processing events early so fsWatcher.Add calls don't deadlock on Windows.
	go fw.processEvents()

	var includes []string
	var excludes []string
	var err error

	if fw.cfg.Repository != nil {
		includes, err = fw.cfg.Repository.ListIncludePaths(ctx)
		if err != nil {
			fw.cancel()
			return err
		}
		excludes, err = fw.cfg.Repository.ListWatchExcludes(ctx)
		if err != nil {
			fw.cancel()
			return err
		}
	}

	if len(includes) == 0 {
		includes = fw.cfg.IncludePaths
	}

	fw.mu.Lock()
	fw.watchExcludes = excludes
	fw.mu.Unlock()

	for _, inc := range includes {
		_ = fw.addIncludePathInternal(inc, excludes)
	}

	return nil
}

// Stop halts the watcher and cleans up resources.
func (fw *FileWatcher) Stop() error {
	fw.running.Store(false)
	if fw.cancel != nil {
		fw.cancel()
	}

	fw.mu.Lock()
	for _, t := range fw.debounceTimers {
		t.Stop()
	}
	for _, entry := range fw.stabilityMap {
		entry.timer.Stop()
	}
	fw.debounceTimers = make(map[string]*time.Timer)
	fw.stabilityMap = make(map[string]*stabilityEntry)
	fw.mu.Unlock()

	return fw.fsWatcher.Close()
}

// addIncludePathInternal adds paths to the underlying fsnotify watcher, skipping excluded directories.
func (fw *FileWatcher) addIncludePathInternal(path string, excludes []string) error {
	for _, exc := range excludes {
		if isPathUnderOrEqual(path, exc) {
			return nil
		}
	}

	fi, err := os.Stat(path)
	if err != nil {
		return err
	}

	if !fi.IsDir() {
		err = fw.fsWatcher.Add(path)
		if err == nil {
			fw.mu.Lock()
			fw.watchedPaths[path] = true
			fw.mu.Unlock()
		}
		return err
	}

	return filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			for _, exc := range excludes {
				if isPathUnderOrEqual(p, exc) {
					return filepath.SkipDir
				}
			}
			err = fw.fsWatcher.Add(p)
			if err == nil {
				fw.mu.Lock()
				fw.watchedPaths[p] = true
				fw.mu.Unlock()
			}
			return err
		}
		return nil
	})
}

// StableFiles returns a channel that emits files that have passed the stability gate.
func (fw *FileWatcher) StableFiles() <-chan StableFile {
	return fw.stableCh
}

// Status returns the current watcher status.
func (fw *FileWatcher) Status() WatcherStatus {
	fw.mu.Lock()
	watchedPaths := len(fw.watchedPaths)
	fw.mu.Unlock()

	return WatcherStatus{
		Running:      fw.running.Load(),
		WatchedPaths: watchedPaths,
		EventCount:   fw.eventCount.Load(),
		StableCount:  fw.stableCount.Load(),
	}
}

// processEvents is the main event loop goroutine.
func (fw *FileWatcher) processEvents() {
	for {
		select {
		case <-fw.ctx.Done():
			return
		case event, ok := <-fw.fsWatcher.Events:
			if !ok {
				return
			}
			fw.eventCount.Add(1)
			fw.handleEvent(event)
		case _, ok := <-fw.fsWatcher.Errors:
			if !ok {
				return
			}
			// Errors are logged but not fatal.
		}
	}
}

// handleEvent processes a single fsnotify event through the pipeline.
func (fw *FileWatcher) handleEvent(event fsnotify.Event) {
	path := event.Name

	// Step 1: Immediate exclude pattern check.
	if fw.isExcluded(path) {
		return
	}

	// Step 2: Debounce - reset the sliding window timer.
	fw.mu.Lock()
	if timer, exists := fw.debounceTimers[path]; exists {
		timer.Stop()
	}

	debounceD := time.Duration(fw.cfg.DebounceMs) * time.Millisecond
	fw.debounceTimers[path] = time.AfterFunc(debounceD, func() {
		fw.onDebounceExpired(path)
	})
	fw.mu.Unlock()
}

// isExcluded checks if a path matches any exclude pattern or watch exclusion.
func (fw *FileWatcher) isExcluded(path string) bool {
	fw.mu.Lock()
	excludes := fw.watchExcludes
	fw.mu.Unlock()

	for _, exc := range excludes {
		if isPathUnderOrEqual(path, exc) {
			return true
		}
	}

	name := filepath.Base(path)
	for _, pattern := range fw.cfg.ExcludePatterns {
		// Try matching against the base filename.
		if matched, _ := filepath.Match(pattern, name); matched {
			return true
		}
		// Try matching against the full path for directory patterns.
		if strings.Contains(pattern, "/") || strings.Contains(pattern, string(os.PathSeparator)) {
			// Normalize separators for comparison.
			normalizedPath := filepath.ToSlash(path)
			normalizedPattern := filepath.ToSlash(pattern)
			if strings.Contains(normalizedPath, strings.TrimSuffix(normalizedPattern, "/")) {
				return true
			}
		}
	}
	return false
}

func isPathUnderOrEqual(p, exc string) bool {
	p = filepath.Clean(p)
	exc = filepath.Clean(exc)
	isWindows := filepath.Separator == '\\'
	if isWindows {
		p = strings.ToLower(p)
		exc = strings.ToLower(exc)
	}
	if p == exc {
		return true
	}
	prefix := exc + string(filepath.Separator)
	return strings.HasPrefix(p, prefix)
}

// onDebounceExpired is called when the debounce timer fires for a path.
// It enters the path into the stability gate.
func (fw *FileWatcher) onDebounceExpired(path string) {
	fw.mu.Lock()
	delete(fw.debounceTimers, path)

	// Check if file exists and get its mod time.
	info, err := os.Stat(path)
	if err != nil {
		// File doesn't exist; discard.
		fw.mu.Unlock()
		return
	}

	modTime := info.ModTime()

	// If already in stability map, reset the timer (modification during stability window).
	if entry, exists := fw.stabilityMap[path]; exists {
		entry.timer.Stop()
		entry.modTime = modTime
		stabilityD := time.Duration(fw.cfg.StabilitySeconds) * time.Second
		entry.timer = time.AfterFunc(stabilityD, func() {
			fw.onStabilityExpired(path)
		})
		fw.mu.Unlock()
		return
	}

	// Enter stability gate.
	stabilityD := time.Duration(fw.cfg.StabilitySeconds) * time.Second
	fw.stabilityMap[path] = &stabilityEntry{
		modTime: modTime,
		timer: time.AfterFunc(stabilityD, func() {
			fw.onStabilityExpired(path)
		}),
	}
	fw.mu.Unlock()
}

// onStabilityExpired is called when the stability timer fires for a path.
// It verifies the file is still stable before emitting it.
func (fw *FileWatcher) onStabilityExpired(path string) {
	fw.mu.Lock()
	entry, exists := fw.stabilityMap[path]
	if !exists {
		fw.mu.Unlock()
		return
	}
	delete(fw.stabilityMap, path)
	expectedModTime := entry.modTime
	fw.mu.Unlock()

	// Check context before doing work.
	if fw.ctx.Err() != nil {
		return
	}

	// Verify file still exists and hasn't been modified.
	info, err := os.Stat(path)
	if err != nil {
		// File no longer exists; discard.
		return
	}

	if !info.ModTime().Equal(expectedModTime) {
		// File was modified during stability window; re-enter stability gate.
		fw.mu.Lock()
		stabilityD := time.Duration(fw.cfg.StabilitySeconds) * time.Second
		fw.stabilityMap[path] = &stabilityEntry{
			modTime: info.ModTime(),
			timer: time.AfterFunc(stabilityD, func() {
				fw.onStabilityExpired(path)
			}),
		}
		fw.mu.Unlock()
		return
	}

	// File is stable. Compute BLAKE3 hash.
	hash, err := crypto.HashFile(path)
	if err != nil {
		return
	}

	sf := StableFile{
		Path:       path,
		Hash:       hash,
		ModifiedAt: info.ModTime(),
		Size:       info.Size(),
	}

	fw.stableCount.Add(1)

	// Send to channel (non-blocking if buffer is full, drop).
	select {
	case fw.stableCh <- sf:
	case <-fw.ctx.Done():
	}
}
