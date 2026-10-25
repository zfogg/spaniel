package api

import (
	"context"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"net/http/httptest"
	"testing"
)

func TestSettingsStorageSharesRequestTrace(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous); _ = provider.Shutdown(context.Background()) })
	router, _, _ := newSettingsRouter(t)
	before := len(recorder.Ended())
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/api/settings", nil))
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
}
