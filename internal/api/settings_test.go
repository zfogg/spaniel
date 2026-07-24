package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/viper"

	"github.com/zfogg/spaniel/internal/forwarder"
	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/ws"
)

// newSettingsRouter spins up an in-memory store + router with a fully
// populated SettingsService. The viper instance is fresh, ConfigPath is a
// temp file so writes don't touch ~/.spaniel.
func newSettingsRouter(t *testing.T) (http.Handler, *SettingsService, *storage.DB) {
	t.Helper()
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	v := viper.New()
	v.Set("port", 8080)
	v.Set("db_path", "/tmp/spaniel.duckdb")
	v.Set("retention_days", 7)
	v.Set("max_sessions", 50)
	v.Set("max_db_size_mb", 500)
	v.Set("auto_prune", true)
	v.Set("otlp_grpc_port", 4317)
	v.Set("otlp_http_port", 4318)
	v.Set("no_browser", false)
	v.Set("forward", []string{})

	svc := &SettingsService{
		Viper:        v,
		ConfigPath:   filepath.Join(t.TempDir(), "config.yaml"),
		Version:      "test-1.2.3",
		StartedAt:    time.Now().Add(-90 * time.Second),
		OTLPGRPCPort: 4317,
		OTLPHTTPPort: 4318,
	}

	router := NewRouterFull(store, ws.NewHub(), (*forwarder.Forwarder)(nil), nil, svc, nil, nil, nil)
	return router, svc, store
}

// ── GET ──────────────────────────────────────────────────────────────────────

func TestGetSettings_HappyPath(t *testing.T) {
	router, _, _ := newSettingsRouter(t)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/settings", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	var resp struct {
		Data SettingsResponse `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Data.Port != 8080 || resp.Data.OTLPGRPCPort != 4317 || resp.Data.OTLPHTTPPort != 4318 {
		t.Errorf("ports wrong: %+v", resp.Data)
	}
	if resp.Data.Runtime.Version != "test-1.2.3" {
		t.Errorf("version = %q", resp.Data.Runtime.Version)
	}
	if resp.Data.Runtime.UptimeNs <= 0 {
		t.Errorf("uptime_ns should be positive, got %d", resp.Data.Runtime.UptimeNs)
	}
	if resp.Data.Runtime.PID != os.Getpid() {
		t.Errorf("pid = %d, want %d", resp.Data.Runtime.PID, os.Getpid())
	}
}

func TestGetSettings_404WhenDisabled(t *testing.T) {
	store, _ := storage.Open(":memory:")
	defer store.Close()
	router := NewRouterWithManifests(store, ws.NewHub(), (*forwarder.Forwarder)(nil), nil)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 without SettingsService, got %d", w.Code)
	}
}

// ── PUT ──────────────────────────────────────────────────────────────────────

func putSettings(t *testing.T, router http.Handler, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestPutSettings_Valid_PersistsAndReturns(t *testing.T) {
	router, svc, _ := newSettingsRouter(t)

	newPort := 9090
	newRet := 14
	newAutoPrune := false
	w := putSettings(t, router, map[string]any{
		"port":           newPort,
		"retention_days": newRet,
		"auto_prune":     newAutoPrune,
		"otlp_grpc_port": 4321,
		"otlp_http_port": 0,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	var resp struct {
		Data SettingsResponse `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Data.Port != newPort || resp.Data.RetentionDays != newRet || resp.Data.AutoPrune != newAutoPrune || resp.Data.OTLPGRPCPort != 4321 || resp.Data.OTLPHTTPPort != 0 {
		t.Errorf("response did not echo update: %+v", resp.Data)
	}
	if svc.Viper.GetInt("port") != newPort {
		t.Errorf("viper not mutated: port = %d", svc.Viper.GetInt("port"))
	}
	if svc.Viper.GetBool("auto_prune") != newAutoPrune {
		t.Errorf("viper not mutated: auto_prune = %v", svc.Viper.GetBool("auto_prune"))
	}
	// File written.
	raw, err := os.ReadFile(svc.ConfigPath)
	if err != nil {
		t.Fatalf("config file not written: %v", err)
	}
	if !bytes.Contains(raw, []byte("9090")) {
		t.Errorf("config file missing new port:\n%s", raw)
	}
}

func TestPutSettings_RejectsInvalidPort(t *testing.T) {
	router, _, _ := newSettingsRouter(t)
	for _, p := range []int{0, -1, 70000} {
		w := putSettings(t, router, map[string]any{"port": p})
		if w.Code != http.StatusBadRequest {
			t.Errorf("port=%d should be rejected, got %d", p, w.Code)
		}
	}
}

func TestPutSettings_AcceptsZeroForReceiverPorts(t *testing.T) {
	router, _, _ := newSettingsRouter(t)
	w := putSettings(t, router, map[string]any{"otlp_grpc_port": 0, "otlp_http_port": 0})
	if w.Code != http.StatusOK {
		t.Errorf("zero receiver ports should be valid (= disabled), got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestPutSettings_HotAppliesStoragePolicy(t *testing.T) {
	router, svc, _ := newSettingsRouter(t)
	var gotMaxMB int
	var gotAutoPrune bool
	var calls int
	svc.SetStoragePolicy = func(maxDBSizeMB int, autoPrune bool) {
		calls++
		gotMaxMB = maxDBSizeMB
		gotAutoPrune = autoPrune
	}

	w := putSettings(t, router, map[string]any{
		"max_db_size_mb": 250,
		"auto_prune":     false,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	if calls != 1 || gotMaxMB != 250 || gotAutoPrune {
		t.Fatalf("storage policy callback: calls=%d max_mb=%d auto_prune=%v", calls, gotMaxMB, gotAutoPrune)
	}
}

func TestPutSettings_RejectsNegativeRetention(t *testing.T) {
	router, _, _ := newSettingsRouter(t)
	w := putSettings(t, router, map[string]any{"retention_days": -1})
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for retention_days=-1, got %d", w.Code)
	}
}

func TestPutSettings_RejectsBadForwardURL(t *testing.T) {
	router, _, _ := newSettingsRouter(t)
	w := putSettings(t, router, map[string]any{"forward": []string{"tempo:4318"}})
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for forward URL without scheme, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestPutSettings_AcceptsHTTPSForward(t *testing.T) {
	router, _, _ := newSettingsRouter(t)
	w := putSettings(t, router, map[string]any{"forward": []string{"https://tempo.example.com:4318"}})
	if w.Code != http.StatusOK {
		t.Errorf("https forward should be accepted, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestPutSettings_PartialUpdatesLeaveOthersAlone(t *testing.T) {
	router, svc, _ := newSettingsRouter(t)
	w := putSettings(t, router, map[string]any{"port": 9999})
	if w.Code != http.StatusOK {
		t.Fatalf("got %d", w.Code)
	}
	// Untouched.
	if svc.Viper.GetInt("retention_days") != 7 {
		t.Errorf("retention_days changed unexpectedly: %d", svc.Viper.GetInt("retention_days"))
	}
}

func TestPutSettings_RejectsInvalidBind(t *testing.T) {
	router, _, _ := newSettingsRouter(t)
	cases := []map[string]any{
		{"bind_address_v4": "localhost"},   // not an IP
		{"bind_address_v4": "0.0.0.0:80"},  // host:port, not a bare IP
		{"bind_address_v4": "::1"},         // IPv6 in the v4 field
		{"bind_address_v4": "999.1.1.1"},   // out of range
		{"bind_address_v6": "127.0.0.1"},   // IPv4 in the v6 field
		{"bind_address_v6": "not-an-addr"}, // garbage
	}
	for _, bad := range cases {
		w := putSettings(t, router, bad)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%v should be rejected, got %d (body=%s)", bad, w.Code, w.Body.String())
		}
	}
}

func TestPutSettings_AcceptsBindOptions(t *testing.T) {
	cases := []struct{ key, val string }{
		{"bind_address_v4", "127.0.0.1"},
		{"bind_address_v4", "0.0.0.0"},
		{"bind_address_v4", "192.168.1.50"},
		{"bind_address_v4", ""}, // empty disables IPv4
		{"bind_address_v6", "::1"},
		{"bind_address_v6", "::"},
		{"bind_address_v6", "fe80::1"},
		{"bind_address_v6", ""}, // empty disables IPv6
	}
	for _, c := range cases {
		router, svc, _ := newSettingsRouter(t)
		w := putSettings(t, router, map[string]any{c.key: c.val})
		if w.Code != http.StatusOK {
			t.Errorf("%s=%q should be accepted, got %d (body=%s)", c.key, c.val, w.Code, w.Body.String())
		}
		if got := svc.Viper.GetString(c.key); got != c.val {
			t.Errorf("viper not mutated for %s=%q: got %q", c.key, c.val, got)
		}
	}
}

func TestPutSettings_RejectsOutOfRangeSample(t *testing.T) {
	router, _, _ := newSettingsRouter(t)
	for _, bad := range []float64{-0.1, 1.5, 2, -1} {
		w := putSettings(t, router, map[string]any{"forward_sample": bad})
		if w.Code != http.StatusBadRequest {
			t.Errorf("forward_sample=%v should be rejected, got %d (body=%s)", bad, w.Code, w.Body.String())
		}
	}
}

func TestPutSettings_AcceptsSampleInRange(t *testing.T) {
	for _, ok := range []float64{0, 0.1, 0.5, 1.0} {
		router, svc, _ := newSettingsRouter(t)
		w := putSettings(t, router, map[string]any{"forward_sample": ok})
		if w.Code != http.StatusOK {
			t.Errorf("forward_sample=%v should be accepted, got %d (body=%s)", ok, w.Code, w.Body.String())
		}
		if got := svc.Viper.GetFloat64("forward_sample"); got != ok {
			t.Errorf("viper not mutated for forward_sample=%v: got %v", ok, got)
		}
	}
}

// ── DELETE /api/settings/data ────────────────────────────────────────────────

func TestDropAllData_WipesStore(t *testing.T) {
	router, _, store := newSettingsRouter(t)
	sess, _ := store.CreateSession("doomed", false)
	store.SetActiveSession(sess.ID, sess.Label)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/api/settings/data", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	sessions, _ := store.ListSessions()
	if len(sessions) != 0 {
		t.Errorf("expected sessions wiped, got %d", len(sessions))
	}
}

// ── POST /api/settings/prune ───────────────────────────────────────────────

func TestPrune_RunsRetentionAndProtectsActive(t *testing.T) {
	router, svc, store := newSettingsRouter(t)
	// Tighten the policy to keep at most 2 sessions.
	svc.Viper.Set("max_sessions", 2)
	svc.Viper.Set("retention_days", 0)
	svc.Viper.Set("max_db_size_mb", 0)

	// Create 4 sessions; mark the first one active so prune must keep it.
	var firstID string
	for i := 0; i < 4; i++ {
		s, err := store.CreateSession("sess", false)
		if err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
		if i == 0 {
			firstID = s.ID
			store.SetActiveSession(s.ID, s.Label)
		}
	}

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/settings/prune", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Data storage.PruneResult `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse: %v", err)
	}
	// 4 sessions, cap 2 → 2 deleted by count, 2 remain.
	if resp.Data.DeletedByCount != 2 {
		t.Errorf("deleted_by_count: want 2, got %d", resp.Data.DeletedByCount)
	}
	if resp.Data.FinalSessions != 2 {
		t.Errorf("final_sessions: want 2, got %d", resp.Data.FinalSessions)
	}

	// The active session must survive the prune.
	sessions, err := store.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	found := false
	for _, s := range sessions {
		if s.ID == firstID {
			found = true
		}
	}
	if !found {
		t.Errorf("active session %s was pruned — it must be protected", firstID)
	}
}

func TestPrune_NoSettingsService404(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	// Router with nil settings service.
	router := NewRouterFull(store, ws.NewHub(), (*forwarder.Forwarder)(nil), nil, nil, nil, nil, nil)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/settings/prune", nil))
	if w.Code != http.StatusNotFound {
		t.Errorf("want 404 when settings service is nil, got %d", w.Code)
	}
}

// ── isNewer ──────────────────────────────────────────────────────────────────

func TestIsNewer(t *testing.T) {
	cases := []struct {
		current string
		latest  string
		want    bool
	}{
		{"0.4.2", "0.5.0", true},
		{"0.5.0", "0.5.0", false},
		{"0.5.0", "0.4.9", false},
		{"dev", "0.5.0", false},
		{"0.4.2", "dev", false},
		{"1.0.0", "1.0.1", true},
	}
	for _, tc := range cases {
		got := isNewer(tc.current, tc.latest)
		if got != tc.want {
			t.Errorf("isNewer(%q, %q) = %v, want %v", tc.current, tc.latest, got, tc.want)
		}
	}
}

// ── POST /api/settings/check-updates ─────────────────────────────────────────

func TestCheckUpdates_PrefersCache(t *testing.T) {
	hitCount := 0
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"tag_name":"v0.5.0","html_url":"https://github.com/zfogg/spaniel/releases/tag/v0.5.0"}`)) //nolint:errcheck
	}))
	defer stub.Close()

	router, svc, _ := newSettingsRouter(t)
	// Override version so is_outdated can fire.
	svc.Version = "0.4.2"
	// Inject a client that redirects github.com calls to our stub.
	svc.GithubClient = &http.Client{
		Transport: &redirectTransport{base: stub.URL},
	}

	doPost := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/settings/check-updates", nil))
		return w
	}

	w1 := doPost()
	if w1.Code != http.StatusOK {
		t.Fatalf("first call: want 200, got %d (body=%s)", w1.Code, w1.Body.String())
	}
	var resp1 struct {
		Data UpdateCheckResult `json:"data"`
	}
	if err := json.Unmarshal(w1.Body.Bytes(), &resp1); err != nil {
		t.Fatalf("unmarshal first: %v", err)
	}
	if !resp1.Data.IsOutdated {
		t.Errorf("is_outdated should be true for 0.4.2 vs 0.5.0")
	}
	if resp1.Data.Latest != "0.5.0" {
		t.Errorf("latest = %q, want 0.5.0", resp1.Data.Latest)
	}

	// Second call — should use cache, not hit the stub again.
	w2 := doPost()
	if w2.Code != http.StatusOK {
		t.Fatalf("second call: want 200, got %d (body=%s)", w2.Code, w2.Body.String())
	}
	if hitCount != 1 {
		t.Errorf("stub hit count = %d, want 1 (second call should use cache)", hitCount)
	}
}

func TestCheckUpdates_HandlesOffline(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer stub.Close()

	router, svc, _ := newSettingsRouter(t)
	svc.Version = "0.4.2"
	svc.GithubClient = &http.Client{
		Transport: &redirectTransport{base: stub.URL},
	}

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/settings/check-updates", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("want 200 (graceful degrade), got %d (body=%s)", w.Code, w.Body.String())
	}
	var resp struct {
		Data UpdateCheckResult `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Data.Error == "" {
		t.Errorf("expected non-empty error field on GitHub failure")
	}
}

// redirectTransport rewrites requests to api.github.com so they hit base instead.
type redirectTransport struct {
	base string
}

func (rt *redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	newURL := rt.base + req.URL.Path
	if req.URL.RawQuery != "" {
		newURL += "?" + req.URL.RawQuery
	}
	newReq, err := http.NewRequestWithContext(req.Context(), req.Method, newURL, req.Body)
	if err != nil {
		return nil, err
	}
	newReq.Header = req.Header
	return http.DefaultTransport.RoundTrip(newReq)
}
