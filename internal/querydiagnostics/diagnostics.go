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
	analyzed := false
	if sql && present && query == "" {
		if _, valid := decode(incoming, attrs); !valid {
			attrs["spaniel.db.query.diagnostics_status"] = "invalid"
		}
	}
	if sql {
		if normalized, valid := decode(incoming, attrs); present && valid {
			results = normalized
		} else if truncated, _ := attrs["gotel.db.query.truncated"].(bool); query != "" && !truncated {
			analysis := sequel.Analyze(query, sequel.AnalysisOptions{Dialect: dialect, OmitSourceMaps: true, OmitBindDescriptors: true})
			analyzed = true
			results = analysis.Diagnostics
			if _, present := attrs["gotel.db.query.fingerprint"]; !present && analysis.Fingerprint != "" {
				attrs["gotel.db.query.fingerprint"] = analysis.Fingerprint
			}
			attrs["gotel.db.query.diagnostic_schema_version"] = sequel.DiagnosticSchemaVersion
			attrs["gotel.db.query.original_length"] = analysis.OriginalLength
			attrs["gotel.db.query.analysis_status"] = analysis.Status
			// These diagnostics refer to the available transported text, not to
			// an unknown pre-sanitization original. Keep provenance explicit.
			attrs["spaniel.db.query.diagnostics_source"] = "display"
			// Existing producer maps refer to a different original. They cannot
			// accompany a new analysis of its transported display text.
			delete(attrs, "gotel.db.query.source_map")
		}
	}
	if sql && query != "" && !analyzed {
		if _, ok := attrs["gotel.db.query.fingerprint"]; !ok {
			if fp, err := sequel.Fingerprint(dialect, query); err == nil && fp != "" {
				attrs["gotel.db.query.fingerprint"] = fp
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

func decode(value any, metadata ...map[string]any) ([]sequel.Diagnostic, bool) {
	version, sourceLength := sequel.DiagnosticSchemaVersion, sequel.MaxInputBytes
	if len(metadata) > 0 {
		var valid bool
		version, sourceLength, valid = diagnosticMetadata(metadata[0])
		if !valid {
			return nil, false
		}
	}

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
	case []map[string]any:
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
		canonical, err := sequel.CanonicalizeDiagnostic(version, sourceLength, d)
		if err != nil {
			return nil, false
		}
		d = canonical
		out = append(out, d)
	}
	return out, true
}
