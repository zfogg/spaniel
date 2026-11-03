package traceassert

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestSDKCapturePreservesStatusAndRecursiveTypedArrayRedaction(t *testing.T) {
	r := tracetest.NewSpanRecorder()
	p := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(r), sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", "app"), attribute.StringSlice("resource.values", []string{"private-value"}))))
	t.Cleanup(func() { p.Shutdown(context.Background()) })
	_, s := p.Tracer("test").Start(context.Background(), "query", trace.WithAttributes(attribute.StringSlice("array", []string{"private-value"})), trace.WithLinks(trace.Link{SpanContext: trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{15: 1}, SpanID: trace.SpanID{7: 1}}), Attributes: []attribute.KeyValue{attribute.StringSlice("link.values", []string{"private-value"})}}))
	s.SetStatus(codes.Error, "private-value")
	s.AddEvent("error", trace.WithAttributes(attribute.StringSlice("event.values", []string{"private-value"})))
	s.End()
	spans := FromReadOnlySpans(r.Ended())
	if len(spans) != 1 || spans[0].StatusCode != 2 || spans[0].ServiceName != "app" || len(spans[0].Events) != 1 || len(spans[0].Links) != 1 {
		t.Fatal("SDK metadata/status lost")
	}
	report := Check(spans, Spec{Version: 1, Redaction: Redaction{ForbiddenValues: []string{"private-value"}}})
	count := 0
	for _, f := range report.Failures {
		if f.Code == "forbidden_value" {
			count++
		}
	}
	if count != 5 {
		t.Fatalf("typed array/resource/event/link secrets: %+v", report)
	}
}
