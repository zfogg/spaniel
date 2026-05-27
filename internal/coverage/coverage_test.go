package coverage

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/zfogg/spaniel/internal/storage"
)

func span(svc, name string, kind int, attrs string, dur int64) *storage.Span {
	if attrs == "" {
		attrs = "{}"
	}
	return &storage.Span{
		TraceID:     "t1",
		SpanID:      svc + "/" + name,
		ServiceName: svc,
		Name:        name,
		Kind:        kind,
		StartNs:     0,
		EndNs:       dur,
		DurationNs:  dur,
		Attributes:  attrs,
	}
}

// ── extractOperation / Compute basics ─────────────────────────────────────────

func TestCompute_HTTPRouteAndMethod(t *testing.T) {
	spans := []*storage.Span{
		span("api", "GET /products", 2,
			`{"http.route":"/api/products","http.request.method":"GET"}`, 10),
		span("api", "GET /products", 2,
			`{"http.route":"/api/products","http.request.method":"GET"}`, 20),
		span("api", "POST /cart", 2,
			`{"http.route":"/api/cart","http.method":"POST"}`, 30),
	}
	r := Compute(spans, nil)
	if len(r.Services) != 1 || r.Services[0].Name != "api" {
		t.Fatalf("expected single api service, got %+v", r.Services)
	}
	sc := r.Services[0]
	if sc.Source != "observed" {
		t.Errorf("source = %q, want observed", sc.Source)
	}
	if len(sc.ObservedRoutes) != 2 {
		t.Fatalf("expected 2 observed routes, got %d", len(sc.ObservedRoutes))
	}
	// Without a manifest, observed-only ⇒ 100%.
	if sc.CoveragePct != 100 {
		t.Errorf("observed-only coverage should be 100%%, got %v", sc.CoveragePct)
	}
	// http.method (legacy) also accepted.
	for _, r := range sc.ObservedRoutes {
		if r.Path == "/api/cart" && r.Method != "POST" {
			t.Errorf("legacy http.method not extracted, got method=%q", r.Method)
		}
	}
}

func TestCompute_RPCSpans(t *testing.T) {
	spans := []*storage.Span{
		span("cart", "GetCart", 2, `{"rpc.method":"GetCart","rpc.service":"cart.v1"}`, 50),
		span("cart", "AddItem", 2, `{"rpc.method":"AddItem","rpc.service":"cart.v1"}`, 60),
	}
	r := Compute(spans, nil)
	if len(r.Services) != 1 || r.Services[0].ObservedOps != 2 {
		t.Fatalf("rpc grouping wrong: %+v", r.Services)
	}
	for _, route := range r.Services[0].ObservedRoutes {
		if route.Method != "RPC" {
			t.Errorf("rpc method tag = %q, want RPC", route.Method)
		}
		if route.Path != "cart.v1.GetCart" && route.Path != "cart.v1.AddItem" {
			t.Errorf("unexpected rpc path %q", route.Path)
		}
	}
}

func TestCompute_ServerSpanFallback(t *testing.T) {
	// No http/rpc attrs but kind=2 (server) → falls back to span name as path.
	spans := []*storage.Span{span("worker", "consume.kafka.events", 2, "", 10)}
	r := Compute(spans, nil)
	if len(r.Services) != 1 || r.Services[0].ObservedRoutes[0].Path != "consume.kafka.events" {
		t.Errorf("expected fallback to span name, got %+v", r.Services)
	}
}

func TestCompute_SkipsClientAndUnknownSpans(t *testing.T) {
	spans := []*storage.Span{
		span("db", "SELECT", 3, `{"db.statement":"SELECT 1"}`, 10), // client → skipped
		span("api", "internal-op", 1, ``, 5),                       // kind=1 internal, no attrs → skipped
	}
	r := Compute(spans, nil)
	if len(r.Services) != 0 {
		t.Errorf("expected no observed routes, got %+v", r.Services)
	}
}

// ── manifest joins ────────────────────────────────────────────────────────────

func TestCompute_WithManifest_ProducesDarkRoutes(t *testing.T) {
	spans := []*storage.Span{
		span("api", "GET /products", 2, `{"http.route":"/api/products","http.request.method":"GET"}`, 10),
	}
	m := &Manifests{
		Spec: "openapi.yaml",
		Routes: map[string][]ManifestRoute{
			"api": {
				{Method: "GET", Path: "/api/products"},
				{Method: "POST", Path: "/api/v1/export"},
				{Method: "DELETE", Path: "/api/v1/users/:id"},
			},
		},
	}
	r := Compute(spans, m)
	sc := r.Services[0]
	if sc.Source != "openapi" || sc.Spec != "openapi.yaml" {
		t.Errorf("expected openapi source, got %+v", sc)
	}
	if sc.ObservedOps != 1 || sc.TotalRoutes != 3 {
		t.Errorf("ops/total wrong: %+v", sc)
	}
	wantPct := round1(1.0 / 3.0 * 100) // 33.3
	if sc.CoveragePct != wantPct {
		t.Errorf("coverage = %v, want %v", sc.CoveragePct, wantPct)
	}
	if len(sc.DarkRoutes) != 2 {
		t.Fatalf("expected 2 dark routes, got %+v", sc.DarkRoutes)
	}
	for _, r := range sc.DarkRoutes {
		if r.Path == "/api/products" {
			t.Errorf("observed route appeared in dark list: %+v", r)
		}
	}
}

func TestCompute_ManifestServiceWithNoSpans_StaysInReport(t *testing.T) {
	m := &Manifests{
		Spec:   "openapi.yaml",
		Routes: map[string][]ManifestRoute{"api": {{Method: "GET", Path: "/x"}}},
	}
	r := Compute(nil, m)
	if len(r.Services) != 1 {
		t.Fatalf("expected api in report even with zero spans, got %+v", r.Services)
	}
	if r.Services[0].CoveragePct != 0 {
		t.Errorf("expected 0%% coverage, got %v", r.Services[0].CoveragePct)
	}
	if len(r.Services[0].DarkRoutes) != 1 {
		t.Errorf("expected 1 dark route, got %+v", r.Services[0].DarkRoutes)
	}
}

func TestCompute_OverallRollup(t *testing.T) {
	spans := []*storage.Span{
		span("api", "GET /a", 2, `{"http.route":"/a","http.request.method":"GET"}`, 10),
		span("api", "GET /b", 2, `{"http.route":"/b","http.request.method":"GET"}`, 10),
	}
	m := &Manifests{
		Spec: "openapi.yaml",
		Routes: map[string][]ManifestRoute{
			"api": {
				{Method: "GET", Path: "/a"},
				{Method: "GET", Path: "/b"},
				{Method: "GET", Path: "/c"},
				{Method: "GET", Path: "/d"},
			},
		},
	}
	r := Compute(spans, m)
	if r.Overall.ObservedOps != 2 || r.Overall.TotalRoutes != 4 || r.Overall.DarkCount != 2 {
		t.Errorf("overall rollup wrong: %+v", r.Overall)
	}
	if r.Overall.CoveragePct != 50.0 {
		t.Errorf("overall pct = %v, want 50.0", r.Overall.CoveragePct)
	}
}

// ── manifest loader ───────────────────────────────────────────────────────────

func TestLoadManifest_OpenAPIYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "openapi.yaml")
	yaml := `openapi: 3.0.0
info:
  title: api
paths:
  /products:
    get:
      summary: list
    post:
      summary: create
  /cart/{sku}:
    delete: {}
  /ignore-me:
    parameters: []          # not a method, must be skipped
    x-codegen-request-body: stuff
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if m.Spec != "openapi.yaml" {
		t.Errorf("spec filename = %q", m.Spec)
	}
	routes, ok := m.Routes["api"]
	if !ok {
		t.Fatalf("expected service 'api' (from info.title), got keys %v", keys(m.Routes))
	}
	// 3 valid HTTP methods extracted (get, post, delete). parameters / x-… ignored.
	if len(routes) != 3 {
		t.Errorf("expected 3 routes, got %d (%+v)", len(routes), routes)
	}
	methods := map[string]bool{}
	for _, r := range routes {
		methods[r.Method] = true
	}
	if !methods["GET"] || !methods["POST"] || !methods["DELETE"] {
		t.Errorf("missing expected methods: %+v", methods)
	}
}

func TestLoadManifest_FallsBackToFilenameWhenNoTitle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "payment.yaml")
	yaml := `paths:
  /charge:
    post: {}
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	m, _ := LoadManifest(path)
	if _, ok := m.Routes["payment"]; !ok {
		t.Errorf("expected service 'payment' from filename, got %v", keys(m.Routes))
	}
}

func TestLoadManifest_BadFile(t *testing.T) {
	if _, err := LoadManifest("/no/such/file.yaml"); err == nil {
		t.Errorf("expected error for missing file")
	}
}

func keys[K comparable, V any](m map[K]V) []K {
	out := make([]K, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
