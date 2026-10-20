package storage

import (
	"context"

	"github.com/zfogg/gotel/sqltrace"
)

var storageNamer = sqltrace.MustNamer("spaniel.storage")

// storageQueryName qualifies and validates a stable storage operation name.
func storageQueryName(name string) string { return storageNamer.MustName(name) }

// WithQueryName attaches a stable storage operation name to one database call.
func WithQueryName(ctx context.Context, name string) context.Context {
	return storageNamer.WithName(ctx, name)
}

func queryNameFromContext(ctx context.Context) string { return sqltrace.NameFromContext(ctx) }

// sqlSummary returns a bounded, parameter-free description of DuckDB SQL.
func sqlSummary(query string) string { return sqltrace.Summary(sqltrace.DialectDuckDB, query) }

// sanitizeSQL delegates the privacy boundary to gotel's DuckDB policy.
func sanitizeSQL(query string) (string, bool) {
	result, ok := sqltrace.SanitizeDialect(sqltrace.DialectDuckDB, query)
	return result.Text, ok
}
