package ingestion

import (
	"context"
	"strings"
	"testing"

	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/zfogg/spaniel/internal/ws"
)

// TestIngestTraces_NestedSpans proves the ingestion side of #108: an OTLP ingest
// produces "IngestTraces" nested under the caller's span (the OTLP HTTP request),
// and a "db.flush" span nested under IngestTraces for the DuckDB write.
func TestIngestTraces_NestedSpans(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	db := openTestDB(t)
	sess, _ := db.CreateSession("s", false)
	db.SetActiveSession(sess.ID, sess.Label)

	// Pipeline is constructed AFTER the provider is set so its tracer exports.
	p := NewPipeline(db, ws.NewHub())
	td := makeTraces("demo-app", func(ss ptrace.ScopeSpans) {
		putSpan(ss, "a", "1", "", "GET /checkout", ptrace.SpanKindServer, nil)
	})

	// Stand in for the instrumented OTLP HTTP request span.
	ctx, reqSpan := otel.Tracer("test").Start(context.Background(), "POST /v1/traces")
	if err := p.IngestTraces(ctx, td); err != nil {
		t.Fatalf("IngestTraces: %v", err)
	}
	reqSpan.End()

	byName := map[string]sdktrace.ReadOnlySpan{}
	for _, s := range sr.Ended() {
		byName[s.Name()] = s
	}

	ingest, ok := byName["IngestTraces"]
	if !ok {
		t.Fatalf("no IngestTraces span; got %v", names(sr))
	}
	if ingest.Parent().SpanID() != reqSpan.SpanContext().SpanID() {
		t.Errorf("IngestTraces not nested under the request span")
	}
	flush, ok := byName["db.flush"]
	if !ok {
		t.Fatalf("no db.flush span; got %v", names(sr))
	}
	if flush.Parent().SpanID() != ingest.SpanContext().SpanID() {
		t.Errorf("db.flush not nested under IngestTraces")
	}
}

// TestIngestTraces_SelfTelemetryQuiet proves the self-monitor feedback fix:
// ingesting Spaniel's own self-telemetry (service.name == selfService) stores
// the spans but generates NO new instrumentation spans (IngestTraces, db.flush,
// db.*, lintSpan), so self-monitoring can't feed back on itself.
func TestIngestTraces_SelfTelemetryQuiet(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	db := openTestDB(t)
	sess, _ := db.CreateSession("s", false)
	db.SetActiveSession(sess.ID, sess.Label)

	p := NewPipeline(db, ws.NewHub())
	p.SetSelfService("spaniel")

	td := makeTraces("spaniel", func(ss ptrace.ScopeSpans) { // service.name == self
		putSpan(ss, "a", "1", "", "GET /thing", ptrace.SpanKindServer, nil)
	})
	pre := len(sr.Ended()) // ignore spans from test setup (CreateSession, etc.)
	if err := p.IngestTraces(context.Background(), td); err != nil {
		t.Fatalf("IngestTraces: %v", err)
	}

	// No instrumentation spans should have been produced by the self ingest.
	for _, s := range sr.Ended()[pre:] {
		n := s.Name()
		if n == "IngestTraces" || n == "db.flush" || n == "lintSpan" || strings.HasPrefix(n, "db.") {
			t.Errorf("self-telemetry ingest produced instrumentation span %q (should be quiet)", n)
		}
	}

	// But the data must still be stored.
	tid := td.ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(0).TraceID().String()
	spans, err := db.GetTrace(tid)
	if err != nil || len(spans) != 1 {
		t.Fatalf("self-telemetry span not stored: got %d spans, err=%v", len(spans), err)
	}
}

func names(sr *tracetest.SpanRecorder) []string {
	var out []string
	for _, s := range sr.Ended() {
		out = append(out, s.Name())
	}
	return out
}
