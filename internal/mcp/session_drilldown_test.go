package mcp

import (
	"context"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zfogg/spaniel/internal/storage"
)

func TestTraceAndSpanDrilldownsStayInSession(t *testing.T) {
	cs, db := connect(t, Options{})
	first := db.ActiveSessionID()
	second, err := db.CreateSession("other", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct{ session, name string }{{first, "first"}, {second.ID, "second"}} {
		span := &storage.Span{SessionID: item.session, TraceID: "shared-trace", SpanID: "shared-span", Name: item.name, StartNs: 1, EndNs: 2, Attributes: "{}", Resource: "{}"}
		if err := db.InsertSpan(span); err != nil {
			t.Fatal(err)
		}
		if err := db.InsertLog(&storage.Log{SessionID: item.session, TraceID: span.TraceID, SpanID: span.SpanID, Body: item.name, TimestampNs: 1, Attributes: "{}"}); err != nil {
			t.Fatal(err)
		}
		if err := db.UpsertTraceIssue(&storage.TraceIssue{ID: "issue-" + item.name, SessionID: item.session, TraceID: span.TraceID, Kind: item.name}); err != nil {
			t.Fatal(err)
		}
		if err := db.InsertSpanEvents([]*storage.SpanEvent{{SessionID: item.session, TraceID: span.TraceID, SpanID: span.SpanID, Name: item.name, Attributes: "{}"}}); err != nil {
			t.Fatal(err)
		}
		if err := db.InsertSpanLinks([]*storage.SpanLink{{SessionID: item.session, TraceID: span.TraceID, SpanID: span.SpanID, LinkedTraceID: item.name, Attributes: "{}"}}); err != nil {
			t.Fatal(err)
		}
	}
	for _, item := range []struct{ session, name string }{{first, "first"}, {second.ID, "second"}} {
		trace := callStructured[GetTraceOutput](t, cs, "get_trace", map[string]any{"trace_id": "shared-trace", "session_id": item.session})
		if trace.SessionID != item.session || len(trace.Spans) != 1 || trace.Spans[0].Name != item.name || len(trace.Logs) != 1 || trace.Logs[0].Body != item.name || len(trace.Issues) != 1 || trace.Issues[0].Kind != item.name || trace.LogsTruncated == nil || *trace.LogsTruncated || len(trace.Warnings) != 0 {
			t.Fatalf("trace for %s: %#v", item.name, trace)
		}
		span := callStructured[GetSpanOutput](t, cs, "get_span", map[string]any{"span_id": "shared-span", "session_id": item.session})
		if span.SessionID != item.session || span.Name != item.name || len(span.Events) != 1 || span.Events[0].Name != item.name || len(span.Links) != 1 || span.Links[0].LinkedTraceID != item.name {
			t.Fatalf("span for %s: %#v", item.name, span)
		}
	}
	if err := db.InsertLog(&storage.Log{SessionID: first, TraceID: "shared-trace", SpanID: "shared-span", Body: "additional", TimestampNs: 2, Attributes: "{}"}); err != nil {
		t.Fatal(err)
	}
	limited := callStructured[GetTraceOutput](t, cs, "get_trace", map[string]any{"trace_id": "shared-trace", "session_id": first, "max_logs": 1})
	if limited.LogLimit != 1 || limited.LogsTruncated == nil || !*limited.LogsTruncated || len(limited.Logs) != 1 {
		t.Fatalf("truncation: %#v", limited)
	}
	for _, item := range []struct{ session, name string }{{first, "first"}, {second.ID, "second"}} {
		for _, resource := range []string{"traces/" + item.session + "/shared-trace", "spans/" + item.session + "/shared-span"} {
			got, err := cs.ReadResource(context.Background(), &mcpsdk.ReadResourceParams{URI: "spaniel://" + resource})
			if err != nil || len(got.Contents) != 1 {
				t.Fatalf("resource %s: %#v %v", resource, got, err)
			}
			text := got.Contents[0].Text
			if !strings.Contains(text, item.name) || strings.Contains(text, map[string]string{"first": "second", "second": "first"}[item.name]) {
				t.Fatalf("wrong resource scope %s: %s", resource, text)
			}
		}
	}
	active := callStructured[GetTraceOutput](t, cs, "get_trace", map[string]any{"trace_id": "shared-trace"})
	if active.SessionID != first || len(active.Spans) != 1 || active.Spans[0].Name != "first" {
		t.Fatalf("default scope: %#v", active)
	}
	db.SetActiveSession("", "")
	result, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: "get_trace", Arguments: map[string]any{"trace_id": "shared-trace"}})
	if err == nil && !result.IsError {
		t.Fatal("unscoped ambiguous trace accepted without an active session")
	}
}
