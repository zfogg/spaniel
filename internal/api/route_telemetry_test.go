package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestRouteTemplateTelemetryMiddlewareUsesChiTemplate(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := trace.NewTracerProvider(trace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(t.Context()) })

	router := chi.NewRouter()
	router.Use(routeTemplateTelemetryMiddleware)
	router.Get("/api/traces/{traceID}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })

	req := httptest.NewRequest(http.MethodGet, "/api/traces/abc123", nil)
	ctx, span := provider.Tracer("test").Start(req.Context(), "request")
	router.ServeHTTP(httptest.NewRecorder(), req.WithContext(ctx))
	span.End()

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(spans))
	}
	for _, attr := range spans[0].Attributes() {
		if attr.Key == "http.route" && attr.Value.AsString() == "/api/traces/{traceID}" {
			return
		}
	}
	t.Fatalf("http.route attribute = %#v, want Chi route template", spans[0].Attributes())
}
