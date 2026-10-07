package api

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/ws"
)

func TestEvaluateAlerts_NestsStorageSpans(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		_ = provider.Shutdown(context.Background())
	})

	store, err := storage.Open(filepath.Join(t.TempDir(), "alerts.duckdb"))
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.CreateAlertRule(&storage.AlertRule{
		ID:            "rule-1",
		Name:          "test rule",
		QuerySQL:      "SELECT 0 AS value",
		ConditionJSON: `{"kind":"threshold","operator":">","value":1}`,
		GroupByJSON:   `[]`,
		Enabled:       true,
	}); err != nil {
		t.Fatalf("create alert rule: %v", err)
	}

	before := len(recorder.Ended())
	evaluateAlerts(context.Background(), store, ws.NewHub(), time.Now(), time.Second)

	var root sdktrace.ReadOnlySpan
	for _, span := range recorder.Ended()[before:] {
		if span.Name() == "spaniel.alerts.evaluate" {
			root = span
			break
		}
	}
	if root == nil {
		t.Fatal("spaniel.alerts.evaluate span was not recorded")
	}
	for _, span := range recorder.Ended()[before:] {
		if span.Name() == "storage.ListAlertRules" && span.Parent().SpanID() == root.SpanContext().SpanID() {
			return
		}
	}
	t.Fatal("alert evaluator did not nest storage.ListAlertRules under spaniel.alerts.evaluate")
}
