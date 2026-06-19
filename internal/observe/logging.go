// Package observe provides structured logging and Prometheus metrics for Tergum.
package observe

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
)

// SetupLogging configures the global slog logger with the specified level and format.
// Level must be one of: "debug", "info", "warn", "error".
// Format must be one of: "text", "json".
func SetupLogging(level, format string) error {
	lvl, err := parseLevel(level)
	if err != nil {
		return err
	}

	opts := &slog.HandlerOptions{Level: lvl}

	var handler slog.Handler
	switch strings.ToLower(format) {
	case "json":
		handler = slog.NewJSONHandler(os.Stdout, opts)
	case "text":
		handler = slog.NewTextHandler(os.Stdout, opts)
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
