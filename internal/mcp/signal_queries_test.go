package mcp

import (
	"context"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zfogg/spaniel/internal/storage"
)

func TestBooleanSignalQueries(t *testing.T) {
	cs, db := connect(t, Options{})
	session := db.ActiveSessionID()
	for _, x := range []struct{ name, service, attr string }{{"checkout", "api", "prod"}, {"worker", "jobs", "dev"}, {"timeout", "api", "dev"}} {
		if err := db.InsertSpan(&storage.Span{TraceID: x.name, SpanID: x.name, Name: x.name, ServiceName: x.service, SessionID: session, StartNs: 10, EndNs: 20, Attributes: `{"env":"` + x.attr + `","count":5}`, Resource: `{"region":"west"}`}); err != nil {
			t.Fatal(err)
		}
		if err := db.InsertLog(&storage.Log{Body: x.name, ServiceName: x.service, SessionID: session, TimestampNs: 10, Attributes: `{"env":"` + x.attr + `","count":5}`, Resource: `{"region":"west"}`}); err != nil {
			t.Fatal(err)
		}
		if err := db.InsertMetric(&storage.Metric{Name: x.name, ServiceName: x.service, SessionID: session, TimestampNs: 10, Attributes: `{"env":"` + x.attr + `","count":5}`, Resource: `{"region":"west"}`}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.InsertSpan(&storage.Span{TraceID: "checkout", SpanID: "child", ParentSpanID: "checkout", Name: "child", ServiceName: "payments", SessionID: session, StartNs: 11, EndNs: 12, Attributes: `{"special":"yes"}`, Resource: `{"region":"east"}`}); err != nil {
		t.Fatal(err)
	}
	numeric := callStructured[QuerySignalOutput](t, cs, "query_metrics", map[string]any{"query": `attributes.count >= 5`})
	if numeric.Total != 3 {
		t.Fatalf("numeric attribute comparison: %#v", numeric)
	}
	trace := callStructured[QuerySignalOutput](t, cs, "query_traces", map[string]any{"query": `attributes.special=yes && resource.region=east`})
	if trace.Total != 1 || len(trace.Rows) != 1 || trace.Rows[0]["trace_id"] != "checkout" {
		t.Fatalf("child span should match its trace: %#v", trace)
	}
	for _, tool := range []string{"query_spans", "query_traces", "query_metrics"} {
		out := callStructured[QuerySignalOutput](t, cs, tool, map[string]any{"query": `service=api && (attributes.env=prod || !name:timeout) && resource.region=west`, "limit": 1})
		if out.Total != 1 || len(out.Rows) != 1 || out.HasMore {
			t.Fatalf("%s: %#v", tool, out)
		}
		second := callStructured[QuerySignalOutput](t, cs, tool, map[string]any{"query": `service=api OR service=jobs`, "limit": 1, "page": 2})
		if second.Total != 3 || len(second.Rows) != 1 || !second.HasMore {
			t.Fatalf("%s pagination: %#v", tool, second)
		}
	}
	empty := callStructured[QueryLogsOutput](t, cs, "query_logs", map[string]any{"query": `attributes.absent=""`})
	if empty.Total != 0 {
		t.Fatalf("missing attribute matched an empty value: %#v", empty)
	}
	words := callStructured[QueryLogsOutput](t, cs, "query_logs", map[string]any{"query": `checkout || worker`})
	if words.Total != 2 {
		t.Fatalf("boolean text search: %#v", words)
	}
	excluded := callStructured[QueryLogsOutput](t, cs, "query_logs", map[string]any{"query": `!timeout`})
	if excluded.Total != 2 {
		t.Fatalf("negated text search: %#v", excluded)
	}
	logs := callStructured[QueryLogsOutput](t, cs, "query_logs", map[string]any{"query": `service=api && (attributes.env=prod || !body:timeout) && resource.region=west`})
	if logs.Total != 1 || logs.Count != 1 || logs.Logs[0].Body != "checkout" || logs.Logs[0].Resource["region"] != "west" {
		t.Fatalf("logs: %#v", logs)
	}
	logs = callStructured[QueryLogsOutput](t, cs, "query_logs", map[string]any{"query": `body:checkout OR body:worker`, "resource_attributes": map[string]string{"region": "west"}, "service": "jobs"})
	if logs.Count != 1 || logs.Logs[0].Body != "worker" {
		t.Fatalf("combined filters: %#v", logs)
	}
	for _, q := range []string{`service=api &&`, `resource.region="west' OR true --"`, `madeup=foo`} {
		res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: "query_spans", Arguments: map[string]any{"query": q}})
		if q == `resource.region="west' OR true --"` {
			if err != nil || res.IsError {
				t.Fatalf("quoted literal: %v %#v", err, res)
			}
			injected := callStructured[QuerySignalOutput](t, cs, "query_spans", map[string]any{"query": q})
			if injected.Total != 0 {
				t.Fatalf("quoted value changed the predicate: %#v", injected)
			}
			continue
		}
		if err == nil && !res.IsError {
			t.Fatalf("accepted invalid query %q", q)
		}
	}
}
