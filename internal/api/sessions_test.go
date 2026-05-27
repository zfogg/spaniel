package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zfogg/spaniel/internal/storage"
)

// sessionRequest is the shape /api/sessions endpoints respond with under "data".
type sessionData struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	IsBaseline bool   `json:"is_baseline"`
}

func decodeData[T any](t *testing.T, body []byte) T {
	t.Helper()
	var resp struct {
		Data T `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode response: %v\nbody: %s", err, body)
	}
	return resp.Data
}

func do(t *testing.T, handler http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body != nil {
		raw, _ := json.Marshal(body)
		r = httptest.NewRequest(method, path, bytes.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

// ── list ─────────────────────────────────────────────────────────────────────

func TestListSessions_Empty(t *testing.T) {
	handler, _ := setupRouter(t)
	w := do(t, handler, http.MethodGet, "/api/sessions", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	data := decodeData[[]sessionData](t, w.Body.Bytes())
	if len(data) != 0 {
		t.Errorf("expected empty, got %d", len(data))
	}
}

func TestListSessions_ReturnsAll(t *testing.T) {
	handler, store := setupRouter(t)
	a, _ := store.CreateSession("alpha", false)
	b, _ := store.CreateSession("beta", true)

	w := do(t, handler, http.MethodGet, "/api/sessions", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	data := decodeData[[]sessionData](t, w.Body.Bytes())
	if len(data) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(data))
	}
	// list is ordered by created_at DESC, so b (newer) comes first.
	if data[0].ID != b.ID || data[1].ID != a.ID {
		t.Errorf("order wrong: got %s, %s", data[0].ID, data[1].ID)
	}
	if !data[0].IsBaseline {
		t.Errorf("expected baseline=true on b")
	}
}

// ── create ───────────────────────────────────────────────────────────────────

func TestCreateSession_NoLabel(t *testing.T) {
	handler, _ := setupRouter(t)
	w := do(t, handler, http.MethodPost, "/api/sessions", map[string]any{})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	got := decodeData[sessionData](t, w.Body.Bytes())
	// CreateSession defaults the label to the id when none is provided.
	if got.Label == "" || got.Label != got.ID {
		t.Errorf("expected label to default to id, got label=%q id=%q", got.Label, got.ID)
	}
}

func TestCreateSession_WithLabel(t *testing.T) {
	handler, _ := setupRouter(t)
	w := do(t, handler, http.MethodPost, "/api/sessions", map[string]any{"label": "my-session"})
	if w.Code != http.StatusOK {
		t.Fatalf("got %d (body=%s)", w.Code, w.Body.String())
	}
	got := decodeData[sessionData](t, w.Body.Bytes())
	if got.Label != "my-session" {
		t.Errorf("label = %q, want my-session", got.Label)
	}
}

// ── activate / active ────────────────────────────────────────────────────────

func TestActivateSession(t *testing.T) {
	handler, store := setupRouter(t)
	s, _ := store.CreateSession("to-activate", false)

	w := do(t, handler, http.MethodPost, "/api/sessions/"+s.ID+"/activate", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if store.ActiveSessionID() != s.ID {
		t.Errorf("ActiveSessionID = %q, want %q", store.ActiveSessionID(), s.ID)
	}
}

func TestActivateSession_NotFound(t *testing.T) {
	handler, _ := setupRouter(t)
	w := do(t, handler, http.MethodPost, "/api/sessions/missing/activate", nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestGetActiveSession(t *testing.T) {
	handler, store := setupRouter(t)
	s, _ := store.CreateSession("the-active-one", false)
	store.SetActiveSession(s.ID, s.Label)

	w := do(t, handler, http.MethodGet, "/api/sessions/active", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d", w.Code)
	}
	got := decodeData[map[string]string](t, w.Body.Bytes())
	if got["id"] != s.ID || got["label"] != "the-active-one" {
		t.Errorf("active session = %+v, want id=%s label=the-active-one", got, s.ID)
	}
}

// ── baseline ─────────────────────────────────────────────────────────────────

func TestBaselineSession_Set(t *testing.T) {
	handler, store := setupRouter(t)
	s, _ := store.CreateSession("not-yet-baseline", false)

	w := do(t, handler, http.MethodPost, "/api/sessions/"+s.ID+"/baseline", map[string]any{"is_baseline": true})
	if w.Code != http.StatusOK {
		t.Fatalf("got %d (body=%s)", w.Code, w.Body.String())
	}
	after, _ := store.GetSession(s.ID)
	if !after.IsBaseline {
		t.Errorf("session not flagged as baseline")
	}
}

func TestBaselineSession_Clear(t *testing.T) {
	handler, store := setupRouter(t)
	s, _ := store.CreateSession("already-baseline", true)

	w := do(t, handler, http.MethodPost, "/api/sessions/"+s.ID+"/baseline", map[string]any{"is_baseline": false})
	if w.Code != http.StatusOK {
		t.Fatalf("got %d", w.Code)
	}
	after, _ := store.GetSession(s.ID)
	if after.IsBaseline {
		t.Errorf("session still flagged as baseline")
	}
}

// ── delete ───────────────────────────────────────────────────────────────────

func TestDeleteSession(t *testing.T) {
	handler, store := setupRouter(t)
	keep, _ := store.CreateSession("keep", false)
	doomed, _ := store.CreateSession("doomed", false)
	// Make `keep` the active one so the delete handler doesn't refuse `doomed`.
	store.SetActiveSession(keep.ID, keep.Label)

	w := do(t, handler, http.MethodDelete, "/api/sessions/"+doomed.ID, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d (body=%s)", w.Code, w.Body.String())
	}
	if s, _ := store.GetSession(doomed.ID); s != nil {
		t.Errorf("session %s still present after delete", doomed.ID)
	}
}

func TestDeleteSession_RefusesActive(t *testing.T) {
	handler, store := setupRouter(t)
	s, _ := store.CreateSession("active", false)
	store.SetActiveSession(s.ID, s.Label)

	w := do(t, handler, http.MethodDelete, "/api/sessions/"+s.ID, nil)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 refusing active-session delete, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "active") {
		t.Errorf("error message should mention 'active', got: %s", w.Body.String())
	}
}

func TestGetSession_NotFound(t *testing.T) {
	handler, _ := setupRouter(t)
	w := do(t, handler, http.MethodGet, "/api/sessions/missing", nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// Compile-time guard: helpers don't accidentally drop the storage import.
var _ = storage.Session{}
