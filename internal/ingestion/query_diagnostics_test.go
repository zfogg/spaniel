package ingestion

import (
	"context"
	"encoding/json"
	"testing"

	sequel "github.com/zfogg/sequel/go"
	"github.com/zfogg/spaniel/internal/ws"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

func TestQueryDiagnosticsPersistedForExternalAndSelfTelemetry(t *testing.T) {
	for _, self := range []bool{false, true} {
		t.Run(map[bool]string{false: "external", true: "self"}[self], func(t *testing.T) {
			store := openTestDB(t)
			session, err := store.CreateSession("diagnostics", false)
			if err != nil {
				t.Fatal(err)
			}
			store.SetActiveSession(session.ID, session.Label)
			p := NewPipeline(store, ws.NewHub())
			t.Cleanup(func() { _ = p.Close(context.Background()) })
			if self {
				p.SetSelfService("diagnostics-test")
			}
			d := sequel.Diagnose(sequel.DialectPostgreSQL, "SELECT 'private-value")
			encoded, _ := json.Marshal(d)
			td := makeTraces("diagnostics-test", func(ss ptrace.ScopeSpans) {
				putSpan(ss, "d", "1", "", "original-query", ptrace.SpanKindClient, map[string]string{"db.system.name": "postgresql"})
				ss.Spans().At(0).Attributes().PutEmptySlice("db.query.diagnostics").AppendEmpty().SetStr(string(encoded))
				putSpan(ss, "d", "2", "", "fallback-query", ptrace.SpanKindClient, map[string]string{"db.system": "duckdb", "db.statement": "SELECT ("})
				putSpan(ss, "d", "3", "", "clean-query", ptrace.SpanKindClient, map[string]string{"db.system.name": "mysql", "db.query.text": "SELECT 1"})
			})
			if err := p.IngestTraces(context.Background(), td); err != nil {
				t.Fatal(err)
			}
			spans, err := store.GetTrace(td.ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(0).TraceID().String())
			if err != nil || len(spans) != 3 {
				t.Fatalf("spans: %d, %v", len(spans), err)
			}
			for _, span := range spans {
				var attrs map[string]json.RawMessage
				if err := json.Unmarshal([]byte(span.Attributes), &attrs); err != nil {
					t.Fatal(err)
				}
				var results []sequel.Diagnostic
				if err := json.Unmarshal(attrs["db.query.diagnostics"], &results); err != nil || results == nil {
					t.Fatalf("not a structured array: %s (%v)", span.Attributes, err)
				}
				want := map[string]string{"original-query": "unterminated_string", "fallback-query": "unclosed_group", "clean-query": ""}[span.Name]
				if want == "" && len(results) != 0 || want != "" && (len(results) != 1 || results[0].Code != want) {
					t.Fatalf("%s: %+v", span.Name, results)
				}
			}
		})
	}
}
