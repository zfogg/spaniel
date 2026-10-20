package storage

import (
	"context"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/zfogg/gotel/gormtrace"
	"github.com/zfogg/gotel/sqltrace"
	"gorm.io/gorm"

	"github.com/zfogg/spaniel/internal/telemetry"
)

// WithoutTracing suppresses storage spans while Spaniel persists its own
// telemetry, preventing recursive self-monitoring.
func WithoutTracing(ctx context.Context) context.Context { return sqltrace.WithoutTracing(ctx) }

func skipTracing(ctx context.Context) bool { return sqltrace.TracingDisabled(ctx) }

func newGORMPlugin() gorm.Plugin {
	return gormtrace.New(gormtrace.Config{
		Name:      "spaniel:otel",
		Scope:     "spaniel/storage",
		Namer:     storageNamer,
		System:    "duckdb",
		Namespace: "spaniel",
		Caller:    storageCallerName,
		Observe: func(ctx context.Context, operation, result string, elapsed time.Duration) {
			telemetry.Catalog().RecordStorage(ctx, operation, result, 0, float64(elapsed.Microseconds())/1000)
		},
	})
}

// storageCallerName gives Spaniel-owned queries a stable operation name without
// repeating string literals at every call site. Explicit WithQueryName values
// still take precedence in gotel's GORM adapter.
func storageCallerName() string {
	const prefix = "github.com/zfogg/spaniel/internal/storage.(*DB)."
	pcs := make([]uintptr, 64)
	n := runtime.Callers(3, pcs)
	for _, pc := range pcs[:n] {
		fn := runtime.FuncForPC(pc)
		if fn == nil {
			continue
		}
		name := fn.Name()
		if strings.HasPrefix(name, prefix) {
			method := strings.TrimPrefix(name, prefix)
			if dot := strings.IndexByte(method, '.'); dot >= 0 {
				method = method[:dot]
			}
			return method
		}
	}
	return ""
}

// registerDBSizeGauge refreshes the central owned storage-size snapshot.
// Its historic name is retained only because Open calls it.
func registerDBSizeGauge(path string) {
	if path == "" || path == ":memory:" {
		return
	}
	if fi, err := os.Stat(path); err == nil {
		telemetry.Catalog().SetStorageDBSizeCurrent(fi.Size())
	}
}
