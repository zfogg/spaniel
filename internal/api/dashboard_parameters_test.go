package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/ws"
)

func TestUsesSessionParameter(t *testing.T) {
	for _, query := range []string{"SELECT $session_id", "SELECT $session_id, $session_id", "SELECT '$other', $session_id"} {
		if !usesSessionParameter(query) {
			t.Errorf("missing bind: %s", query)
		}
	}
	for _, query := range []string{
		"SELECT '$session_id'", "SELECT \"$session_id\"", "SELECT $$ $session_id $$",
		"SELECT $text$ $session_id $text$", "SELECT 1 -- $session_id",
		"SELECT /* outer /* $session_id */ comment */ 1", "SELECT $session_id_extra",
		"SELECT E'escaped\\' $session_id'",
	} {
		if usesSessionParameter(query) {
			t.Errorf("false bind: %s", query)
		}
	}
}

func TestDashboardCurrentSessionParameter(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "session-parameters.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	handler := NewRouter(db, ws.NewHub(), nil)
	dashboard, err := db.CreateDashboard("Dynamic session", "")
	if err != nil {
		t.Fatal(err)
	}
	run := func(query string, variables map[string]string) *httptest.ResponseRecorder {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"query_sql": query, "variables": variables})
		req := httptest.NewRequest(http.MethodPost, "/api/dashboards/"+dashboard.ID+"/query-preview", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	for _, session := range []string{"first", "second", "quoted' session", ""} {
		db.SetActiveSession(session, session)
		w := run("SELECT $session_id AS value, $session_id AS repeated", map[string]string{"session_id": "must-not-override"})
		if w.Code != 200 {
			t.Fatalf("%d: %s", w.Code, w.Body.String())
		}
		var result struct {
			Data struct{ Rows []map[string]any }
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if len(result.Data.Rows) != 1 || result.Data.Rows[0]["value"] != session || result.Data.Rows[0]["repeated"] != session {
			t.Fatalf("wrong active session: %s", w.Body.String())
		}
	}
	for _, query := range []string{"SELECT 'pinned' AS value", "SELECT '$session_id' AS value", "SELECT 1 AS value /* $session_id */"} {
		if w := run(query, nil); w.Code != 200 {
			t.Fatalf("literal query changed: %s", w.Body.String())
		}
	}
	if w := run("DELETE FROM spans WHERE session_id = $session_id", nil); w.Code != 400 {
		t.Fatalf("write guard bypassed: %d", w.Code)
	}
}
