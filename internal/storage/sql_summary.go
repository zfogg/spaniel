package storage

import (
	"context"
	"fmt"
	"strings"

	"github.com/zfogg/gotel/sqltrace"
)

var storageNamer = sqltrace.Namer{Prefix: "spaniel.storage"}

// storageQueryName retains Spaniel's stricter historical requirement that a
// query name contain at least two characters. gotel owns the shared syntax and
// prefixing contract.
func storageQueryName(name string) string {
	name = strings.TrimSpace(name)
	if len(name) < 2 {
		panic(fmt.Sprintf("storage query name must match [A-Za-z][A-Za-z0-9_]+: %q", name))
	}
	return storageNamer.Name(name)
}

// WithQueryName attaches a stable simple name to one database operation.
func WithQueryName(ctx context.Context, name string) context.Context {
	_ = storageQueryName(name)
	return storageNamer.WithName(ctx, strings.TrimSpace(name))
}

func queryNameFromContext(ctx context.Context) string { return sqltrace.NameFromContext(ctx) }

func sqlSummary(query string) string { return sqltrace.Summary(sqltrace.DialectDuckDB, query) }

func sanitizeSQL(query string) (string, bool) { return sqltrace.Sanitize(query) }
