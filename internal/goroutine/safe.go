// Package goroutine provides a panic-safe goroutine launcher.
package goroutine

import (
	"fmt"
	"runtime/debug"

	"github.com/zfogg/spaniel/internal/logger"
)

// Go launches fn in a new goroutine. If fn panics the panic is recovered,
// logged at ERROR level via the logger package (never re-panicked), and the
// goroutine exits cleanly.
//
// labels are optional key=value strings that appear in the log record as
// "goroutine" fields for filtering (e.g. Go(fn, "subsystem", "ingestion")).
func Go(fn func(), labels ...string) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				args := make([]any, 0, len(labels)+4)
				for i := 0; i+1 < len(labels); i += 2 {
					args = append(args, labels[i], labels[i+1])
				}
				args = append(args,
					"panic", fmt.Sprintf("%v", r),
					"stack", string(debug.Stack()),
				)
				logger.Error("goroutine panic recovered", args...)
			}
		}()
		fn()
	}()
}
