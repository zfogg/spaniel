package storage

import (
	"context"
	"os"
	"time"

	"github.com/zfogg/gotel/go/dbmetrics"
	"github.com/zfogg/gotel/go/gormtrace"
	"github.com/zfogg/gotel/go/sqltrace"
	"github.com/zfogg/spaniel/internal/telemetry"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"
)

var storageMetrics = dbmetrics.Must(dbmetrics.Config{
	Scope: "spaniel/storage", System: "duckdb", Namespace: "spaniel",
})

// observeStorageQuery preserves Spaniel's storage catalog alongside gotel's
// standard database client duration histogram.
func observeStorageQuery(ctx context.Context, _ string, result string, elapsed time.Duration) {
	telemetry.Catalog().RecordStorage(ctx, "query", result, 0, float64(elapsed.Microseconds())/1000)
}

var storageObserver = dbmetrics.Chain(storageMetrics.Observer(), observeStorageQuery)

// WithoutTracing suppresses recursive spans while Spaniel stores its own
// telemetry.
func WithoutTracing(ctx context.Context) context.Context { return sqltrace.WithoutTracing(ctx) }

func skipTracing(ctx context.Context) bool { return sqltrace.TracingDisabled(ctx) }

// newGORMPlugin instruments embedded DuckDB work as INTERNAL spans. Names are
// explicit when supplied, otherwise recovered from the owning DB method.
func newGORMPlugin() gorm.Plugin {
	return gormtrace.New(gormtrace.Config{
		Name:      "spaniel:otel",
		Scope:     "spaniel/storage",
		Namer:     storageNamer,
		System:    "duckdb",
		Namespace: "spaniel",
		Dialect:   sqltrace.DialectDuckDB,
		SpanKind:  trace.SpanKindInternal,
		Caller:    sqltrace.CallerNamer("github.com/zfogg/spaniel/internal/storage.(*DB)."),
		Observe:   storageObserver,
	})
}

// registerDBSizeGauge refreshes the central owned storage-size snapshot.
func registerDBSizeGauge(path string) {
	if path == "" || path == ":memory:" {
		return
	}
	if fi, err := os.Stat(path); err == nil {
		telemetry.Catalog().SetStorageDBSizeCurrent(fi.Size())
	}
}
