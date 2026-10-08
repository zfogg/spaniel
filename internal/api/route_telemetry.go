package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel/attribute"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// routeTemplateTelemetryMiddleware updates the active otelhttp server span
// after Chi resolves its route. This keeps http.route low-cardinality and
// makes Coverage report endpoint templates rather than the outer API mount.
func routeTemplateTelemetryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
		if rc := chi.RouteContext(r.Context()); rc != nil && rc.RoutePattern() != "" {
			route := rc.RoutePattern()
			span := oteltrace.SpanFromContext(r.Context())
			span.SetAttributes(attribute.String("http.route", route))
			span.SetName(r.Method + " " + route)
		}
	})
}
