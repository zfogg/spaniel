package storage

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	sequel "github.com/zfogg/sequel/go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// This bounded probe executes deliberately bad SQL only in a temporary DB.
// An explicit endpoint exports its actual database spans for local API checks.
func TestQueryDiagnosticsRealDuckDB(t *testing.T) {
	ctx := context.Background()
	sr := tracetest.NewSpanRecorder()
	opts := []sdktrace.TracerProviderOption{sdktrace.WithSpanProcessor(sr), sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", "spaniel-api")))}
	if endpoint := os.Getenv("SPANIEL_DIAGNOSTICS_PROBE_ENDPOINT"); endpoint != "" {
		exporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithEndpoint(endpoint), otlptracegrpc.WithInsecure())
		if err != nil {
			t.Fatal(err)
		}
		opts = append(opts, sdktrace.WithSyncer(exporter))
	}
	previous := otel.GetTracerProvider()
	tp := sdktrace.NewTracerProvider(opts...)
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(previous); _ = tp.Shutdown(ctx) })
	d, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	for _, tc := range []struct{ name, query, code string }{
		{"DiagnosticsCleanProbe", "SELECT 1", ""},
		{"DiagnosticsBadProbe", "SELECT 'private-diagnostics-value", "unterminated_string"},
	} {
		_, _, _, err := d.WriteEnabledQuery(WithQueryName(ctx, tc.name), tc.query, 1)
		if (err != nil) != (tc.code != "") {
			t.Fatalf("%s error = %v", tc.name, err)
		}
		var found bool
		for _, span := range sr.Ended() {
			if span.Name() != "storage.duckdb."+tc.name {
				continue
			}
			found = true
			var raw []string
			for _, attr := range span.Attributes() {
				if attr.Key == "db.query.diagnostics" {
					raw = attr.Value.AsStringSlice()
				}
				if tc.code != "" && attr.Key == "db.query.text" {
					t.Fatal("malformed original SQL was emitted")
				}
				if strings.Contains(attr.Value.Emit(), "private-diagnostics-value") {
					t.Fatalf("private SQL in %s", attr.Key)
				}
			}
			if raw == nil {
				t.Fatal("SDK did not emit a diagnostics array")
			}
			if tc.code == "" && len(raw) != 0 {
				t.Fatalf("clean results: %v", raw)
			}
			if tc.code != "" {
				var diagnostic sequel.Diagnostic
				if len(raw) != 1 || json.Unmarshal([]byte(raw[0]), &diagnostic) != nil || diagnostic.Code != tc.code {
					t.Fatalf("bad results: %v", raw)
				}
			}
			t.Logf("trace_id=%s span_id=%s name=%s db.query.diagnostics=%v", span.SpanContext().TraceID(), span.SpanContext().SpanID(), span.Name(), raw)
		}
		if !found {
			t.Fatalf("missing %s span", tc.name)
		}
	}
}
