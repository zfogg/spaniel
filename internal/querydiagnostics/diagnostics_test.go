package querydiagnostics

import (
	"encoding/json"
	"strings"
	"testing"

	sequel "github.com/zfogg/sequel/go"
)

func TestNormalizeSQL(t *testing.T) {
	for _, tc := range []struct{ system, query, code string }{
		{"postgresql", "SELECT 1", ""},
		{"postgresql", "SELECT 'private-value", "unterminated_string"},
		{"postgres", "SELECT $tag$private-value", "unterminated_string"},
		{"mysql", "SELECT 1 # '", ""},
		{"mariadb", "SELECT `private-value", "unterminated_identifier"},
		{"sqlite3", "SELECT [private-value", "unterminated_identifier"},
		{"duckdb", "SELECT (1", "unclosed_group"},
		{"clickhouse", "SELECT {id:UInt64", "unterminated_parameter"},
		{"clickhouse", "SELECT {id:}", "invalid_parameter"},
		{"", "SELECT 1)", "unmatched_close"},
		{"postgresql", "SELECT /* private-value", "unterminated_comment"},
		{"postgresql", "SELECT FROM", ""}, // Lexical checks are not grammar validation.
		{"postgresql", "", ""},
		{"redis", "SET key 'private-value", ""},
		{"mongodb", "{private-value", ""},
	} {
		t.Run(tc.system+tc.query, func(t *testing.T) {
			attrs := map[string]any{"db.system.name": tc.system, "db.query.text": tc.query}
			Normalize(attrs)
			results, ok := attrs[Attribute].([]sequel.Diagnostic)
			if !ok || results == nil {
				t.Fatalf("not a results array: %#v", attrs)
			}
			if tc.code == "" {
				if len(results) != 0 {
					t.Fatalf("unexpected issues: %+v", results)
				}
			} else if len(results) != 1 || results[0].Code != tc.code {
				t.Fatalf("want %s, got %+v", tc.code, results)
			}
			data, _ := json.Marshal(results)
			if strings.Contains(string(data), "private-value") {
				t.Fatal("SQL secret in diagnostics")
			}
		})
	}
	attrs := map[string]any{"http.route": "/"}
	Normalize(attrs)
	if _, ok := attrs[Attribute]; ok {
		t.Fatal("non-DB span received diagnostics")
	}
	attrs = map[string]any{"db.system": "postgresql", "db.statement": "SELECT '"}
	Normalize(attrs)
	if len(attrs[Attribute].([]sequel.Diagnostic)) != 1 {
		t.Fatal("legacy attributes not analyzed")
	}
}

func TestNormalizeInstrumented(t *testing.T) {
	d := sequel.Diagnose(sequel.DialectPostgreSQL, "SELECT 'private-value")
	d.Message, d.Explanation, d.Hint = "private-value", "private-value", "private-value"
	encoded, _ := json.Marshal(d)
	for _, value := range []any{[]string{string(encoded)}, []sequel.Diagnostic{d}} {
		attrs := map[string]any{"db.system.name": "postgresql", Attribute: value}
		Normalize(attrs)
		results := attrs[Attribute].([]sequel.Diagnostic)
		if len(results) != 1 || results[0].Offset != 7 || results[0].EndOffset != 21 {
			t.Fatalf("lost original ranges: %+v", results)
		}
		data, _ := json.Marshal(results)
		if strings.Contains(string(data), "private-value") {
			t.Fatal("untrusted prose persisted")
		}
	}
	// Empty SDK arrays are authoritative even when the emitted template is cut
	// short: ranges from sanitized/truncated SQL must not replace originals.
	attrs := map[string]any{"db.system.name": "postgresql", "db.query.text": "SELECT (", Attribute: []string{}}
	Normalize(attrs)
	if len(attrs[Attribute].([]sequel.Diagnostic)) != 0 {
		t.Fatal("ignored authoritative empty array")
	}
	for _, incoming := range []any{nil, []string{"invalid"}} {
		attrs = map[string]any{"db.system.name": "postgresql", "db.query.text": "SELECT (", "gotel.db.query.truncated": true, Attribute: incoming}
		Normalize(attrs)
		if len(attrs[Attribute].([]sequel.Diagnostic)) != 0 {
			t.Fatal("analyzed a truncated template")
		}
	}
	attrs = map[string]any{"db.system.name": "postgresql", "db.query.text": "SELECT (", "gotel.db.query.truncated": true, Attribute: []string{string(encoded)}}
	Normalize(attrs)
	if results := attrs[Attribute].([]sequel.Diagnostic); len(results) != 1 || results[0].Code != d.Code || results[0].EndOffset != d.EndOffset {
		t.Fatal("lost original diagnostics on a truncated template")
	}
	attrs = map[string]any{"db.system.name": "redis", Attribute: []string{string(encoded)}}
	Normalize(attrs)
	if len(attrs[Attribute].([]sequel.Diagnostic)) != 0 {
		t.Fatal("SQL diagnostics on Redis")
	}
}

func TestNormalizeInvalidAttributes(t *testing.T) {
	d := sequel.Diagnose(sequel.DialectGeneric, "SELECT '")
	encoded, _ := json.Marshal(d)
	var object map[string]any
	_ = json.Unmarshal(encoded, &object)
	invalid := []any{nil, "[]", "secret", 12, map[string]any{}, []string{"null"}, []string{"{}"}, []string{"{broken"}, []any{false}, []string{strings.Repeat("x", 17000)}, make([]string, 65)}
	for key, value := range map[string]any{"code": "secret", "severity": "note", "offset": -1, "end_offset": -1, "line": 0, "column": nil, "end_line": 0, "incomplete": false, "statement_complete": true, "extra": "secret"} {
		copy := map[string]any{}
		for k, v := range object {
			copy[k] = v
		}
		copy[key] = value
		invalid = append(invalid, []any{copy})
	}
	for _, value := range invalid {
		attrs := map[string]any{"db.system.name": "duckdb", "db.query.text": "SELECT (", Attribute: value}
		Normalize(attrs)
		results := attrs[Attribute].([]sequel.Diagnostic)
		if len(results) != 1 || results[0].Code != "unclosed_group" {
			t.Fatalf("invalid %#v did not fall back: %+v", value, results)
		}
		delete(attrs, "db.query.text")
		attrs[Attribute] = value
		Normalize(attrs)
		if len(attrs[Attribute].([]sequel.Diagnostic)) != 0 {
			t.Fatalf("invalid %#v persisted without SQL", value)
		}
	}
}
