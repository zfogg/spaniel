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
	v.Set("otlp_grpc_port", 4317)
	v.Set("otlp_http_port", 4318)
	v.Set("no_browser", false)
	v.Set("forward", []string{})

	svc := &SettingsService{
		Viper:      v,
		ConfigPath: filepath.Join(t.TempDir(), "config.yaml"),
		Version:    "test-1.2.3",
		StartedAt:  time.Now().Add(-90 * time.Second),
		OTLPGRPCPort:   4317,
		OTLPHTTPPort:   4318,
	}

	router := NewRouterFull(store, ws.NewHub(), (*forwarder.Forwarder)(nil), nil, svc)
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
	w := putSettings(t, router, map[string]any{
		"port":           newPort,
		"retention_days": newRet,
		"otlp_grpc_port":      4321,
		"otlp_http_port":      0,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	var resp struct {
		Data SettingsResponse `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Data.Port != newPort || resp.Data.RetentionDays != newRet || resp.Data.OTLPGRPCPort != 4321 || resp.Data.OTLPHTTPPort != 0 {
		t.Errorf("response did not echo update: %+v", resp.Data)
	}
	if svc.Viper.GetInt("port") != newPort {
		t.Errorf("viper not mutated: port = %d", svc.Viper.GetInt("port"))
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
	for _, bad := range []string{"localhost", "1.2.3.4", "", "0.0.0.0:80"} {
		w := putSettings(t, router, map[string]any{"bind_address": bad})
		if w.Code != http.StatusBadRequest {
			t.Errorf("bind_address=%q should be rejected, got %d (body=%s)", bad, w.Code, w.Body.String())
		}
	}
}

func TestPutSettings_AcceptsBindOptions(t *testing.T) {
	for _, ok := range []string{"127.0.0.1", "0.0.0.0", "::1"} {
		router, svc, _ := newSettingsRouter(t)
		w := putSettings(t, router, map[string]any{"bind_address": ok})
		if w.Code != http.StatusOK {
			t.Errorf("bind_address=%q should be accepted, got %d (body=%s)", ok, w.Code, w.Body.String())
		}
		if got := svc.Viper.GetString("bind_address"); got != ok {
			t.Errorf("viper not mutated for bind_address=%q: got %q", ok, got)
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
