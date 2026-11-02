package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sequel "github.com/zfogg/sequel/go"
	"github.com/zfogg/spaniel/internal/ingestion"
	"github.com/zfogg/spaniel/internal/ws"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"
)

func TestQueryDiagnosticsAPIAndOTLPExport(t *testing.T) {
	handler, store := setupRouter(t)
	p := ingestion.NewPipeline(store, ws.NewHub())
	t.Cleanup(func() { _ = p.Close(context.Background()) })
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	rs.Resource().Attributes().PutStr("service.name", "diagnostics-api-test")
	span := rs.ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	span.SetTraceID(pcommon.TraceID{1})
	span.SetSpanID(pcommon.SpanID{2})
	span.SetName("original-query-omitted")
	span.SetStartTimestamp(pcommon.Timestamp(time.Now().UnixNano()))
	span.SetEndTimestamp(span.StartTimestamp() + 1000)
	span.Attributes().PutStr("db.system.name", "postgresql")
	d := sequel.Diagnose(sequel.DialectPostgreSQL, "SELECT 'private-value")
	encoded, _ := json.Marshal(d)
	span.Attributes().PutEmptySlice("db.query.diagnostics").AppendEmpty().SetStr(string(encoded))
	if err := p.IngestTraces(context.Background(), td); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/spans/" + span.SpanID().String(), "/api/traces/" + span.TraceID().String()} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		var body struct {
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		var rows []struct {
			Attributes string `json:"attributes"`
		}
		if path == "/api/spans/"+span.SpanID().String() {
			var row struct {
				Attributes string `json:"attributes"`
			}
			if err := json.Unmarshal(body.Data, &row); err != nil {
				t.Fatal(err)
			}
			rows = append(rows, row)
		} else {
			if err := json.Unmarshal(body.Data, &rows); err != nil {
				t.Fatal(err)
			}
		}
		if len(rows) != 1 {
			t.Fatalf("%s: unexpected response: %s", path, w.Body.String())
		}
		var attrs map[string]json.RawMessage
		_ = json.Unmarshal([]byte(rows[0].Attributes), &attrs)
		var results []sequel.Diagnostic
		if err := json.Unmarshal(attrs["db.query.diagnostics"], &results); err != nil || len(results) != 1 || results[0].Code != d.Code {
			t.Fatalf("%s: %s (%v)", path, rows[0].Attributes, err)
		}
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/traces/"+span.TraceID().String()+"/export", nil))
	export := ptraceotlp.NewExportRequest()
	if w.Code != 200 || export.UnmarshalJSON(w.Body.Bytes()) != nil {
		t.Fatalf("export: %d %s", w.Code, w.Body.String())
	}
	attrs := export.Traces().ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(0).Attributes()
	results, ok := attrs.Get("db.query.diagnostics")
	if !ok || results.Type() != pcommon.ValueTypeSlice || results.Slice().Len() != 1 || results.Slice().At(0).Type() != pcommon.ValueTypeMap {
		t.Fatalf("OTLP diagnostics are not structured: %v", attrs.AsRaw())
	}
}
