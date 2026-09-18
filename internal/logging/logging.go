// Package logging provides the application's structured logger and a
// context-scoped accessor for it.
//
// The logger is threaded through context.Context rather than through
// constructor arguments: every service and repository method already accepts a
// ctx, so a request-scoped logger reaches every layer without a single
// signature change.
package logging

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
)

type ctxKey struct{}

// Options configures the root logger.
type Options struct {
	Level  string // debug | info | warn | error
	Format string // json | text
	Output io.Writer
}

// New builds the root logger. JSON is the sane default for production; text is
// easier to read during development.
func New(opts Options) *slog.Logger {
	out := opts.Output
	if out == nil {
		out = os.Stdout
	}

	handlerOpts := &slog.HandlerOptions{Level: ParseLevel(opts.Level)}

	var handler slog.Handler
	if strings.EqualFold(opts.Format, "text") {
		handler = slog.NewTextHandler(out, handlerOpts)
	} else {
		handler = slog.NewJSONHandler(out, handlerOpts)
	}

	return slog.New(handler)
}

// ParseLevel maps a config string to a slog level, defaulting to info.
func ParseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// Into returns a context carrying l.
func Into(ctx context.Context, l *slog.Logger) context.Context {
	if l == nil {
		return ctx
	}
	return context.WithValue(ctx, ctxKey{}, l)
}

// FromContext returns the request-scoped logger, or the process default when
// the context carries none (background jobs, tests).
func FromContext(ctx context.Context) *slog.Logger {
	if ctx != nil {
		if l, ok := ctx.Value(ctxKey{}).(*slog.Logger); ok && l != nil {
			return l
		}
	}
	return slog.Default()
}
