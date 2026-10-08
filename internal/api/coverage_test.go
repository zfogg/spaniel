package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/zfogg/spaniel/internal/coverage"
	"github.com/zfogg/spaniel/internal/forwarder"
	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/ws"
)

func TestCoverageSpecsPersistAndMapExplicitService(t *testing.T) {
	handler, db := setupRouterWithManifests(t, nil)
	body := []byte(`{"name":"Spaniel public API","service_name":"spaniel","content":"openapi: 3.1.0\ninfo: {title: Spaniel API, version: '1'}\npaths:\n  /api/coverage:\n    get:\n      responses: {'200': {description: ok}}"}`)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/coverage/specs", bytes.NewReader(body)))
	if w.Code != http.StatusOK {
		t.Fatalf("create spec = %d: %s", w.Code, w.Body.String())
	}
	_ = db.InsertSpan(&storage.Span{TraceID: "t", SpanID: "s", ServiceName: "spaniel", Name: "GET /api/coverage", Kind: 2, StartNs: 1, EndNs: 2, Attributes: `{"http.route":"/api/coverage","http.request.method":"GET"}`, Resource: "{}", SessionID: db.ActiveSessionID()})
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/coverage", nil))
	var out struct {
		Data coverage.Report `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Data.Services) != 1 || out.Data.Services[0].Name != "spaniel" || out.Data.Services[0].CoveragePct != 100 {
		t.Fatalf("coverage = %#v", out.Data)
	}
}

func TestCoverageSpecPersistsAfterStorageReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "coverage.duckdb")
	store, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateCoverageSpec(&storage.CoverageSpec{ID: "spec", Name: "persisted", ServiceName: "spaniel", Format: "openapi", Content: "{}", Digest: "digest", RouteCount: 1, Enabled: true, CreatedAt: 1, UpdatedAt: 1}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	specs, err := reopened.ListCoverageSpecs()
	if err != nil || len(specs) != 1 || specs[0].Name != "persisted" || specs[0].ServiceName != "spaniel" {
		t.Fatalf("reloaded specs = %#v, %v", specs, err)
	}
}

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

func TestGetCoverage_ReportsInstrumentationQuality(t *testing.T) {
	handler, db := setupRouterWithManifests(t, nil)
	sid := db.ActiveSessionID()
	for _, span := range []*storage.Span{
		{TraceID: "t1", SpanID: "missing", ServiceName: "api", Name: "missing route", Kind: 2, StartNs: 1, EndNs: 2, Attributes: `{}`, Resource: `{}`, SessionID: sid},
		{TraceID: "t2", SpanID: "generic", ServiceName: "api", Name: "GET /api", Kind: 2, StartNs: 2, EndNs: 3, Attributes: `{"http.route":"/api/","http.request.method":"GET"}`, Resource: `{}`, SessionID: sid},
		{TraceID: "t3", SpanID: "dynamic", ServiceName: "api", Name: "GET order", Kind: 2, StartNs: 3, EndNs: 4, Attributes: `{"http.route":"/api/orders/123","http.request.method":"GET"}`, Resource: `{}`, SessionID: sid},
	} {
		if err := db.InsertSpan(span); err != nil {
			t.Fatalf("InsertSpan: %v", err)
		}
	}

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/coverage", nil))
	var resp struct {
		Data coverage.Report `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := resp.Data.Quality; got.MissingRouteSpans != 1 || got.GenericRouteSpans != 1 || got.DynamicRouteSpans != 1 {
		t.Fatalf("quality = %+v, want one count in each category", got)
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
