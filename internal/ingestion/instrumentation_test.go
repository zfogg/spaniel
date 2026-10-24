package ingestion

import (
	"context"
	"strings"
	"testing"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/zfogg/spaniel/internal/ws"
)

// TestIngestTraces_NestedSpans proves the ingestion side of #108: an OTLP ingest
// produces "spaniel.IngestTraces" nested under the caller's span (the OTLP HTTP
// request), and a "spaniel.db.flush" span nested under it for the DuckDB write.
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

	ingest, ok := byName["spaniel.IngestTraces"]
	if !ok {
		t.Fatalf("no IngestTraces span; got %v", names(sr))
	}
	if ingest.Parent().SpanID() != reqSpan.SpanContext().SpanID() {
		t.Errorf("IngestTraces not nested under the request span")
	}
	flush, ok := byName["spaniel.db.flush"]
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
		if n == "spaniel.IngestTraces" || n == "spaniel.db.flush" || n == "spaniel.lintSpan" || strings.HasPrefix(n, "spaniel.db.") {
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

func TestMetricCatalogPreservesIngestContext(t *testing.T) {
	for _, self := range []bool{false, true} {
		name := "external"
		if self {
			name = "self"
		}
		t.Run(name, func(t *testing.T) {
			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			previous := otel.GetTracerProvider()
			otel.SetTracerProvider(provider)
			t.Cleanup(func() { otel.SetTracerProvider(previous); _ = provider.Shutdown(context.Background()) })
			db := openTestDB(t)
			sess, err := db.CreateSession("metrics", false)
			if err != nil {
				t.Fatal(err)
			}
			db.SetActiveSession(sess.ID, sess.Label)
			pipeline := NewPipeline(db, ws.NewHub())
			pipeline.SetSelfService("spaniel")
			service := "test-app"
			if self {
				service = "spaniel"
			}
			md := makeMetrics(service, func(rm pmetric.ResourceMetrics) {
				metric := rm.ScopeMetrics().AppendEmpty().Metrics().AppendEmpty()
				metric.SetName("regression.counter")
				dp := metric.SetEmptyGauge().DataPoints().AppendEmpty()
				dp.SetDoubleValue(1)
				dp.SetTimestamp(pcommon.Timestamp(1))
			})
			ctx, request := otel.Tracer("test").Start(context.Background(), "metrics request")
			before := len(recorder.Ended())
			if err := pipeline.IngestMetrics(ctx, md); err != nil {
				t.Fatal(err)
			}
			request.End()
			found := map[string]bool{}
			for _, span := range recorder.Ended()[before:] {
				if self && span.Name() != "metrics request" {
					t.Fatalf("self ingest emitted %s", span.Name())
				}
				if strings.HasPrefix(span.Name(), "spaniel.storage.") {
					if span.SpanContext().TraceID() != request.SpanContext().TraceID() || !span.Parent().IsValid() {
						t.Fatalf("orphan catalog query: %s", span.Name())
					}
					found[span.Name()] = true
				}
			}
			if !self && (!found["spaniel.storage.RecordMetricSeries"] || !found["spaniel.storage.ActiveMetricSeries"]) {
				t.Fatalf("missing catalog spans: %v", found)
			}
			rows, err := db.ListMetricCatalog(sess.ID)
			if err != nil || len(rows) != 1 {
				t.Fatalf("metrics not stored: %v %v", rows, err)
			}
			if err := pipeline.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSplitServicesRemainQuiet(t *testing.T) {
	for _, service := range []string{"spaniel-api", "spaniel-mcp", "spaniel-web"} {
		t.Run(service, func(t *testing.T) {
			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			previous := otel.GetTracerProvider()
			otel.SetTracerProvider(provider)
			t.Cleanup(func() { otel.SetTracerProvider(previous); _ = provider.Shutdown(context.Background()) })
			db := openTestDB(t)
			session, err := db.CreateSession("split", false)
			if err != nil {
				t.Fatal(err)
			}
			db.SetActiveSession(session.ID, session.Label)
			pipeline := NewPipeline(db, ws.NewHub())
			pipeline.SetSelfService("spaniel")
			td := makeTraces(service, func(ss ptrace.ScopeSpans) { putSpan(ss, "a", "1", "", "request", ptrace.SpanKindServer, nil) })
			before := len(recorder.Ended())
			if err := pipeline.IngestTraces(context.Background(), td); err != nil {
				t.Fatal(err)
			}
			if len(recorder.Ended()) != before {
				t.Fatalf("recursive spans for %s: %v", service, names(recorder))
			}
			if err := pipeline.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
		})
	}
}
