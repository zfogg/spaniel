// Package logger provides a structured slog-backed logger for spaniel.
// It picks a JSONHandler when stderr is not a TTY (production / CI) and a
// TextHandler otherwise (interactive development).  Every subsystem should
// call With to attach a "subsystem" field so log lines can be filtered by
// component.
package logger

import (
	"log/slog"
	"os"
)

var defaultLogger *slog.Logger

func init() {
	defaultLogger = New()
}

// New returns a fresh *slog.Logger wired to os.Stderr.  The handler is chosen
// based on whether stderr looks like a terminal; callers that want a specific
// handler should build their own logger directly with slog.New.
func New() *slog.Logger {
	var h slog.Handler
	if isTTY(os.Stderr) {
		h = slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level:     slog.LevelInfo,
			AddSource: false,
		})
	} else {
		h = slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
			Level:     slog.LevelInfo,
			AddSource: false,
		})
	}
	return slog.New(h)
}

// Default returns the package-level default logger.
func Default() *slog.Logger { return defaultLogger }

// With returns a logger derived from the package default with the given
// key-value pairs pre-attached.  Typical use:
//
//	var log = logger.With("subsystem", "ingestion")
func With(args ...any) *slog.Logger { return defaultLogger.With(args...) }

// Info logs at INFO level on the default logger.
func Info(msg string, args ...any) { defaultLogger.Info(msg, args...) }

// Warn logs at WARN level on the default logger.
func Warn(msg string, args ...any) { defaultLogger.Warn(msg, args...) }

// Error logs at ERROR level on the default logger.
func Error(msg string, args ...any) { defaultLogger.Error(msg, args...) }

// isTTY reports whether f is a character device (terminal).
func isTTY(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
