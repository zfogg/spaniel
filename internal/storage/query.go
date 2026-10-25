package storage

import (
	"context"
	"fmt"
	"strings"

	"github.com/zfogg/gotel/go/sqltrace"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

// ReadOnlyQuery runs validated read-only SQL on the server's bounded DuckDB
// pool and returns columns and rows capped at maxRows. []byte values become
// strings for JSON. DuckDB cannot reopen an active writer with a different
// access_mode or configuration, on any platform. The SQL gate rejects mutations
// before execution; the shared instance disables external access and extensions.
func (d *DB) ReadOnlyQuery(ctx context.Context, query string, maxRows int) (cols []string, rows [][]any, truncated bool, err error) {
	return d.ReadOnlyQueryArgs(ctx, query, nil, maxRows)
}

// ReadOnlyQueryArgs is ReadOnlyQuery with bound DuckDB parameters. Query text
// is always checked before execution; values are never interpolated into SQL.
func (d *DB) ReadOnlyQueryArgs(ctx context.Context, query string, args []any, maxRows int) (cols []string, rows [][]any, truncated bool, err error) {
	if err := validateReadOnlySQL(query); err != nil {
		return nil, nil, false, err
	}
	return d.querySQLArgs(ctx, query, args, maxRows)
}

// WriteEnabledQuery is reserved for MCP's explicit SQL write opt-in.
// Dashboard and alert callers must continue using ReadOnlyQuery.
func (d *DB) WriteEnabledQuery(ctx context.Context, query string, maxRows int) ([]string, [][]any, bool, error) {
	return d.querySQLArgs(ctx, query, nil, maxRows)
}

func (d *DB) querySQLArgs(ctx context.Context, query string, args []any, maxRows int) (cols []string, rows [][]any, truncated bool, err error) {
	if maxRows <= 0 {
		maxRows = 1000
	}

	// Internal callers such as catalog discovery can opt out through
	// WithoutTracing. Explicit WithQueryName contexts win; otherwise the stable
	// ReadOnlyQuery fallback is used for user-authored dashboard and alert SQL.
	rs, err := sqltrace.QueryContext(ctx, d.SQL(), sqltrace.SpanConfig{
		Tracer: otel.Tracer("spaniel/storage"), Namer: storageNamer, Name: "ReadOnlyQuery",
		Dialect: sqltrace.DialectDuckDB, System: "duckdb", Namespace: "spaniel",
		Kind: trace.SpanKindInternal, Observe: storageObserver,
	}, query, args...)
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
// query runner. The same gate protects the shared writer pool on every platform.
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
