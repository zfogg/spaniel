package telemetry

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/instrumentation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestServiceRoutingPreservesTraceParentsAndResources(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", APIService), attribute.String("service.version", "test"))),
		sdktrace.WithSpanProcessor(serviceOwnerProcessor{}),
		sdktrace.WithSyncer(serviceSpanExporter{exporter}),
	)
	defer provider.Shutdown(context.Background())
	tracer := provider.Tracer("spaniel/mcp")
	ctx, root := tracer.Start(WithMCPService(context.Background()), "mcp request")
	_, child := provider.Tracer("spaniel/storage").Start(ctx, "SQL")
	child.End()
	root.End()
	_, api := provider.Tracer("spaniel/api").Start(context.Background(), "API request")
	api.End()
	spans := exporter.GetSpans()
	if len(spans) != 3 {
		t.Fatalf("spans=%d", len(spans))
	}
	for i, span := range spans {
		want := MCPService
		if i == 2 {
			want = APIService
		}
		service, _ := span.Resource.Set().Value(attribute.Key("service.name"))
		version, _ := span.Resource.Set().Value(attribute.Key("service.version"))
		if service.AsString() != want || version.AsString() != "test" {
			t.Fatalf("resource=%v", span.Resource)
		}
		for _, attr := range span.Attributes {
			if string(attr.Key) == serviceOwnerAttribute {
				t.Fatal("internal ownership attribute leaked")
			}
		}
	}
	if spans[0].Parent.SpanID() != spans[1].SpanContext.SpanID() || spans[0].SpanContext.TraceID() != spans[1].SpanContext.TraceID() {
		t.Fatal("routing broke child context")
	}
}

type captureMetricExporter struct {
	sdkmetric.Exporter
	data []metricdata.ResourceMetrics
}

func (e *captureMetricExporter) Export(_ context.Context, data *metricdata.ResourceMetrics) error {
	e.data = append(e.data, *data)
	return nil
}

func TestMCPMetricsUseMCPService(t *testing.T) {
	downstream := &captureMetricExporter{}
	exporter := serviceMetricExporter{downstream}
	data := metricdata.ResourceMetrics{Resource: resource.NewSchemaless(attribute.String("service.name", APIService)), ScopeMetrics: []metricdata.ScopeMetrics{{Scope: instrumentation.Scope{Name: "spaniel"}}, {Scope: instrumentation.Scope{Name: "spaniel/mcp"}}}}
	if err := exporter.Export(context.Background(), &data); err != nil {
		t.Fatal(err)
	}
	if len(downstream.data) != 2 {
		t.Fatal("expected two service resources")
	}
	for i, want := range []string{APIService, MCPService} {
		actual, _ := downstream.data[i].Resource.Set().Value(attribute.Key("service.name"))
		if actual.AsString() != want {
			t.Fatalf("service=%v", actual)
		}
		if len(downstream.data[i].ScopeMetrics) != 1 {
			t.Fatal("metric scope duplicated")
		}
	}
}
