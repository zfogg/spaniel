package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/zfogg/spaniel/internal/storage"
)

// sessionStub records every inbound request so tests can assert path,
// method, and body in one place. /api/sessions, /api/sessions/active,
// /api/sessions/:id/activate, /api/sessions/:id/baseline, and DELETE
// /api/sessions/:id are all served from the same in-memory model.
type recordedReq struct {
	method string
	path   string
	body   []byte
}

type sessionStub struct {
	mu       sync.Mutex
	server   *httptest.Server
	requests []recordedReq
	sessions map[string]*storage.Session
	active   string
	// deleteResponse overrides the default 200 on DELETE so we can simulate
	// the server refusing to delete the active session.
	deleteStatus int
	deleteError  string
}

func newSessionStub(t *testing.T, sessions []storage.Session, active string) *sessionStub {
	t.Helper()
	s := &sessionStub{
		sessions:     map[string]*storage.Session{},
		active:       active,
		deleteStatus: 200,
	}
	for i := range sessions {
		s.sessions[sessions[i].ID] = &sessions[i]
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/sessions", func(w http.ResponseWriter, r *http.Request) {
		s.record(r)
		out := make([]storage.Session, 0, len(s.sessions))
		for _, sess := range s.sessions {
			out = append(out, *sess)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": out})
	})
	mux.HandleFunc("/api/sessions/active", func(w http.ResponseWriter, r *http.Request) {
		s.record(r)
		if active := s.sessions[s.active]; active != nil {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]string{"id": active.ID, "label": active.Label},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{}})
	})
	mux.HandleFunc("/api/sessions/", func(w http.ResponseWriter, r *http.Request) {
		s.record(r)
		// Routes: /api/sessions/:id/activate | /baseline | (DELETE)
		path := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
		parts := strings.SplitN(path, "/", 2)
		id := parts[0]
		sess, ok := s.sessions[id]
		if !ok {
			http.NotFound(w, r)
			return
		}
		switch {
		case len(parts) == 2 && parts[1] == "activate" && r.Method == http.MethodPost:
			s.active = id
			_ = json.NewEncoder(w).Encode(map[string]any{"data": sess})
		case len(parts) == 2 && parts[1] == "baseline" && r.Method == http.MethodPost:
			var body struct{ IsBaseline bool `json:"is_baseline"` }
			_ = json.NewDecoder(r.Body).Decode(&body)
			sess.IsBaseline = body.IsBaseline
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]bool{"ok": true}})
		case r.Method == http.MethodDelete:
			if s.deleteStatus != 200 {
				w.WriteHeader(s.deleteStatus)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": s.deleteError})
				return
			}
			delete(s.sessions, id)
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]bool{"ok": true}})
		default:
			http.NotFound(w, r)
		}
	})
	s.server = httptest.NewServer(mux)
	t.Cleanup(s.server.Close)
	return s
}

func (s *sessionStub) record(r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	// Reset r.Body so downstream handlers can still decode it.
	r.Body = io.NopCloser(bytes.NewReader(body))
	s.mu.Lock()
	s.requests = append(s.requests, recordedReq{r.Method, r.URL.Path, body})
	s.mu.Unlock()
}

func (s *sessionStub) findRequest(method, path string) *recordedReq {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.requests {
		if s.requests[i].method == method && s.requests[i].path == path {
			return &s.requests[i]
		}
	}
	return nil
}

// ── tests ──────────────────────────────────────────────────────────────────

func TestSessionListData_HitsSessionsEndpoint(t *testing.T) {
	stub := newSessionStub(t, []storage.Session{
		{ID: "sess-aaaaaaaaa", Label: "main", IsBaseline: true, TraceCount: 5, SpanCount: 24},
		{ID: "sess-bbbbbbbbb", Label: "feat-foo", IsImported: false, TraceCount: 2, SpanCount: 9},
	}, "sess-aaaaaaaaa")

	sessions, activeID, err := sessionListData(stub.server.URL)
	if err != nil {
		t.Fatalf("sessionListData: %v", err)
	}
	if stub.findRequest("GET", "/api/sessions") == nil {
		t.Errorf("expected GET /api/sessions")
	}
	if activeID != "sess-aaaaaaaaa" {
		t.Errorf("activeID = %q, want sess-aaaaaaaaa", activeID)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}

	// Drive the model's view rendering — independent of the HTTP layer.
	m := newSessionListModel(sessions, activeID, stub.server.URL)
	out := m.View()
	for _, want := range []string{"main", "feat-foo", "baseline", "*"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q\n--- got ---\n%s", want, out)
		}
	}
}

func TestSessionActivate_ResolvesLabelAndPosts(t *testing.T) {
	stub := newSessionStub(t, []storage.Session{
		{ID: "sess-target0", Label: "feat-bar"},
	}, "")

	var buf bytes.Buffer
	if err := sessionActivate(stub.server.URL, "feat-bar", &buf); err != nil {
		t.Fatalf("sessionActivate: %v", err)
	}
	if stub.findRequest("POST", "/api/sessions/sess-target0/activate") == nil {
		t.Errorf("expected POST /api/sessions/sess-target0/activate (label resolved to id)")
	}
	if stub.active != "sess-target0" {
		t.Errorf("stub.active = %q, want sess-target0", stub.active)
	}
	if !strings.Contains(buf.String(), "activated session") {
		t.Errorf("missing confirmation in output: %q", buf.String())
	}
}

func TestSessionBaseline_TogglesCurrentFlag(t *testing.T) {
	stub := newSessionStub(t, []storage.Session{
		{ID: "sess-baseline", Label: "main", IsBaseline: false},
	}, "sess-baseline")

	// First call should mark baseline = true.
	if err := sessionBaseline(stub.server.URL, "main", &bytes.Buffer{}); err != nil {
		t.Fatalf("baseline on: %v", err)
	}
	req := stub.findRequest("POST", "/api/sessions/sess-baseline/baseline")
	if req == nil {
		t.Fatalf("expected POST /baseline")
	}
	if !bytes.Contains(req.body, []byte(`"is_baseline":true`)) {
		t.Errorf("first call should toggle to true, got body=%s", req.body)
	}
	// Second call should toggle back to false (stub mutated IsBaseline).
	if err := sessionBaseline(stub.server.URL, "main", &bytes.Buffer{}); err != nil {
		t.Fatalf("baseline off: %v", err)
	}
	// Find the most recent /baseline request.
	var last *recordedReq
	for i := range stub.requests {
		if stub.requests[i].method == "POST" && strings.HasSuffix(stub.requests[i].path, "/baseline") {
			last = &stub.requests[i]
		}
	}
	if last == nil || !bytes.Contains(last.body, []byte(`"is_baseline":false`)) {
		t.Errorf("second call should toggle to false, got body=%s", last.body)
	}
}

func TestSessionBaseline_DefaultsToActive(t *testing.T) {
	stub := newSessionStub(t, []storage.Session{
		{ID: "sess-active", Label: "active-one"},
		{ID: "sess-other",  Label: "other"},
	}, "sess-active")

	// No ref → uses active session.
	if err := sessionBaseline(stub.server.URL, "", &bytes.Buffer{}); err != nil {
		t.Fatalf("baseline (active default): %v", err)
	}
	if stub.findRequest("POST", "/api/sessions/sess-active/baseline") == nil {
		t.Errorf("expected baseline to target the active session")
	}
}

func TestSessionDelete_OK(t *testing.T) {
	stub := newSessionStub(t, []storage.Session{
		{ID: "sess-doomed", Label: "throwaway"},
	}, "")

	var buf bytes.Buffer
	if err := sessionDelete(stub.server.URL, "throwaway", &buf); err != nil {
		t.Fatalf("sessionDelete: %v", err)
	}
	if stub.findRequest("DELETE", "/api/sessions/sess-doomed") == nil {
		t.Errorf("expected DELETE /api/sessions/sess-doomed")
	}
	if !strings.Contains(buf.String(), "deleted session") {
		t.Errorf("missing confirmation: %q", buf.String())
	}
}

func TestSessionDelete_SurfacesServerRefusal(t *testing.T) {
	stub := newSessionStub(t, []storage.Session{
		{ID: "sess-active", Label: "main"},
	}, "sess-active")
	stub.deleteStatus = 400
	stub.deleteError = "cannot delete the active session"

	err := sessionDelete(stub.server.URL, "main", &bytes.Buffer{})
	if err == nil {
		t.Fatalf("expected refusal error")
	}
	if !strings.Contains(err.Error(), "cannot delete the active session") {
		t.Errorf("error did not surface server message, got %v", err)
	}
}

func TestResolveSessionRefFull_UnknownRef(t *testing.T) {
	stub := newSessionStub(t, []storage.Session{
		{ID: "x", Label: "x"},
	}, "")
	_, err := resolveSessionRefFull(stub.server.URL, "missing")
	if err == nil || !strings.Contains(err.Error(), "no session") {
		t.Errorf("expected 'no session' error, got %v", err)
	}
}
