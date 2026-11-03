package traceassert

import (
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// FromReadOnlySpans converts an SDK recorder capture without exporting telemetry.
// It preserves typed attribute arrays, resources, events and causal links. Status
// codes are converted from SDK codes to the OTLP/Spaniel wire numbering.
func FromReadOnlySpans(spans []sdktrace.ReadOnlySpan) []Span {
	out := make([]Span, 0, len(spans))
	for _, s := range spans {
		sc := s.SpanContext()
		row := Span{TraceID: sc.TraceID().String(), SpanID: sc.SpanID().String(), Name: s.Name(), Kind: int(s.SpanKind()), Attributes: attributeMap(s.Attributes())}
		row.StatusMessage = s.Status().Description
		if s.Parent().IsValid() {
			row.ParentSpanID = s.Parent().SpanID().String()
		}
		switch s.Status().Code {
		case codes.Ok:
			row.StatusCode = 1
		case codes.Error:
			row.StatusCode = 2
		}
		if r := s.Resource(); r != nil {
			row.Resource = attributeMap(r.Attributes())
			row.ServiceName, _ = row.Resource["service.name"].(string)
		}
		for _, e := range s.Events() {
			row.Events = append(row.Events, map[string]any{"name": e.Name, "attributes": attributeMap(e.Attributes)})
		}
		for _, l := range s.Links() {
			row.Links = append(row.Links, Link{TraceID: l.SpanContext.TraceID().String(), SpanID: l.SpanContext.SpanID().String(), Attributes: attributeMap(l.Attributes)})
		}
		out = append(out, row)
	}
	return out
}

func attributeMap(values []attribute.KeyValue) map[string]any {
	out := make(map[string]any, len(values))
	for _, v := range values {
		out[string(v.Key)] = v.Value.AsInterface()
	}
	return out
}
