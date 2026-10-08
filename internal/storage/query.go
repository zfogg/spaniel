package storage

import (
	"context"
	"database/sql"
	"fmt"
	"runtime"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// ReadOnlyQuery runs query against a read-only DuckDB connection to the same
// database file and returns the result columns and rows (capped at maxRows;
// truncated reports whether more rows were available). []byte values are
// converted to strings so they marshal cleanly to JSON.
//
// Read-only is enforced by DuckDB itself on platforms that can open a
// read-only instance alongside the server's writer. Windows cannot do that:
// DuckDB holds an exclusive file lock for its read-write instance, so there we
// reuse the server's connection after rejecting every mutating SQL keyword.
// The guard deliberately tokenizes rather than parsing a different SQL dialect,
// preserving DuckDB-specific SELECT syntax.
//
// Non-Windows platforms require a file-backed database because an in-memory
// database can't be reopened as a second, read-only instance.
func (d *DB) ReadOnlyQuery(ctx context.Context, query string, maxRows int) (cols []string, rows [][]any, truncated bool, err error) {
	return d.ReadOnlyQueryArgs(ctx, query, nil, maxRows)
}

// ReadOnlyQueryArgs is ReadOnlyQuery with bound DuckDB parameters. Query text
// is always checked before execution; values are never interpolated into SQL.
func (d *DB) ReadOnlyQueryArgs(ctx context.Context, query string, args []any, maxRows int) (cols []string, rows [][]any, truncated bool, err error) {
	if maxRows <= 0 {
		maxRows = 1000
	}
	if runtime.GOOS != "windows" && (d.path == "" || d.path == ":memory:") {
		return nil, nil, false, fmt.Errorf("read-only SQL is unavailable: the database is in-memory, not file-backed")
	}
	if err := validateReadOnlySQL(query); err != nil {
		return nil, nil, false, err
	}
	// Internal callers such as catalog discovery can opt out to avoid making
	// their own metadata probes appear as telemetry data. User-authored SQL
	// still gets the explicit query name or a sanitized low-cardinality summary.
	if !skipTracing(ctx) {
		name := queryNameFromContext(ctx)
		if name == "" {
			name = sqlSummary(query)
		}
		attrs := []attribute.KeyValue{
			semconv.DBSystemKey.String("duckdb"),
			attribute.String("db.query.summary", name),
		}
		if sanitized, ok := sanitizeSQL(query); ok {
			attrs = append(attrs, semconv.DBQueryTextKey.String(sanitized))
		}
		var span trace.Span
		ctx, span = otel.Tracer("spaniel/storage").Start(ctx, name,
			trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(attrs...))
		defer span.End()
	}

	var ro *sql.DB
	if runtime.GOOS == "windows" {
		ro = d.SQL()
	} else {
		ro, err = sql.Open("duckdb", duckDBDSN(d.path, true))
		if err != nil {
			return nil, nil, false, fmt.Errorf("open read-only connection: %w", err)
		}
		defer ro.Close() //nolint:errcheck
		ro.SetMaxOpenConns(1)
	}

	rs, err := ro.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, nil, false, err
	}
	defer rs.Close()

	cols, err = rs.Columns()
	if err != nil {
		return nil, nil, false, err
	}

	for rs.Next() {
		if len(rows) >= maxRows {
			truncated = true
			break
		}
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rs.Scan(ptrs...); err != nil {
			return nil, nil, false, err
		}
		for i, v := range vals {
			if b, ok := v.([]byte); ok {
				vals[i] = string(b)
			}
		}
		rows = append(rows, vals)
	}
	if err := rs.Err(); err != nil {
		return nil, nil, false, err
	}
	return cols, rows, truncated, nil
}

// ValidateReadOnlySQL verifies that a statement is suitable for the read-only
// query runner. Execution still occurs on DuckDB's read-only connection.
func ValidateReadOnlySQL(query string) error { return validateReadOnlySQL(query) }

var mutatingSQLKeywords = map[string]struct{}{
	"ALTER": {}, "ANALYZE": {}, "ATTACH": {}, "BEGIN": {}, "CALL": {},
	"CHECKPOINT": {}, "COMMENT": {}, "COMMIT": {}, "COPY": {}, "CREATE": {},
	"DELETE": {}, "DETACH": {}, "DROP": {}, "EXPORT": {}, "IMPORT": {},
	"INSERT": {}, "INSTALL": {}, "LOAD": {}, "MERGE": {}, "PRAGMA": {},
	"REINDEX": {}, "ROLLBACK": {}, "SET": {}, "TRUNCATE": {}, "UPDATE": {},
	"USE": {}, "VACUUM": {},
}

// validateReadOnlySQL accepts SELECT-style statements only. It ignores quoted
// strings, quoted identifiers, and comments so a harmless value such as
// 'DELETE' is not mistaken for a mutation.
func validateReadOnlySQL(query string) error {
	tokens, err := sqlTokens(query)
	if err != nil {
		return fmt.Errorf("read-only SQL: %w", err)
	}
	if len(tokens) == 0 {
		return fmt.Errorf("read-only SQL: query is empty")
	}
	switch tokens[0] {
	case "SELECT", "WITH", "VALUES", "EXPLAIN", "SHOW", "DESCRIBE":
	default:
		return fmt.Errorf("read-only SQL: %s statements are not allowed", strings.ToLower(tokens[0]))
	}
	for _, token := range tokens {
		if _, mutating := mutatingSQLKeywords[token]; mutating {
			return fmt.Errorf("read-only SQL: %s statements are not allowed", strings.ToLower(token))
		}
	}
	return nil
}

func sqlTokens(query string) ([]string, error) {
	var tokens []string
	for i := 0; i < len(query); {
		switch query[i] {
		case ' ', '\t', '\r', '\n', '(', ')', ',':
			i++
		case ';':
			tokens = append(tokens, ";")
			i++
		case '-', '/':
			if i+1 < len(query) && query[i:i+2] == "--" {
				i += 2
				for i < len(query) && query[i] != '\n' {
					i++
				}
			} else if i+1 < len(query) && query[i:i+2] == "/*" {
				end := strings.Index(query[i+2:], "*/")
				if end < 0 {
					return nil, fmt.Errorf("unterminated comment")
				}
				i += end + 4
			} else {
				i++
			}
		case '\'', '"', '`':
			quote := query[i]
			i++
			for i < len(query) {
				if query[i] == quote {
					if i+1 < len(query) && query[i+1] == quote {
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
			if i > len(query) || (i == len(query) && query[i-1] != quote) {
				return nil, fmt.Errorf("unterminated quoted value")
			}
		default:
			start := i
			for i < len(query) && ((query[i] >= 'a' && query[i] <= 'z') || (query[i] >= 'A' && query[i] <= 'Z') || query[i] == '_') {
				i++
			}
			if start == i {
				i++
				continue
			}
			tokens = append(tokens, strings.ToUpper(query[start:i]))
		}
	}
	return tokens, nil
}
