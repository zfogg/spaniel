package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zfogg/spaniel/internal/coverage"
	"github.com/zfogg/spaniel/internal/forwarder"
	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/ws"
)

func setupRouterWithManifests(t *testing.T, m *coverage.Manifests) (http.Handler, *storage.DB) {
	t.Helper()
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	sess, _ := store.CreateSession("cov", false)
	store.SetActiveSession(sess.ID, sess.Label)
	handler := NewRouterWithManifests(store, ws.NewHub(), (*forwarder.Forwarder)(nil), m)
	return handler, store
}

func TestGetCoverage_Empty(t *testing.T) {
	handler, _ := setupRouterWithManifests(t, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/coverage", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		Data coverage.Report `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Data.Services) != 0 || resp.Data.Overall.TotalRoutes != 0 {
		t.Errorf("expected empty report, got %+v", resp.Data)
	}
}

func TestGetCoverage_ObservedOnly(t *testing.T) {
	handler, db := setupRouterWithManifests(t, nil)
	sid := db.ActiveSessionID()
	_ = db.InsertSpan(&storage.Span{
		TraceID: "t1", SpanID: "s1", ServiceName: "api", Name: "GET /cart",
		Kind: 2, StartNs: 0, EndNs: 1000, Attributes: `{"http.route":"/api/cart","http.request.method":"GET"}`,
		Resource: "{}", SessionID: sid, ReceivedAt: 1,
	})
	_ = db.InsertSpan(&storage.Span{
		TraceID: "t1", SpanID: "s2", ServiceName: "api", Name: "POST /checkout",
		Kind: 2, StartNs: 0, EndNs: 2000, Attributes: `{"http.route":"/api/checkout","http.request.method":"POST"}`,
		Resource: "{}", SessionID: sid, ReceivedAt: 2,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/coverage", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var resp struct {
		Data coverage.Report `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Data.Services) != 1 || resp.Data.Services[0].Name != "api" {
		t.Fatalf("expected single api service, got %+v", resp.Data.Services)
	}
	if resp.Data.Services[0].ObservedOps != 2 {
		t.Errorf("ops = %d, want 2", resp.Data.Services[0].ObservedOps)
	}
	if resp.Data.Services[0].CoveragePct != 100 {
		t.Errorf("observed-only pct = %v, want 100", resp.Data.Services[0].CoveragePct)
	}
}

func TestGetCoverage_WithManifest_HasDarkRoutes(t *testing.T) {
	m := &coverage.Manifests{
		Spec: "openapi.yaml",
		Routes: map[string][]coverage.ManifestRoute{
			"api": {
				{Method: "GET", Path: "/api/cart"},
				{Method: "POST", Path: "/api/v1/export"},
				{Method: "DELETE", Path: "/api/v1/users/:id"},
			},
		},
	}
	handler, db := setupRouterWithManifests(t, m)
	sid := db.ActiveSessionID()
	_ = db.InsertSpan(&storage.Span{
		TraceID: "t1", SpanID: "s1", ServiceName: "api", Name: "GET /cart",
		Kind: 2, EndNs: 1000, Attributes: `{"http.route":"/api/cart","http.request.method":"GET"}`,
		Resource: "{}", SessionID: sid,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/coverage", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var resp struct {
		Data coverage.Report `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	sc := resp.Data.Services[0]
	if sc.Source != "openapi" || sc.Spec != "openapi.yaml" {
		t.Errorf("source/spec wrong: %+v", sc)
	}
	if sc.TotalRoutes != 3 || len(sc.DarkRoutes) != 2 {
		t.Errorf("expected 3 total / 2 dark, got %+v", sc)
	}
	if resp.Data.Overall.DarkCount != 2 {
		t.Errorf("overall dark = %d, want 2", resp.Data.Overall.DarkCount)
	}
}
