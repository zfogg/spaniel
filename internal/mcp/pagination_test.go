package mcp

import (
	"github.com/zfogg/spaniel/internal/storage"
	"testing"
)

func TestTelemetryPaginationReportsTotalAndHasMore(t *testing.T) {
	cs, db := connect(t, Options{})
	session := db.ActiveSessionID()
	seedTrace(t, db, session)
	if err := db.InsertSpan(&storage.Span{TraceID: "second", SpanID: "second", ServiceName: "api", Name: "second", StartNs: 3000000, EndNs: 4000000, SessionID: session, ReceivedAt: 1}); err != nil {
		t.Fatal(err)
	}
	if err := db.InsertLog(&storage.Log{TimestampNs: 3000000, Body: "second", SessionID: session, ReceivedAt: 1}); err != nil {
		t.Fatal(err)
	}
	traces := callStructured[ListTracesOutput](t, cs, "list_traces", map[string]any{"limit": 1})
	if traces.Total != 2 || traces.Page != 1 || traces.Limit != 1 || !traces.HasMore {
		t.Fatalf("first traces: %#v", traces)
	}
	traces = callStructured[ListTracesOutput](t, cs, "list_traces", map[string]any{"limit": 1, "page": 2})
	if traces.Total != 2 || traces.HasMore || traces.Count != 1 {
		t.Fatalf("last traces: %#v", traces)
	}
	logs := callStructured[QueryLogsOutput](t, cs, "query_logs", map[string]any{"limit": 1})
	if logs.Total != 2 || !logs.HasMore {
		t.Fatalf("first logs: %#v", logs)
	}
	logs = callStructured[QueryLogsOutput](t, cs, "query_logs", map[string]any{"limit": 1, "page": 2})
	if logs.Total != 2 || logs.HasMore || logs.Count != 1 {
		t.Fatalf("last logs: %#v", logs)
	}
}
