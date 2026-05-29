package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// doRaw issues a request with a raw (possibly malformed) body, bypassing the
// JSON marshalling in do().
func doRaw(t *testing.T, handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func TestCreateSession_LabelTooLong(t *testing.T) {
	handler, _ := setupRouter(t)
	w := do(t, handler, http.MethodPost, "/api/sessions", map[string]any{
		"label": strings.Repeat("x", 201),
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for over-long label, got %d (body: %s)", w.Code, w.Body)
	}
}

func TestCreateSession_MalformedJSON(t *testing.T) {
	handler, _ := setupRouter(t)
	w := doRaw(t, handler, http.MethodPost, "/api/sessions", "{not valid json")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed JSON, got %d", w.Code)
	}
}

func TestCreateSession_EmptyBodyAllowed(t *testing.T) {
	// An empty body must still succeed (label defaults to the session id),
	// preserving the previous tolerant behaviour.
	handler, _ := setupRouter(t)
	w := do(t, handler, http.MethodPost, "/api/sessions", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for empty body, got %d (body: %s)", w.Code, w.Body)
	}
}

func TestPatchSession_NoteTooLong(t *testing.T) {
	handler, store := setupRouter(t)
	s, _ := store.CreateSession("s", false)
	w := do(t, handler, http.MethodPatch, "/api/sessions/"+s.ID, map[string]any{
		"note": strings.Repeat("x", 10001),
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for over-long note, got %d (body: %s)", w.Code, w.Body)
	}
}

func TestPatchSession_LabelTooLong(t *testing.T) {
	handler, store := setupRouter(t)
	s, _ := store.CreateSession("s", false)
	w := do(t, handler, http.MethodPatch, "/api/sessions/"+s.ID, map[string]any{
		"label": strings.Repeat("x", 201),
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for over-long label, got %d (body: %s)", w.Code, w.Body)
	}
}

func TestBaselineSession_MalformedJSON(t *testing.T) {
	handler, store := setupRouter(t)
	s, _ := store.CreateSession("s", false)
	w := doRaw(t, handler, http.MethodPost, "/api/sessions/"+s.ID+"/baseline", "garbage")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed JSON, got %d", w.Code)
	}
}
