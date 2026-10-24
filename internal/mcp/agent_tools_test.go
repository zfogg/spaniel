package mcp

import (
	"context"
	"encoding/json"
	toongo "github.com/toon-format/toon-go"
	"strconv"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zfogg/spaniel/internal/storage"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestAgentDiscoveryAndFilters(t *testing.T) {
	cs, db := connect(t, Options{})
	session := db.ActiveSessionID()
	for i := 0; i < 3; i++ {
		attrs := `{"test.id":"other","numeric":42}`
		resource := `{"service.version":"old"}`
		if i == 0 {
			attrs = `{"test.id":"target","numeric":42,"empty":"","quote\"key":"x' OR 1=1 --"}`
			resource = `{"service.version":"new"}`
		}
		parent := ""
		trace := "t" + strconv.Itoa(i)
		if i == 0 {
			parent = "root"
			trace = "parent-trace"
		}
		if err := db.InsertSpan(&storage.Span{SpanID: "s" + strconv.Itoa(i), TraceID: trace, ParentSpanID: parent, ServiceName: "api", Name: "work", StartNs: int64(i + 1), EndNs: int64(i + 11), Attributes: attrs, Resource: resource, SessionID: session}); err != nil {
			t.Fatal(err)
		}
		if err := db.InsertLog(&storage.Log{TimestampNs: int64(i + 1), ServiceName: "api", Body: "log", Attributes: attrs, SessionID: session}); err != nil {
			t.Fatal(err)
		}
		if err := db.InsertMetric(&storage.Metric{Name: "test.metric", Type: "gauge", TimestampNs: int64(i + 1), ServiceName: "api", Attributes: attrs, Resource: resource, SessionID: session}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.InsertSpan(&storage.Span{SpanID: "root", TraceID: "parent-trace", ServiceName: "gateway", Name: "request", StartNs: 1, EndNs: 20, Attributes: "{}", Resource: "{}", SessionID: session}); err != nil {
		t.Fatal(err)
	}
	keys := callAgentTool[AttributeKeysOutput](t, cs, "list_attribute_keys", map[string]any{"signal": "spans", "scope": "attributes", "prefix": "test.", "limit": 1})
	if len(keys.Keys) != 1 || keys.Keys[0].Key != "test.id" || keys.Keys[0].Count != 3 || keys.HasMore {
		t.Fatalf("keys: %+v", keys)
	}
	allSignals := callAgentTool[AttributeValuesOutput](t, cs, "list_attribute_values", map[string]any{"key": "test.id", "from_ns": "1", "to_ns": "1"})
	if len(allSignals.Values) != 3 {
		t.Fatalf("cross-signal discovery: %+v", allSignals)
	}
	for _, value := range allSignals.Values {
		if value.Count != 1 || value.Value != "target" {
			t.Fatalf("windowed discovery: %+v", value)
		}
	}
	resources := callAgentTool[AttributeKeysOutput](t, cs, "list_attribute_keys", map[string]any{"scope": "resource", "prefix": "service.version"})
	if len(resources.Keys) != 2 {
		t.Fatalf("resource discovery: %+v", resources)
	}
	values := callAgentTool[AttributeValuesOutput](t, cs, "list_attribute_values", map[string]any{"signal": "spans", "scope": "attributes", "key": "test.id", "limit": 1})
	if !values.HasMore || len(values.Values) != 1 || values.Values[0].Value != "other" {
		t.Fatalf("values: %+v", values)
	}
	values = callAgentTool[AttributeValuesOutput](t, cs, "list_attribute_values", map[string]any{"signal": "spans", "scope": "attributes", "key": "numeric"})
	if len(values.Values) != 1 || values.Values[0].Value != "42" || values.Values[0].ValueType != "UBIGINT" {
		t.Fatalf("numeric: %+v", values)
	}
	filters := map[string]any{"attributes": map[string]string{"test.id": "target", "numeric": "42"}, "resource_attributes": map[string]string{"service.version": "new"}, "limit": 1}
	traces := callAgentTool[ListTracesOutput](t, cs, "list_traces", filters)
	if traces.Count != 1 || traces.Traces[0].TraceID != "parent-trace" {
		t.Fatalf("any-span trace filtering: %+v", traces)
	}
	spans := callAgentTool[ListSlowSpansOutput](t, cs, "list_slow_spans", filters)
	if spans.Count != 1 || spans.Spans[0].SpanID != "s0" {
		t.Fatalf("filter before limit: %+v", spans)
	}
	logs := callAgentTool[QueryLogsOutput](t, cs, "query_logs", map[string]any{"attributes": map[string]string{"test.id": "target"}, "limit": 1})
	if logs.Count != 1 || logs.Logs[0].TimestampNs != 1 {
		t.Fatalf("logs: %+v", logs)
	}
	filters["name"] = "test.metric"
	points := callAgentTool[GetMetricSeriesOutput](t, cs, "get_metric_series", filters)
	if points.Count != 1 || points.Points[0].TimestampNs != 1 {
		t.Fatalf("metrics: %+v", points)
	}
	spans = callAgentTool[ListSlowSpansOutput](t, cs, "list_slow_spans", map[string]any{"attributes": map[string]string{"quote\"key": "x' OR 1=1 --", "empty": ""}})
	if spans.Count != 1 {
		t.Fatalf("literal keys/values and empty strings: %+v", spans)
	}
	spans = callAgentTool[ListSlowSpansOutput](t, cs, "list_slow_spans", map[string]any{"attributes": map[string]string{"missing": ""}})
	if spans.Count != 0 {
		t.Fatal("missing key matched empty string")
	}
	keys = callAgentTool[AttributeKeysOutput](t, cs, "list_attribute_keys", map[string]any{"session_id": "absent"})
	if len(keys.Keys) != 0 {
		t.Fatal("discovery leaked session")
	}
}

func TestServiceSummaryScopes(t *testing.T) {
	cs, db := connect(t, Options{})
	seedTrace(t, db, db.ActiveSessionID())
	out := callAgentTool[ServiceSummaryOutput](t, cs, "get_service_summary", map[string]any{"service": "postgres", "from_ns": "2000000", "to_ns": "2000000"})
	if out.SpanCount != 1 || out.TraceCount != 1 || out.ErrorSpanCount != 1 || out.SpanErrorRate != 1 || out.P95Ms != 300 || out.LogCount != 0 || len(out.Findings) != 2 || len(out.Dependencies) != 1 {
		t.Fatalf("summary: %+v", out)
	}
	empty := callAgentTool[ServiceSummaryOutput](t, cs, "get_service_summary", map[string]any{"service": "absent"})
	if empty.SpanCount != 0 || empty.P95Ms != 0 || len(empty.Findings) != 0 {
		t.Fatalf("empty: %+v", empty)
	}
	for _, args := range []map[string]any{{"service": "api", "from_ns": "20", "to_ns": "10"}, {"service": "api", "from_ns": "oops"}} {
		res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: "get_service_summary", Arguments: args})
		if err == nil && !res.IsError {
			t.Fatal("invalid window accepted")
		}
	}
}

func TestMCPTelemetryIncludesToolErrors(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	previousMeter := otel.GetMeterProvider()
	otel.SetMeterProvider(meterProvider)
	t.Cleanup(func() { otel.SetMeterProvider(previousMeter); _ = meterProvider.Shutdown(context.Background()) })
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous); _ = provider.Shutdown(context.Background()) })
	cs, _ := connect(t, Options{})
	callAgentTool[AttributeKeysOutput](t, cs, "list_attribute_keys", map[string]any{"signal": "spans"})
	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: "list_attribute_values", Arguments: map[string]any{}})
	if err != nil || !res.IsError {
		t.Fatalf("expected tool error: %v %v", res, err)
	}
	foundOK, foundError := false, false
	var names []string
	for _, s := range recorder.Ended() {
		names = append(names, s.Name())
		if s.Name() == "spaniel.mcp.tool/list_attribute_keys" {
			foundOK = s.Status().Code != codes.Error
		}
		if s.Name() == "spaniel.mcp.tool/list_attribute_values" {
			foundError = s.Status().Code == codes.Error
		}
	}
	if !foundOK || !foundError {
		t.Fatalf("MCP span status: success=%v error=%v names=%v", foundOK, foundError, names)
	}
	var data metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &data); err != nil {
		t.Fatal(err)
	}
	foundCounter, foundDuration := false, false
	for _, scope := range data.ScopeMetrics {
		if scope.Scope.Name != "spaniel/mcp" {
			continue
		}
		for _, m := range scope.Metrics {
			switch m.Name {
			case "spaniel.mcp.requests":
				for _, p := range m.Data.(metricdata.Sum[int64]).DataPoints {
					tool, _ := p.Attributes.Value(attribute.Key("tool"))
					result, _ := p.Attributes.Value(attribute.Key("result"))
					if tool.AsString() == "list_attribute_values" && result.AsString() == "error" && p.Value == 1 {
						foundCounter = true
					}
				}
			case "spaniel.mcp.duration":
				foundDuration = len(m.Data.(metricdata.Histogram[float64]).DataPoints) > 0
			}
		}
	}
	if !foundCounter || !foundDuration {
		t.Fatalf("gotel MCP metrics missing: counter=%v duration=%v", foundCounter, foundDuration)
	}
}

// These tools intentionally return timestamps and discovered values as strings.
// Do not apply the legacy numeric-string coercion used by older tool tests.
func callAgentTool[T any](t *testing.T, cs *mcpsdk.ClientSession, name string, args map[string]any) T {
	t.Helper()
	text := callText(t, cs, name, args)
	decoded, err := toongo.DecodeString(text)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("%s: %v\n%s", name, err, text)
	}
	return out
}
