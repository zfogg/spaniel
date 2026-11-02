package storage

import (
	"context"

	gotelsql "github.com/zfogg/gotel/go/tracers/sql"
	sequel "github.com/zfogg/sequel/go"
)

var storageNamer = gotelsql.StorageNamer("duckdb")

// storageQueryName qualifies and validates a stable storage operation name.
func storageQueryName(name string) string { return storageNamer.MustName(name) }

// WithQueryName attaches a stable storage operation name to one database call.
func WithQueryName(ctx context.Context, name string) context.Context {
	return storageNamer.WithName(ctx, name)
}

func queryNameFromContext(ctx context.Context) string { return gotelsql.NameFromContext(ctx) }

// sqlSummary returns a bounded, parameter-free description of DuckDB SQL.
func sqlSummary(query string) string { return sequel.Summary(sequel.DialectDuckDB, query) }

// sanitizeSQL delegates the privacy boundary to sequel's DuckDB policy.
func sanitizeSQL(query string) (string, bool) {
	result, ok := sequel.SanitizeDialect(sequel.DialectDuckDB, query)
	return result.Text, ok
}
