// Package querydiagnostics normalizes SQL diagnostics at the telemetry boundary.
package querydiagnostics

import (
	"bytes"
	"encoding/json"
	"strings"

	sequel "github.com/zfogg/sequel/go"
)

const Attribute = "db.query.diagnostics"

// Normalize preserves instrumented diagnostics, whose ranges refer to original
// SQL even when the exporter has omitted or sanitized db.query.text. Invalid
// attributes fall back to analysis of available SQL. Redis and other non-SQL
// databases receive an empty results array and are never parsed as SQL.
func Normalize(attrs map[string]any) {
	system := stringAttr(attrs, "db.system.name")
	if system == "" {
		system = stringAttr(attrs, "db.system")
	}
	query := stringAttr(attrs, "db.query.text")
	if query == "" {
		query = stringAttr(attrs, "db.statement")
	}
	incoming, present := attrs[Attribute]
	if system == "" && query == "" && !present {
		database := false
		for key := range attrs {
			database = database || strings.HasPrefix(key, "db.")
		}
		if !database {
			return
		}
	}
	dialect, sql := sqlDialect(system)
	results := []sequel.Diagnostic{}
	if sql {
		if normalized, valid := decode(incoming); present && valid {
			results = normalized
		} else if truncated, _ := attrs["gotel.db.query.truncated"].(bool); query != "" && !truncated {
			if diagnostic := sequel.Diagnose(dialect, query); diagnostic.Code != "none" {
				results = append(results, diagnostic)
			}
		}
	}
	attrs[Attribute] = results
}

func stringAttr(attrs map[string]any, key string) string {
	value, _ := attrs[key].(string)
	return value
}

func sqlDialect(system string) (sequel.Dialect, bool) {
	switch strings.ToLower(strings.TrimSpace(system)) {
	case "", "other_sql", "sql":
		return sequel.DialectGeneric, true
	case "postgres", "postgresql":
		return sequel.DialectPostgreSQL, true
	case "sqlite", "sqlite3":
		return sequel.DialectSQLite, true
	case "mysql", "mariadb":
		return sequel.DialectMySQL, true
	case "duckdb":
		return sequel.DialectDuckDB, true
	case "clickhouse":
		return sequel.DialectClickHouse, true
	case "mssql", "microsoft.sql_server", "oracle", "db2", "h2", "hsqldb", "snowflake", "trino", "presto", "cockroachdb":
		return sequel.DialectGeneric, true
	default:
		return "", false
	}
}

// Canonical descriptions come from trusted, literal-free probes of Sequel.
// Incoming prose is never persisted: even a valid-looking Diagnostic can carry
// SQL literals, bind values or arbitrary injected text in its message fields.
var descriptions = func() map[string]sequel.Diagnostic {
	out := map[string]sequel.Diagnostic{}
	for _, probe := range []struct {
		dialect sequel.Dialect
		query   string
	}{
		{sequel.DialectGeneric, "SELECT '"},
		{sequel.DialectGeneric, "SELECT \""},
		{sequel.DialectGeneric, "/*"},
		{sequel.DialectClickHouse, "SELECT {name:"},
		{sequel.DialectClickHouse, "SELECT {name:}"},
		{sequel.DialectGeneric, ")"},
		{sequel.DialectGeneric, "("},
		{sequel.DialectGeneric, strings.Repeat("(", 65)},
		{sequel.DialectGeneric, "\xff"},
		{sequel.DialectGeneric, "\x00"},
		{sequel.DialectClickHouse, "INSERT INTO t FORMAT CSV\n1"},
	} {
		d := sequel.Diagnose(probe.dialect, probe.query)
		if d.Code != "none" {
			out[d.Code] = d
		}
	}
	// Resource/API failures cannot be reproduced cheaply at initialization.
	for _, code := range []string{"null_input", "invalid_argument", "input_limit", "token_limit", "unsupported_layout", "output_limit", "out_of_memory"} {
		out[code] = sequel.Diagnostic{Code: code, Severity: "error", Message: "SQL analysis could not complete", Explanation: "SQL analysis encountered an input, resource or API limit.", Hint: "Check the diagnostic code and the SQL analysis limits."}
	}
	return out
}()

func decode(value any) ([]sequel.Diagnostic, bool) {
	// OTLP SDKs send []string; native OTLP can also carry structured maps.
	switch values := value.(type) {
	case []string:
		if len(values) > 64 {
			return nil, false
		}
		for _, entry := range values {
			if len(entry) > 16384 {
				return nil, false
			}
		}
	case []any:
		if len(values) > 64 {
			return nil, false
		}
	case []sequel.Diagnostic:
		if len(values) > 64 {
			return nil, false
		}
	default:
		return nil, false
	}
	data, err := json.Marshal(value)
	if err != nil || len(data) > 64*16384 || len(data) == 0 || data[0] != '[' {
		return nil, false
	}
	var entries []json.RawMessage
	if json.Unmarshal(data, &entries) != nil || len(entries) > 64 {
		return nil, false
	}
	out := make([]sequel.Diagnostic, 0, len(entries))
	for _, entry := range entries {
		var encoded string
		if json.Unmarshal(entry, &encoded) == nil {
			entry = []byte(encoded)
		}
		if len(entry) > 16384 || len(entry) == 0 || entry[0] != '{' {
			return nil, false
		}
		// Require all fields with their proper JSON types, including booleans
		// and numeric ranges; null and partial objects are not diagnostics.
		var fields map[string]json.RawMessage
		if json.Unmarshal(entry, &fields) != nil || len(fields) != 13 {
			return nil, false
		}
		for _, key := range []string{"code", "severity", "message", "explanation", "hint", "offset", "end_offset", "line", "column", "end_line", "end_column", "incomplete", "statement_complete"} {
			if raw, ok := fields[key]; !ok || bytes.Equal(raw, []byte("null")) {
				return nil, false
			}
		}
		var d sequel.Diagnostic
		decoder := json.NewDecoder(bytes.NewReader(entry))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&d) != nil {
			return nil, false
		}
		canonical, known := descriptions[d.Code]
		if !known || d.Severity != canonical.Severity || d.Incomplete != canonical.Incomplete || d.StatementComplete ||
			d.Offset < 0 || d.EndOffset < d.Offset || d.EndOffset > sequel.MaxInputBytes ||
			d.Line < 1 || d.Column < 1 || d.EndLine < d.Line || d.EndColumn < 1 ||
			d.Line > sequel.MaxInputBytes+1 || d.EndLine > sequel.MaxInputBytes+1 || d.Column > sequel.MaxInputBytes+1 || d.EndColumn > sequel.MaxInputBytes+1 ||
			(d.EndLine == d.Line && d.EndColumn < d.Column) {
			return nil, false
		}
		d.Message, d.Explanation, d.Hint = canonical.Message, canonical.Explanation, canonical.Hint
		out = append(out, d)
	}
	return out, true
}
