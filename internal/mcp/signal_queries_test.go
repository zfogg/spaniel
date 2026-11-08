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

func TestSignalQueriesScopeSessionsAndTraceChildren(t *testing.T) {
	cs, db := connect(t, Options{})
	active := db.ActiveSessionID()
	other, err := db.CreateSession("other-query-session", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, session := range []string{active, other.ID} {
		if err := db.InsertSpan(&storage.Span{TraceID: "shared", SpanID: "root", Name: "root", ServiceName: "api", SessionID: session, StartNs: 10, Attributes: `{"scope":"root"}`, Resource: `{"region":"west"}`}); err != nil {
			t.Fatal(err)
		}
		childAttr := `{"scope":"other"}`
		if session == other.ID {
			childAttr = `{"scope":"secret"}`
		}
		if err := db.InsertSpan(&storage.Span{TraceID: "shared", SpanID: "child", ParentSpanID: "root", Name: "child", ServiceName: "api", SessionID: session, StartNs: 11, Attributes: childAttr, Resource: `{"region":"west"}`}); err != nil {
			t.Fatal(err)
		}
		if err := db.InsertLog(&storage.Log{Body: "shared", ServiceName: "api", SessionID: session, TimestampNs: 10, Attributes: childAttr, Resource: `{"region":"west"}`}); err != nil {
			t.Fatal(err)
		}
		if err := db.InsertMetric(&storage.Metric{Name: "shared", ServiceName: "api", SessionID: session, TimestampNs: 10, Attributes: childAttr, Resource: `{"region":"west"}`}); err != nil {
			t.Fatal(err)
		}
	}
	for _, tool := range []string{"query_spans", "query_traces", "query_metrics"} {
		t.Run(tool, func(t *testing.T) {
			// The same trace and span IDs in another session must never satisfy this query.
			args := map[string]any{"query": "attributes.scope=secret"}
			if got := callStructured[QuerySignalOutput](t, cs, tool, args); got.Total != 0 {
				t.Fatalf("cross-session result: %#v", got)
			}
			args["session_id"] = other.ID
			got := callStructured[QuerySignalOutput](t, cs, tool, args)
			if got.Total != 1 || len(got.Rows) != 1 || got.SessionID != other.ID {
				t.Fatalf("explicit session: %#v", got)
			}
		})
	}
	args := map[string]any{"query": "attributes.scope=secret"}
	if got := callStructured[QueryLogsOutput](t, cs, "query_logs", args); got.Total != 0 {
		t.Fatalf("cross-session logs: %#v", got)
	}
	args["session_id"] = other.ID
	if got := callStructured[QueryLogsOutput](t, cs, "query_logs", args); got.Total != 1 || got.SessionID != other.ID {
		t.Fatalf("explicit session logs: %#v", got)
	}
}

func TestSignalQueriesBooleanAndAttributeSemantics(t *testing.T) {
	cs, db := connect(t, Options{})
	session := db.ActiveSessionID()
	for i, x := range []struct{ body, attrs, resource string }{
		{"alpha", `{"env":"prod","empty":"","n":5}`, `{"region":"west"}`},
		{"beta", `{"env":"dev","n":10}`, `{"region":"east"}`},
		{"gamma", `{"env":"prod","n":15}`, `{"region":"west"}`},
	} {
		if err := db.InsertLog(&storage.Log{Body: x.body, ServiceName: "api", SessionID: session, TimestampNs: int64(i + 1), Attributes: x.attrs, Resource: x.resource}); err != nil {
			t.Fatal(err)
		}
	}
	tests := []struct {
		query string
		want  int
	}{
		{`body=alpha OR body=beta AND attributes.env=prod`, 1},
		{`(body=alpha OR body=beta) AND attributes.env=dev`, 1},
		{`NOT attributes.env=prod`, 1},
		{`attributes.n>5 && attributes.n<=10`, 1},
		{`body:ALP`, 1},
		{`attributes.empty=""`, 1},
		{`attributes.missing=""`, 0},
		{`!attributes.missing=anything`, 3},
		{`resource_attributes.region=west`, 2},
		{`body="alpha' OR 1=1 --"`, 0},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			got := callStructured[QueryLogsOutput](t, cs, "query_logs", map[string]any{"query": tt.query})
			if got.Total != tt.want || got.Count != tt.want {
				t.Fatalf("got %#v, want %d", got, tt.want)
			}
		})
	}
	// All direct attribute filters and the DSL must apply before pagination and totals.
	args := map[string]any{"query": `attributes.n>=5`, "attributes": map[string]string{"env": "prod"}, "resource_attributes": map[string]string{"region": "west"}, "limit": 1, "page": 1}
	first := callStructured[QueryLogsOutput](t, cs, "query_logs", args)
	if first.Total != 2 || first.Count != 1 || !first.HasMore || first.Logs[0].Body != "gamma" {
		t.Fatalf("first page: %#v", first)
	}
	args["page"] = 2
	second := callStructured[QueryLogsOutput](t, cs, "query_logs", args)
	if second.Total != 2 || second.Count != 1 || second.HasMore || second.Logs[0].Body != "alpha" {
		t.Fatalf("second page: %#v", second)
	}
	args["page"] = 3
	last := callStructured[QueryLogsOutput](t, cs, "query_logs", args)
	if last.Total != 2 || last.Count != 0 || last.HasMore {
		t.Fatalf("past last page: %#v", last)
	}
}

func TestSignalQueryRejectsInvalidInputs(t *testing.T) {
	cs, _ := connect(t, Options{})
	for _, tool := range []string{"query_logs", "query_spans", "query_traces", "query_metrics"} {
		for _, query := range []string{`body=`, `(service=api`, `service=api &&`, `unknown_field=x`, `attributes.=value`} {
			t.Run(tool+"/"+query, func(t *testing.T) {
				result, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: tool, Arguments: map[string]any{"query": query}})
				if err != nil {
					t.Fatalf("tool call failed: %v", err)
				}
				if result == nil || !result.IsError {
					t.Fatalf("accepted invalid query: %#v", result)
				}
			})
		}
	}
}

func TestSignalQueriesDirectFiltersAndPageBounds(t *testing.T) {
	cs, db := connect(t, Options{})
	session := db.ActiveSessionID()
	for _, x := range []struct{ name, env, region string }{{"kept", "prod", "west"}, {"wrong-env", "dev", "west"}, {"wrong-region", "prod", "east"}} {
		attrs := `{"env":"` + x.env + `"}`
		resource := `{"region":"` + x.region + `"}`
		if err := db.InsertSpan(&storage.Span{TraceID: x.name, SpanID: x.name, Name: x.name, ServiceName: "api", SessionID: session, StartNs: 1, Attributes: attrs, Resource: resource}); err != nil {
			t.Fatal(err)
		}
		if err := db.InsertLog(&storage.Log{Body: x.name, ServiceName: "api", SessionID: session, TimestampNs: 1, Attributes: attrs, Resource: resource}); err != nil {
			t.Fatal(err)
		}
		if err := db.InsertMetric(&storage.Metric{Name: x.name, ServiceName: "api", SessionID: session, TimestampNs: 1, Attributes: attrs, Resource: resource}); err != nil {
			t.Fatal(err)
		}
	}
	args := map[string]any{"attributes": map[string]string{"env": "prod"}, "resource_attributes": map[string]string{"region": "west"}, "limit": 1001, "page": 0}
	for _, tool := range []string{"query_spans", "query_traces", "query_metrics"} {
		t.Run(tool, func(t *testing.T) {
			got := callStructured[QuerySignalOutput](t, cs, tool, args)
			if got.Total != 1 || len(got.Rows) != 1 || got.Rows[0]["name"] != "kept" || got.Limit != 1000 || got.Page != 1 || got.HasMore {
				t.Fatalf("filters/page bounds: %#v", got)
			}
		})
	}
	logs := callStructured[QueryLogsOutput](t, cs, "query_logs", args)
	if logs.Total != 1 || logs.Count != 1 || logs.Logs[0].Body != "kept" || logs.Limit != 1000 || logs.Page != 1 || logs.HasMore {
		t.Fatalf("log filters/page bounds: %#v", logs)
	}
	for _, tool := range []string{"query_logs", "query_spans", "query_traces", "query_metrics"} {
		result, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: tool, Arguments: map[string]any{"query": "service=api", "page": 1000001}})
		if err != nil {
			t.Fatalf("tool call failed: %v", err)
		}
		if result == nil || !result.IsError {
			t.Errorf("%s accepted an excessive page: %#v", tool, result)
		}
	}
}

func TestQueryLogsCombinesDSLWithTimeSeverityAndCorrelation(t *testing.T) {
	cs, db := connect(t, Options{})
	session := db.ActiveSessionID()
	for _, l := range []*storage.Log{
		{Body: "kept", ServiceName: "api", SessionID: session, TraceID: "trace-a", SpanID: "span-a", Severity: 17, TimestampNs: 100, Attributes: `{"kind":"request"}`},
		{Body: "wrong trace", ServiceName: "api", SessionID: session, TraceID: "trace-b", SpanID: "span-a", Severity: 17, TimestampNs: 100, Attributes: `{"kind":"request"}`},
		{Body: "wrong span", ServiceName: "api", SessionID: session, TraceID: "trace-a", SpanID: "span-b", Severity: 17, TimestampNs: 100, Attributes: `{"kind":"request"}`},
		{Body: "wrong severity", ServiceName: "api", SessionID: session, TraceID: "trace-a", SpanID: "span-a", Severity: 9, TimestampNs: 100, Attributes: `{"kind":"request"}`},
		{Body: "wrong time", ServiceName: "api", SessionID: session, TraceID: "trace-a", SpanID: "span-a", Severity: 17, TimestampNs: 101, Attributes: `{"kind":"request"}`},
	} {
		if err := db.InsertLog(l); err != nil {
			t.Fatal(err)
		}
	}
	got := callStructured[QueryLogsOutput](t, cs, "query_logs", map[string]any{"query": "attributes.kind=request", "trace_id": "trace-a", "span_id": "span-a", "min_severity": "ERROR", "max_severity": "ERROR", "from_ns": "100", "to_ns": "100"})
	if got.Total != 1 || got.Count != 1 || got.Logs[0].Body != "kept" {
		t.Fatalf("combined filters: %#v", got)
	}
}

func TestQueryLogsPreservesQuotedAndLiteralAttributeValues(t *testing.T) {
	cs, db := connect(t, Options{})
	session := db.ActiveSessionID()
	if err := db.InsertLog(&storage.Log{Body: `payment said "approved"`, ServiceName: "api", SessionID: session, TimestampNs: 1, Attributes: `{"http.method":"GET","quoted\"key":"x' OR 1=1 --"}`, Resource: `{"cloud.region":"us east"}`}); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		args map[string]any
		want int
	}{
		{"quoted body", map[string]any{"query": `body:"said \"approved\""`}, 1},
		{"dotted key and spaced value", map[string]any{"query": `attributes.http.method=GET && resource.cloud.region="us east"`}, 1},
		{"injection-shaped direct filter", map[string]any{"query": `service=api`, "attributes": map[string]string{`quoted"key`: `x' OR 1=1 --`}}, 1},
		{"different injection-shaped value", map[string]any{"query": `service=api`, "attributes": map[string]string{`quoted"key`: `x' OR 1=1 --more`}}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := callStructured[QueryLogsOutput](t, cs, "query_logs", tt.args)
			if got.Total != tt.want || got.Count != tt.want {
				t.Fatalf("got %#v, want %d", got, tt.want)
			}
		})
	}
}

func TestQueryToolsStructuredResultsPreserveNanoseconds(t *testing.T) {
	cs, db := connect(t, Options{})
	session := db.ActiveSessionID()
	const ts int64 = 1794074157898417901
	if err := db.InsertSpan(&storage.Span{TraceID: "trace", SpanID: "root", Name: "span", SessionID: session, StartNs: ts}); err != nil {
		t.Fatal(err)
	}
	if err := db.InsertLog(&storage.Log{Body: "log", SessionID: session, TimestampNs: ts}); err != nil {
		t.Fatal(err)
	}
	if err := db.InsertMetric(&storage.Metric{Name: "metric", SessionID: session, TimestampNs: ts}); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ tool, rows, time string }{{"query_logs", "logs", "timestamp_ns"}, {"query_spans", "rows", "start_ns"}, {"query_traces", "rows", "start_ns"}, {"query_metrics", "rows", "timestamp_ns"}} {
		t.Run(tt.tool, func(t *testing.T) {
			result, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: tt.tool, Arguments: map[string]any{"query": ""}})
			if err != nil || result.IsError {
				t.Fatalf("call: %#v %v", result, err)
			}
			structured, ok := result.StructuredContent.(map[string]any)
			if !ok {
				t.Fatalf("structured output: %#v", result.StructuredContent)
			}
			rows, ok := structured[tt.rows].([]any)
			if !ok || len(rows) != 1 {
				t.Fatalf("rows: %#v", structured)
			}
			row, ok := rows[0].(map[string]any)
			if !ok || row[tt.time] != "1794074157898417901" {
				t.Fatalf("timestamp rounded: %#v", rows[0])
			}
			if structured["has_more"] != false {
				t.Fatalf("pagination: %#v", structured)
			}
		})
	}
}

func TestQueryToolsRejectFieldsFromOtherSignals(t *testing.T) {
	cs, _ := connect(t, Options{})
	for _, tt := range []struct{ tool, query string }{{"query_logs", "name=span"}, {"query_spans", "body=log"}, {"query_traces", "value=5"}, {"query_metrics", "trace_id=trace"}, {"query_logs", "timestamp_ns>not-a-number"}, {"query_spans", "start_ns>not-a-number"}} {
		t.Run(tt.tool+"/"+tt.query, func(t *testing.T) {
			result, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: tt.tool, Arguments: map[string]any{"query": tt.query}})
			if err != nil {
				t.Fatalf("tool call failed: %v", err)
			}
			if result == nil || !result.IsError {
				t.Fatalf("unexpected success: %#v", result)
			}
		})
	}
}
