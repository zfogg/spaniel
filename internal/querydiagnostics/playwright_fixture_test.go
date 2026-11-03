package querydiagnostics

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sequel "github.com/zfogg/sequel/go"
)

// The browser consumes the exact backend-computed contract fixtures. Regenerate
// deliberately with SPANIEL_UPDATE_SQL_FIXTURE=1, then normal tests detect drift.
func TestDisplayPlaywrightContractFixture(t *testing.T) {
	type fixture struct {
		Input  map[string]any  `json:"input"`
		Output DisplayAnalysis `json:"output"`
	}
	fixtures := map[string]fixture{}
	add := func(name, query string, attrs map[string]any, original bool) {
		t.Helper()
		out, err := Display(context.Background(), query, sequel.DialectPostgreSQL, original, attrs)
		if err != nil {
			t.Fatal(err)
		}
		source := "telemetry"
		if original {
			source = "original"
		}
		input := map[string]any{"sql": query, "source": source, "dialect": "postgresql"}
		if attrs != nil {
			input["diagnostics"] = attrs[Attribute]
			for field, key := range map[string]string{"source_map": "gotel.db.query.source_map", "original_length": "gotel.db.query.original_length", "diagnostic_schema_version": "gotel.db.query.diagnostic_schema_version"} {
				if v, ok := attrs[key]; ok {
					input[field] = v
				}
			}
		}
		fixtures[name] = fixture{input, out}
	}
	original := "SELECT 'private-fixture-literal', \"😀é\"\r\nFROM things WHERE id IN (1) OR id IN (2)"
	diagnostics := []sequel.Diagnostic{}
	for at := 0; at < len(original); at++ {
		if original[at] == ')' {
			diagnostics = append(diagnostics, producerDiagnostic(original, at, at+1, "unmatched_close"))
		}
	}
	text, attrs := producerAttributes(original, true, diagnostics)
	add("mapped", text, attrs, false)
	delete(attrs, "gotel.db.query.source_map")
	add("unavailable", text, attrs, false)
	at := strings.Index(original, "private-fixture-literal")
	text, attrs = producerAttributes(original, true, []sequel.Diagnostic{producerDiagnostic(original, at, at+len("private-fixture-literal"), "unterminated_string")})
	add("approximate", text, attrs, false)
	d := sequel.Diagnose(sequel.DialectPostgreSQL, "SELECT 'private-fixture-literal")
	add("omitted", "", map[string]any{Attribute: []sequel.Diagnostic{d}, "gotel.db.query.original_length": len("SELECT 'private-fixture-literal"), "gotel.db.query.diagnostic_schema_version": 1}, false)
	add("editor", "SELECT \"😀é\",\n(1", nil, true)
	data, err := json.MarshalIndent(fixtures, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if strings.Contains(string(data), "private-fixture-literal") {
		t.Fatal("browser fixture retained original literal")
	}
	path := filepath.Join("..", "..", "frontend", "e2e", "fixtures", "query-analysis.json")
	if os.Getenv("SPANIEL_UPDATE_SQL_FIXTURE") == "1" {
		if err = os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("generate SQL browser contract fixture:", err)
	}
	// Frontend formatting may indent JSON differently; compare its structure.
	var left, right any
	if json.Unmarshal(want, &left) != nil || json.Unmarshal(data, &right) != nil {
		t.Fatal("invalid browser fixture")
	}
	l, _ := json.Marshal(left)
	r, _ := json.Marshal(right)
	if string(l) != string(r) {
		t.Fatal("SQL browser contract fixture drift; regenerate deliberately")
	}
}
