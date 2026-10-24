package sqlformat

import (
	"encoding/json"
	"strings"
	"testing"

	toongo "github.com/toon-format/toon-go"
)

func TestPresentationPreservesSourceAndExactIntegers(t *testing.T) {
	const query = "-- keep\nselect a,b from spans where trace_id=$trace_id; -- trailing  "
	input := struct {
		Query      string `json:"query_sql"`
		At         int64  `json:"at"`
		Attributes string `json:"attributes"`
	}{query, 1789987654321098765, `{"db.query.text":"select a,b from spans where id=?","count":1789987654321098765}`}
	out := Presentation(input)
	if input.Query != query || strings.Contains(input.Attributes, "\\n") {
		t.Fatal("source mutated")
	}
	data, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "1789987654321098765") || !strings.Contains(string(data), "select\\n") || !strings.Contains(string(data), "$trace_id") || !strings.Contains(string(data), "-- trailing  ") {
		t.Fatal(string(data))
	}
	toon, err := toongo.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(toon), "1789987654321098765") || !strings.Contains(string(toon), "$trace_id") {
		t.Fatal(string(toon))
	}
	decoded, err := toongo.DecodeString(string(toon))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.(map[string]any)["at"] != "1789987654321098765" {
		t.Fatalf("TOON timestamp lost precision: %#v", decoded)
	}
}

func TestPresentationFormatsAllQueryFieldsAndToleratesDrafts(t *testing.T) {
	raw := map[string]any{"panels": []any{map[string]any{"query_sql": "select a,b from spans"}}, "instance_discovery_sql": "select distinct service_name from spans", "query": "select a,b from spans", "attributes": map[string]any{"db.query.text": "select a,b from t"}, "draft": "select 'unfinished"}
	data, err := json.Marshal(Presentation(raw))
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err = json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out["query"] != raw["query"] || out["draft"] != "select 'unfinished" {
		t.Fatal(string(data))
	}
	if raw["query"] != "select a,b from spans" {
		t.Fatal("mutated raw input")
	}
}

func TestPresentationFormatsNamedQueryResultColumns(t *testing.T) {
	input := map[string]any{"columns": []string{"db.query.text", "source", "count", "db.system.name", "query"}, "rows": [][]any{{"select a,b from t where id=$1", "select is a source name", int64(1789987654321098765), "postgresql", "select worker"}}}
	out := Presentation(input).(map[string]any)
	row := out["rows"].([]any)[0].([]any)
	if row[0] != "select\n  a,\n  b\nfrom t\nwhere\n  id = $1" || row[1] != "select is a source name" || row[2] != int64(1789987654321098765) || row[4] != "select worker" {
		t.Fatalf("unexpected row: %#v", row)
	}
}

func TestPresentationUsesTelemetryDatabaseDialect(t *testing.T) {
	input := map[string]any{"attributes": map[string]any{"db.system.name": "mysql", "db.query.text": "select id,name # keep SELECT and WHERE opaque\nfrom accounts where id=?"}}
	out := Presentation(input).(map[string]any)["attributes"].(map[string]any)["db.query.text"].(string)
	if out != "select\n  id,\n  name # keep SELECT and WHERE opaque\nfrom accounts\nwhere\n  id = ?" {
		t.Fatal(out)
	}
}

func TestTelemetryPreservesMySQLLiteralAndUnknownDialect(t *testing.T) {
	const query = `select 'it\'s select id,name from t \' ok'`
	for _, attributes := range []map[string]any{
		{}, {"db.system.name": "unknown"}, {"db.system": "generic"},
	} {
		if got := TelemetryQuery(attributes, query); got != query {
			t.Fatalf("unknown dialect changed literal: %q", got)
		}
	}
	for _, key := range []string{"db.system.name", "db.system"} {
		if got := TelemetryQuery(map[string]any{key: "mysql"}, query); got != "select\n  "+strings.TrimPrefix(query, "select ") {
			t.Fatalf("MySQL literal changed: %q", got)
		}
	}
}

func TestPresentationPreservesSearchTextAndArbitraryResultCells(t *testing.T) {
	input := map[string]any{
		"query":   "select worker",
		"columns": []string{"query", "query_sql", "db.query.text"},
		"rows":    [][]any{{"select worker", "select user data", `select 'unknown dialect'`}},
	}
	before, _ := json.Marshal(input)
	after, _ := json.Marshal(Presentation(input))
	if string(before) != string(after) {
		t.Fatalf("non-SQL data changed: %s", after)
	}
}
