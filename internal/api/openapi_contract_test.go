package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/go-chi/chi/v5"
	"github.com/zfogg/spaniel/internal/api/apigen"
)

// TestOpenAPIContract verifies that the public, pretty-printed document is the
// embedded source used by oapi-codegen and that every documented operation is
// registered by the generated Chi server.
func TestOpenAPIContract(t *testing.T) {
	handler, _ := setupRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/api/openapi.json", nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("GET /api/openapi.json = %d", res.Code)
	}
	if got := res.Header().Get("Content-Type"); !strings.Contains(got, "openapi+json") {
		t.Fatalf("content type = %q", got)
	}
	if !strings.Contains(res.Body.String(), "\n  \"openapi\":") {
		t.Fatal("spec is not two-space pretty-printed")
	}
	var served openapi3.T
	if err := json.Unmarshal(res.Body.Bytes(), &served); err != nil {
		t.Fatalf("served OpenAPI is invalid JSON: %v", err)
	}
	generated, err := apigen.GetSwagger()
	if err != nil {
		t.Fatalf("generated OpenAPI unavailable: %v", err)
	}
	if served.OpenAPI != generated.OpenAPI || len(served.Paths.Map()) != len(generated.Paths.Map()) {
		t.Fatal("served spec differs from generated embedded contract")
	}
	routes, ok := handler.(chi.Routes)
	if !ok {
		t.Fatalf("router %T does not expose Chi routes", handler)
	}
	registered := map[string]bool{}
	for _, route := range routes.Routes() {
		for method := range route.Handlers {
			registered[method+" "+route.Pattern] = true
		}
	}
	for path, item := range generated.Paths.Map() {
		for method, operation := range item.Operations() {
			if !registered[strings.ToUpper(method)+" "+path] {
				t.Errorf("documented operation %s %s is not registered", strings.ToUpper(method), path)
			}
			if len(operation.Tags) == 0 {
				t.Errorf("documented operation %s %s has no Redoc group", strings.ToUpper(method), path)
			}
			response := operation.Responses.Value("200")
			if response == nil || response.Value == nil {
				continue
			}
			jsonResponse := response.Value.Content.Get("application/json")
			if jsonResponse != nil && len(jsonResponse.Examples) == 0 {
				t.Errorf("documented operation %s %s has no response example", strings.ToUpper(method), path)
			}
			if jsonResponse != nil && jsonResponse.Schema != nil && jsonResponse.Schema.Ref == "#/components/schemas/Envelope" {
				t.Errorf("documented operation %s %s still uses the untyped Envelope response", strings.ToUpper(method), path)
			}
		}
	}
	if len(generated.Tags) == 0 {
		t.Fatal("OpenAPI document has no Redoc group definitions")
	}
}
