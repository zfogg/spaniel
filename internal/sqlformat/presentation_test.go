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
	if !strings.Contains(out["query"].(string), "\n") || out["draft"] != "select 'unfinished" {
		t.Fatal(string(data))
	}
	if raw["query"] != "select a,b from spans" {
		t.Fatal("mutated raw input")
	}
}
