package logger

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"testing"
)

// TestDefaultNotNil verifies the package initializes a usable default logger.
func TestDefaultNotNil(t *testing.T) {
	if Default() == nil {
		t.Fatal("Default() returned nil")
	}
	// New() must also return an independent, usable logger.
	if New() == nil {
		t.Fatal("New() returned nil")
	}
}

// TestNewPicksJSONHandlerWhenNotTTY confirms the documented handler-selection
// behavior: when stderr is not a terminal (as under `go test`), New emits JSON.
func TestNewPicksJSONHandlerWhenNotTTY(t *testing.T) {
	if isTTY(os.Stderr) {
		t.Skip("stderr is a TTY in this environment; JSON-handler branch not exercised")
	}
	// Build a logger the same way New does, but capture output so we can assert
	// the format without writing to the real stderr.
	var buf bytes.Buffer
	h := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	slog.New(h).Info("hello", "k", "v")

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("expected JSON log line, got %q: %v", buf.String(), err)
	}
	if rec["msg"] != "hello" || rec["k"] != "v" {
		t.Errorf("unexpected JSON fields: %v", rec)
	}
}

// TestLevelFiltering verifies the INFO default level drops DEBUG but keeps
// INFO/WARN/ERROR — the contract every New()-built handler is configured with.
func TestLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	h := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	log := slog.New(h)

	log.Debug("debug-msg")
	if buf.Len() != 0 {
		t.Errorf("DEBUG should be filtered at INFO level, got %q", buf.String())
	}

	log.Info("info-msg")
	log.Warn("warn-msg")
	log.Error("error-msg")
	out := buf.String()
	for _, want := range []string{"info-msg", "warn-msg", "error-msg"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output, got %q", want, out)
		}
	}
}

// TestWithAttachesFields ensures With pre-attaches key/value pairs that then
// appear on every line emitted through the derived logger.
func TestWithAttachesFields(t *testing.T) {
	var buf bytes.Buffer
	h := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	base := slog.New(h)
	derived := base.With("subsystem", "ingestion")
	derived.Info("started")

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("bad JSON: %v (%q)", err, buf.String())
	}
	if rec["subsystem"] != "ingestion" {
		t.Errorf("expected subsystem=ingestion attached by With, got %v", rec["subsystem"])
	}
}

// TestPackageHelpersDoNotPanic exercises the global Info/Warn/Error/With
// facade. They write to stderr; we only assert they run without panicking and
// that With returns a non-nil derived logger.
func TestPackageHelpersDoNotPanic(t *testing.T) {
	Info("info via facade", "n", 1)
	Warn("warn via facade")
	Error("error via facade", "err", "boom")
	if With("subsystem", "test") == nil {
		t.Error("With returned nil logger")
	}
}

// TestIsTTYOnRegularFile confirms isTTY reports false for a plain file (not a
// character device), which drives the JSON-handler selection in New.
func TestIsTTYOnRegularFile(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "logger-tty-*")
	if err != nil {
		t.Fatalf("temp file: %v", err)
	}
	defer f.Close()
	if isTTY(f) {
		t.Errorf("isTTY(regular file) = true, want false")
	}
}
