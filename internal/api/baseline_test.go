package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zfogg/spaniel/internal/ci"
	"github.com/zfogg/spaniel/internal/storage"
)

// seedSpan inserts one span row for tests.
func seedSpan(t *testing.T, db *storage.DB, sessID, traceID, spanID, parent, svc, name string, dur int64, status int) {
	t.Helper()
	if err := db.InsertSpan(&storage.Span{
		TraceID:      traceID,
		SpanID:       spanID,
		ParentSpanID: parent,
		ServiceName:  svc,
		Name:         name,
		StartNs:      0,
		EndNs:        dur,
		StatusCode:   status,
		Attributes:   "{}",
		Resource:     "{}",
		SessionID:    sessID,
		SessionLabel: sessID,
		ReceivedAt:   time.Now().UnixNano(),
	}); err != nil {
		t.Fatalf("InsertSpan: %v", err)
	}
}

func TestExportBaseline_SessionNotFound(t *testing.T) {
	handler, _ := setupRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/does-not-exist/baseline-export", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestExportBaseline_HappyPath(t *testing.T) {
	handler, db := setupRouter(t)

	sess, err := db.CreateSession("ci-baseline", false)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Root span + two child DB spans + one error span.
	seedSpan(t, db, sess.ID, "t1", "root", "", "api", "GET /cart", 100, 0)
	seedSpan(t, db, sess.ID, "t1", "db-1", "root", "db",  "SELECT",   30, 0)
	seedSpan(t, db, sess.ID, "t1", "db-2", "root", "db",  "SELECT",   50, 0)
	seedSpan(t, db, sess.ID, "t1", "err",  "root", "api", "GET /bad", 10, 2)

	if err := db.UpsertTraceIssue(&storage.TraceIssue{
		ID: "issue-1", TraceID: "t1", SessionID: sess.ID,
		Kind: "n_plus_one", Fingerprint: "SELECT *", Count: 12, WastedNs: 1000,
		CreatedAt: time.Now().UnixNano(),
	}); err != nil {
		t.Fatalf("UpsertTraceIssue: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/"+sess.ID+"/baseline-export", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%s)", w.Code, w.Body.String())
	}

	var resp struct {
		Data ci.Snapshot `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	snap := resp.Data
	if snap.Version != ci.SnapshotVersion {
		t.Errorf("version = %d, want %d", snap.Version, ci.SnapshotVersion)
	}
	if snap.SessionLabel != sess.Label {
		t.Errorf("label = %q, want %q", snap.SessionLabel, sess.Label)
	}
	if snap.SpanCount != 4 || snap.TraceCount != 1 {
		t.Errorf("counts wrong: spans=%d traces=%d", snap.SpanCount, snap.TraceCount)
	}
	if snap.RootDurationNs != 100 {
		t.Errorf("RootDurationNs = %d, want 100", snap.RootDurationNs)
	}
	if len(snap.NPlusOne) != 1 || snap.NPlusOne[0].Count != 12 {
		t.Errorf("expected single N+1 with count 12, got %+v", snap.NPlusOne)
	}
	var foundErr bool
	for _, e := range snap.Errors {
		if e.Service == "api" && e.Name == "GET /bad" {
			foundErr = true
		}
	}
	if !foundErr {
		t.Errorf("expected ERROR op api/GET /bad in snapshot, got %+v", snap.Errors)
	}
}
