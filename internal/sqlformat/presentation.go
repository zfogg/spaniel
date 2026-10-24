// Package sqlformat owns SQL presentation, separate from stored/executed SQL.
package sqlformat

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/zfogg/gotel/sqltrace"
)

func Format(query string) string { return sqltrace.FormatSQLOrOriginal(sqltrace.DialectDuckDB, query) }

// TelemetryQuery uses the database identity supplied with a span's attributes.
func TelemetryQuery(attributes map[string]any, query string) string {
	for _, key := range []string{"db.system.name", "db.system"} {
		if system, ok := attributes[key].(string); ok {
			dialect := sqltrace.Dialect(system)
			switch dialect {
			case sqltrace.DialectPostgreSQL, sqltrace.DialectDuckDB, sqltrace.DialectSQLite, sqltrace.DialectMySQL:
				return sqltrace.FormatSQLOrOriginal(dialect, query)
			}
		}
	}
	return query
}

// Presentation returns an independent wire representation. It never mutates
// storage models or editor inputs. UseNumber preserves nanosecond timestamps
// and other integers beyond JavaScript's exact range, including MCP TOON.
func Presentation(value any) any {
	data, err := json.Marshal(value)
	if err != nil {
		return value
	}
	if !bytes.Contains(data, []byte("query")) && !bytes.Contains(data, []byte("db.statement")) && !bytes.Contains(data, []byte("discovery_sql")) {
		return value
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var out any
	if decoder.Decode(&out) != nil {
		return value
	}
	walk(out)
	return out
}

func walk(value any) {
	switch v := value.(type) {
	case []any:
		for i, item := range v {
			item = integerValue(item)
			v[i] = item
			walk(item)
		}
	case map[string]any:
		// Only telemetry SQL columns have a defined SQL contract in arbitrary
		// query results. Other cells (including "query") are user data.
		if columns, ok := v["columns"].([]any); ok {
			if rows, ok := v["rows"].([]any); ok {
				for _, row := range rows {
					cells, ok := row.([]any)
					if !ok {
						continue
					}
					attributes := make(map[string]any, len(columns))
					for i, column := range columns {
						if name, ok := column.(string); ok && i < len(cells) {
							attributes[name] = cells[i]
						}
					}
					for i, column := range columns {
						name, ok := column.(string)
						if !ok || i >= len(cells) {
							continue
						}
						if query, ok := cells[i].(string); ok && (name == "db.query.text" || name == "db.statement") {
							cells[i] = TelemetryQuery(attributes, query)
						} else if name == "attributes" || name == "attributes_json" {
							field := map[string]any{name: cells[i]}
							walk(field)
							cells[i] = field[name]
						}
					}
				}
			}
		}
		for key, item := range v {
			item = integerValue(item)
			v[key] = item
			if query, ok := item.(string); ok {
				switch key {
				case "query_sql", "instance_discovery_sql", "annotation_query":
					v[key] = Format(query)
				case "db.query.text", "db.statement":
					v[key] = TelemetryQuery(v, query)
				case "attributes", "attributes_json", "settings_json":
					if strings.Contains(query, "db.query.text") || strings.Contains(query, "db.statement") || (key == "settings_json" && strings.Contains(query, "annotation_query")) {
						decoder := json.NewDecoder(strings.NewReader(query))
						decoder.UseNumber()
						var attrs any
						if decoder.Decode(&attrs) == nil {
							walk(attrs)
							if data, err := json.Marshal(attrs); err == nil {
								v[key] = string(data)
							}
						}
					}
				}
			} else {
				walk(item)
			}
		}
	}
}

// TOON distinguishes native integers from json.Number: native integers beyond
// the safe JavaScript range are quoted, allowing clients to round-trip them.
func integerValue(value any) any {
	if number, ok := value.(json.Number); ok {
		if signed, err := number.Int64(); err == nil {
			return signed
		}
		if unsigned, err := strconv.ParseUint(number.String(), 10, 64); err == nil {
			return unsigned
		}
	}
	return value
}
