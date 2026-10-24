package telemetry

import (
	"context"
	"errors"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

const (
	APIService            = "spaniel-api"
	MCPService            = "spaniel-mcp"
	WebService            = "spaniel-web"
	serviceOwnerAttribute = "spaniel.internal.service_owner"
)

func IsOwnService(name string) bool {
	return name == APIService || name == MCPService || name == WebService
}

type serviceOwnerKey struct{}

// WithMCPService keeps the service boundary on every descendant, including SQL.
// A single SDK pipeline still owns sampling, propagation, shutdown and exporters.
func WithMCPService(ctx context.Context) context.Context {
	return context.WithValue(ctx, serviceOwnerKey{}, MCPService)
}

type serviceOwnerProcessor struct{}

func (serviceOwnerProcessor) OnStart(ctx context.Context, span sdktrace.ReadWriteSpan) {
	if ctx.Value(serviceOwnerKey{}) == MCPService {
		span.SetAttributes(attribute.String(serviceOwnerAttribute, MCPService))
	}
}
func (serviceOwnerProcessor) OnEnd(sdktrace.ReadOnlySpan)      {}
func (serviceOwnerProcessor) Shutdown(context.Context) error   { return nil }
func (serviceOwnerProcessor) ForceFlush(context.Context) error { return nil }

func serviceResource(original *resource.Resource, service string) *resource.Resource {
	merged, _ := resource.Merge(original, resource.NewSchemaless(attribute.String("service.name", service)))
	return merged
}

type serviceSpan struct {
	sdktrace.ReadOnlySpan
	res   *resource.Resource
	attrs []attribute.KeyValue
}

func (s serviceSpan) Resource() *resource.Resource     { return s.res }
func (s serviceSpan) Attributes() []attribute.KeyValue { return s.attrs }

type serviceSpanExporter struct{ sdktrace.SpanExporter }

func (e serviceSpanExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	routed := make([]sdktrace.ReadOnlySpan, len(spans))
	resources := map[*resource.Resource]*resource.Resource{}
	for i, span := range spans {
		routed[i] = span
		owner := ""
		for _, a := range span.Attributes() {
			if string(a.Key) == serviceOwnerAttribute {
				owner = a.Value.AsString()
			}
		}
		if owner != MCPService {
			continue
		}
		res, ok := resources[span.Resource()]
		if !ok {
			res = serviceResource(span.Resource(), owner)
			resources[span.Resource()] = res
		}
		attrs := make([]attribute.KeyValue, 0, len(span.Attributes()))
		for _, a := range span.Attributes() {
			if string(a.Key) != serviceOwnerAttribute {
				attrs = append(attrs, a)
			}
		}
		routed[i] = serviceSpan{span, res, attrs}
	}
	return e.SpanExporter.ExportSpans(ctx, routed)
}

type serviceMetricExporter struct{ sdkmetric.Exporter }

func (e serviceMetricExporter) Export(ctx context.Context, data *metricdata.ResourceMetrics) error {
	api := metricdata.ResourceMetrics{Resource: data.Resource}
	mcp := metricdata.ResourceMetrics{Resource: serviceResource(data.Resource, MCPService)}
	for _, scope := range data.ScopeMetrics {
		if scope.Scope.Name == "spaniel/mcp" {
			mcp.ScopeMetrics = append(mcp.ScopeMetrics, scope)
		} else {
			api.ScopeMetrics = append(api.ScopeMetrics, scope)
		}
	}
	var errs []error
	if len(api.ScopeMetrics) > 0 {
		errs = append(errs, e.Exporter.Export(ctx, &api))
	}
	if len(mcp.ScopeMetrics) > 0 {
		errs = append(errs, e.Exporter.Export(ctx, &mcp))
	}
	return errors.Join(errs...)
}
