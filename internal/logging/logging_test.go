package logging_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/ferriyusra/clean-arch-go-gin/internal/logging"
	"github.com/ferriyusra/clean-arch-go-gin/internal/testutil"
)

func TestParseLevel(t *testing.T) {
	tests := []struct {
		in   string
		want slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"INFO", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"error", slog.LevelError},
		{"nonsense", slog.LevelInfo},
		{"", slog.LevelInfo},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			testutil.Equal(t, logging.ParseLevel(tt.in), tt.want, "level")
		})
	}
}

func TestNewRespectsFormatAndLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.New(logging.Options{Level: "warn", Format: "json", Output: &buf})

	logger.Info("should be filtered out")
	testutil.Equal(t, buf.Len(), 0, "bytes written below the configured level")

	logger.Warn("should be recorded")
	out := buf.String()
	testutil.True(t, strings.Contains(out, "should be recorded"), "message recorded")
	testutil.True(t, strings.HasPrefix(strings.TrimSpace(out), "{"), "json format")
}

func TestNewDefaultsToText(t *testing.T) {
	var buf bytes.Buffer
	logging.New(logging.Options{Level: "info", Format: "text", Output: &buf}).Info("hello")

	testutil.True(t, strings.Contains(buf.String(), "msg=hello"), "text format")
}

// TestFromContextFallsBackToTheDefault matters because services and
// repositories call it on contexts that never passed through the middleware,
// such as in unit tests and background jobs.
func TestFromContextFallsBackToTheDefault(t *testing.T) {
	if logging.FromContext(context.Background()) == nil {
		t.Errorf("expected a usable logger for a bare context")
	}
	//lint:ignore SA1012 a nil context is exactly the case under test
	if logging.FromContext(nil) == nil { //nolint:staticcheck
		t.Errorf("expected a usable logger for a nil context")
	}
}

func TestIntoAndFromContextRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.New(logging.Options{Level: "debug", Format: "json", Output: &buf}).
		With("request_id", "abc-123")

	ctx := logging.Into(context.Background(), logger)
	logging.FromContext(ctx).Info("work happened")

	testutil.True(t, strings.Contains(buf.String(), "abc-123"), "attributes survive the round trip")
}

func TestIntoIgnoresANilLogger(t *testing.T) {
	ctx := logging.Into(context.Background(), nil)

	if logging.FromContext(ctx) == nil {
		t.Errorf("expected the default logger, got nil")
	}
}
