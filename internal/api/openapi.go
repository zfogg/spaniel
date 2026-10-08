package api

import (
	"encoding/json"
	"net/http"

	"github.com/zfogg/spaniel/internal/api/apigen"
	"gopkg.in/yaml.v3"
)

func (r *Router) openAPI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/vnd.oai.openapi+json;version=3.1")
	spec, err := apigen.GetSwagger()
	if err != nil {
		http.Error(w, "OpenAPI specification is unavailable", http.StatusInternalServerError)
		return
	}
	// Indentation is intentional: Redoc and humans receive the exact contract
	// used to generate this server in a stable, inspectable form.
	encoded, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		http.Error(w, "OpenAPI specification could not be encoded", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(append(encoded, '\n'))
}

func (r *Router) openAPIYAML(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	spec, err := apigen.GetSwagger()
	if err != nil {
		http.Error(w, "OpenAPI specification is unavailable", http.StatusInternalServerError)
		return
	}
	encoded, err := yaml.Marshal(spec)
	if err != nil {
		http.Error(w, "OpenAPI specification could not be encoded", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(encoded)
}
