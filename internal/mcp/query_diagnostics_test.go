package mcp

import (
	"context"
	"testing"

	"github.com/zfogg/spaniel/internal/ingestion"
	"github.com/zfogg/spaniel/internal/ws"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

func TestQueryDiagnosticsMCPStructuredResponses(t *testing.T) {
	cs, store := connect(t, Options{Version: "t"})
	p := ingestion.NewPipeline(store, ws.NewHub())
	t.Cleanup(func() { _ = p.Close(context.Background()) })
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	rs.Resource().Attributes().PutStr("service.name", "diagnostics-mcp-test")
	ss := rs.ScopeSpans().AppendEmpty()
	for i, query := range []string{"SELECT (", "SELECT 1"} {
		span := ss.Spans().AppendEmpty()
		span.SetTraceID(pcommon.TraceID{1})
		span.SetSpanID(pcommon.SpanID{byte(i + 1)})
		span.SetName("query")
		span.Attributes().PutStr("db.system.name", "duckdb")
		span.Attributes().PutStr("db.query.text", query)
	}
	if err := p.IngestTraces(context.Background(), td); err != nil {
		t.Fatal(err)
	}
	traceID := ss.Spans().At(0).TraceID().String()
	trace := callStructured[GetTraceOutput](t, cs, "get_trace", map[string]any{"trace_id": traceID})
	if len(trace.Spans) != 2 {
		t.Fatalf("unexpected trace: %+v", trace)
	}
	for _, span := range trace.Spans {
		detail := callStructured[GetSpanOutput](t, cs, "get_span", map[string]any{"span_id": span.SpanID})
		for _, attrs := range []map[string]any{span.Attributes, detail.Attributes} {
			results, ok := attrs["db.query.diagnostics"].([]any)
			if !ok {
				t.Fatalf("not an array: %#v", attrs)
			}
			if span.SpanID == ss.Spans().At(0).SpanID().String() {
				if len(results) != 1 {
					t.Fatalf("missing diagnostic: %#v", attrs)
				}
				object, ok := results[0].(map[string]any)
				if !ok || object["code"] != "unclosed_group" {
					t.Fatalf("not structured: %#v", results)
				}
			} else if len(results) != 0 {
				t.Fatalf("clean diagnostics: %#v", results)
			}
		}
	}
}
