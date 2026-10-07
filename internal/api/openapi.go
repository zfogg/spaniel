package api

import (
	"net/http"

	"github.com/zfogg/spaniel/internal/generated"
)

func (r *Router) openAPI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/vnd.oai.openapi+json;version=3.1")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(generated.OpenAPI)
}
