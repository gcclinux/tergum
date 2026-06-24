// Package observe provides structured logging and Prometheus metrics for Tergum.
package observe

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
)

var (
	logMu        sync.RWMutex
	logHistory   []string
	logMaxLines  = 1000
	logListeners []func(string)
)

// LogWriter is a custom writer that duplicates output to logHistory and listeners.
type LogWriter struct {
	stdout io.Writer
}

func (w *LogWriter) Write(p []byte) (n int, err error) {
	n, err = w.stdout.Write(p)
	if err != nil {
		return n, err
	}

	line := string(p)

	logMu.Lock()
	if len(logHistory) >= logMaxLines {
		logHistory = logHistory[1:]
	}
	logHistory = append(logHistory, line)

	listeners := make([]func(string), len(logListeners))
	copy(listeners, logListeners)
	logMu.Unlock()

	for _, listener := range listeners {
		listener(line)
	}

	return n, nil
}

// GetLogHistory returns the stored log lines.
func GetLogHistory() []string {
	logMu.RLock()
	defer logMu.RUnlock()
	res := make([]string, len(logHistory))
	copy(res, logHistory)
	return res
}

// RegisterLogListener adds a callback for every log line written.
func RegisterLogListener(listener func(string)) {
	logMu.Lock()
	defer logMu.Unlock()
	logListeners = append(logListeners, listener)
}

// SetupLogging configures the global slog logger with the specified level and format.
// Level must be one of: "debug", "info", "warn", "error".
// Format must be one of: "text", "json".
func SetupLogging(level, format string) error {
	lvl, err := parseLevel(level)
	if err != nil {
		return err
	}

	opts := &slog.HandlerOptions{Level: lvl}
	writer := &LogWriter{stdout: os.Stdout}

	var handler slog.Handler
	switch strings.ToLower(format) {
	case "json":
		handler = slog.NewJSONHandler(writer, opts)
	case "text":
		handler = slog.NewTextHandler(writer, opts)
	default:
		return fmt.Errorf("unsupported log format %q: must be \"text\" or \"json\"", format)
	}

	slog.SetDefault(slog.New(handler))
	return nil
}

// Logger returns a *slog.Logger with a "component" attribute pre-set.
// Use this to scope log output to a specific subsystem.
func Logger(component string) *slog.Logger {
	return slog.Default().With(slog.String("component", component))
}

func parseLevel(level string) (slog.Level, error) {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("unsupported log level %q: must be one of \"debug\", \"info\", \"warn\", \"error\"", level)
	}
}
