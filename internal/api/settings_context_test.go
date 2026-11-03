package api

import (
	"context"
	"github.com/zfogg/spaniel/traceassert"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"net/http/httptest"
	"testing"
)

func TestSettingsStorageSharesRequestTrace(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	previousPropagation := otel.GetTextMapPropagator()
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		otel.SetTextMapPropagator(previousPropagation)
		_ = provider.Shutdown(context.Background())
	})
	router, _, _ := newSettingsRouter(t)
	before := len(recorder.Ended())
	response := httptest.NewRecorder()
	ctx, browser := provider.Tracer("spaniel/test").Start(context.Background(), "test.browser.settings")
	request := httptest.NewRequest("GET", "/api/settings", nil)
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(request.Header))
	router.ServeHTTP(response, request)
	browser.End()
	if response.Code != 200 {
		t.Fatal(response.Code)
	}
	var root sdktrace.ReadOnlySpan
	var storageSpan sdktrace.ReadOnlySpan
	for _, span := range recorder.Ended()[before:] {
		if span.Name() == "spaniel.http.server.GET /api/settings" {
			root = span
		}
		if span.Name() == "storage.duckdb.GetStats" {
			storageSpan = span
		}
	}
	if root == nil || storageSpan == nil {
		t.Fatal("missing settings request/storage spans")
	}
	if storageSpan.Parent().SpanID() != root.SpanContext().SpanID() {
		t.Fatal("settings storage span lost request context")
	}
	max := 1
	report := traceassert.Check(traceassert.FromReadOnlySpans(recorder.Ended()[before:]), traceassert.Spec{
		Version: 1, NoDuplicateSpanIDs: true,
		Counts:    []traceassert.Count{{Name: "one-api-request", Select: traceassert.Selector{Name: "spaniel.http.server.GET /api/settings"}, Min: 1, Max: &max}, {Name: "one-stats-read", Select: traceassert.Selector{Name: "storage.duckdb.GetStats"}, Min: 1, Max: &max}},
		Ancestry:  []traceassert.Ancestry{{Name: "browser-api-duckdb", Direct: true, Path: []traceassert.Selector{{Name: "test.browser.settings"}, {Name: "spaniel.http.server.GET /api/settings"}, {Name: "storage.duckdb.GetStats", Attributes: map[string]any{"db.system.name": "duckdb"}}}}},
		Redaction: traceassert.Redaction{ForbiddenKeys: []string{"db.query.parameters"}, RequireQueryTextReason: true, ValidateDiagnostics: true},
	})
	if !report.OK() {
		t.Fatalf("Spaniel generated request/storage telemetry violates shared assertions: %+v", report)
	}
}
