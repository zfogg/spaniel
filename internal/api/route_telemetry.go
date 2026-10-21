package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/zfogg/spaniel/internal/telemetry"
)

// Finalize the existing server span once Chi has resolved the route template.
var routeTemplateTelemetryMiddleware = telemetry.HTTP.RouteMiddleware(func(r *http.Request) string {
	if rc := chi.RouteContext(r.Context()); rc != nil {
		return rc.RoutePattern()
	}
	return ""
})
