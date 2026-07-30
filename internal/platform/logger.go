package platform

import (
	"log/slog"
	"os"
	"strings"
)

// NewLogger builds the application logger and installs it as the slog default.
//
// Dev mode gets human-readable text output; everything else gets JSON so logs
// are machine-parseable by whatever collector runs in front of the service.
func NewLogger(devMode bool, level string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: parseLogLevel(level, devMode)}

	var handler slog.Handler
	if devMode {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}

	logger := slog.New(handler)
	slog.SetDefault(logger)
	return logger
}

func parseLogLevel(level string, devMode bool) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	}
	if devMode {
		return slog.LevelDebug
	}
	return slog.LevelInfo
}
