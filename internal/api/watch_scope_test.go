package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zfogg/spaniel/internal/storage"
)

func TestWatchScopedTraceSpanAndMetadata(t *testing.T) {
	handler, db := setupRouter(t)
	insertSpan(t, db, "first", "reused-trace", "first-span", "", "api", "first")
	insertSpan(t, db, "second", "reused-trace", "second-span", "", "api", "second")
	db.SetActiveSession("second", "second")
	for _, row := range []*storage.SpanEvent{
		{SessionID: "first", TraceID: "reused-trace", SpanID: "first-span", Name: "owned", Attributes: "{}"},
		{SessionID: "second", TraceID: "reused-trace", SpanID: "first-span", Name: "foreign-session", Attributes: "{}"},
		{SessionID: "first", TraceID: "other-trace", SpanID: "first-span", Name: "foreign-trace", Attributes: "{}"},
	} {
		if err := db.InsertSpanEvents([]*storage.SpanEvent{row}); err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range []*storage.SpanLink{
		{SessionID: "first", TraceID: "reused-trace", SpanID: "first-span", LinkedTraceID: "target", LinkedSpanID: "target-span", Attributes: `{"exact":9007199254740993}`},
		{SessionID: "second", TraceID: "reused-trace", SpanID: "first-span", LinkedTraceID: "foreign-session", Attributes: "{}"},
		{SessionID: "first", TraceID: "other-trace", SpanID: "first-span", LinkedTraceID: "foreign-trace", Attributes: "{}"},
	} {
		if err := db.InsertSpanLinks([]*storage.SpanLink{row}); err != nil {
			t.Fatal(err)
		}
	}
	read := func(path string, status int) string {
		t.Helper()
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != status {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		return w.Body.String()
	}
	for _, path := range []string{"/api/spans/first-span?sessionId=first", "/api/spans/first-span", "/api/traces/reused-trace?sessionId=first"} {
		body := read(path, 200)
		if !strings.Contains(body, "owned") || !strings.Contains(body, "target-span") || strings.Contains(body, "foreign-") || strings.Contains(body, "second-span") {
			t.Fatalf("metadata escaped scope: %s", body)
		}
	}
	read("/api/spans/first-span?sessionId=second", 404)
	var mismatch struct {
		Data []storage.Span `json:"data"`
	}
	json.Unmarshal([]byte(read("/api/traces/reused-trace?sessionId=missing", 200)), &mismatch)
	if len(mismatch.Data) != 0 {
		t.Fatal("wrong-session trace returned spans")
	}
	unscoped := read("/api/traces/reused-trace", 200)
	if !strings.Contains(unscoped, "first-span") || !strings.Contains(unscoped, "second-span") {
		t.Fatal("unscoped web behavior changed", unscoped)
	}
	links := read("/api/traces/target/incoming-links?sessionId=first", 200)
	if !strings.Contains(links, `"linked_span_id":"target-span"`) || !strings.Contains(links, "9007199254740993") {
		t.Fatal(links)
	}
	var incoming struct {
		Data []storage.SpanLink `json:"data"`
	}
	json.Unmarshal([]byte(read("/api/traces/target/incoming-links?sessionId=second", 200)), &incoming)
	if len(incoming.Data) != 0 {
		t.Fatal("incoming links escaped session")
	}
}

func TestWatchPinnedPreviewAndReadOnlyScope(t *testing.T) {
	handler, db := setupRouter(t)
	insertSpan(t, db, "selected", "t-selected", "s-selected", "", "api", "pinned-result")
	insertSpan(t, db, "active", "t-active", "s-active", "", "api", "active-result")
	db.SetActiveSession("active", "active")
	d, err := db.CreateDashboard("Session preview", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, selected := range []string{"selected", "", "missing"} {
		body, _ := json.Marshal(map[string]any{"query_sql": "SELECT name, 9007199254740993::BIGINT AS exact FROM spans WHERE session_id = $session_id", "sessionId": selected, "variables": map[string]string{"session_id": "active"}})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/dashboards/"+d.ID+"/query-preview", strings.NewReader(string(body))))
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		switch selected {
		case "selected":
			if !strings.Contains(w.Body.String(), "pinned-result") || strings.Contains(w.Body.String(), "active-result") || !strings.Contains(w.Body.String(), "9007199254740993") {
				t.Fatal(w.Body.String())
			}
		case "":
			if !strings.Contains(w.Body.String(), "active-result") || strings.Contains(w.Body.String(), "pinned-result") {
				t.Fatal(w.Body.String())
			}
		case "missing":
			if strings.Contains(w.Body.String(), "-result") {
				t.Fatal("missing scope fell back", w.Body.String())
			}
		}
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/dashboards/"+d.ID+"/query-preview", strings.NewReader(`{"query_sql":"DELETE FROM spans","sessionId":"selected"}`)))
	if w.Code != 400 || db.ActiveSessionID() != "active" {
		t.Fatal("preview mutated data/session", w.Code)
	}
}

func TestWatchLogTextAndErrorsBeforePagination(t *testing.T) {
	handler, db := setupRouter(t)
	for i := 0; i < 240; i++ {
		severity := 9
		body := "ordinary"
		if i%30 == 0 {
			severity = 21
			body = "needle %_\\ literal"
		}
		if err := db.InsertLog(&storage.Log{SessionID: "selected", TimestampNs: int64(i + 1), Severity: severity, Body: body, Attributes: "{}", ServiceName: "api"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.InsertLog(&storage.Log{SessionID: "foreign", TimestampNs: 999, Severity: 17, Body: "needle %_\\ literal", Attributes: "{}"}); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"text=needle", "severity=error%2B", "text=%25_"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/logs?sessionId=selected&limit=3&page=2&"+q, nil))
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var response struct {
			Data []storage.Log             `json:"data"`
			Meta struct{ Total, Page int } `json:"meta"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if response.Meta.Total != 8 || response.Meta.Page != 2 || len(response.Data) != 3 || response.Data[0].TimestampNs != 121 {
			t.Fatal("count/page applied before filters", w.Body.String())
		}
		for _, row := range response.Data {
			if row.SessionID != "selected" || row.Severity < 17 {
				t.Fatal("scope leak", row)
			}
		}
	}
}

func TestWatchAlertInspectionDoesNotAcknowledgeDefinition(t *testing.T) {
	handler, db := setupRouter(t)
	rule := &storage.AlertRule{ID: "observe", Name: "Observe only", SourceHash: "revision", ConditionJSON: "{}", GroupByJSON: "[]", AnnotationsJSON: "{}"}
	if err := db.CreateAlertRule(rule); err != nil {
		t.Fatal(err)
	}
	read := func(path string) {
		t.Helper()
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	read("/api/alerts/observe?markSeen=false")
	read("/api/alerts/observe?markSeen=0")
	if updated, err := db.DefinitionUpdated("alert", rule.ID, rule.SourceHash, rule.UpdatedAt); err != nil || !updated {
		t.Fatal("watch acknowledged revision", updated, err)
	}
	// Existing unqualified browser callers retain their explicit-access behavior.
	read("/api/alerts/observe")
	if updated, err := db.DefinitionUpdated("alert", rule.ID, rule.SourceHash, rule.UpdatedAt); err != nil || updated {
		t.Fatal("browser behavior changed", updated, err)
	}
	dashboard := &storage.Dashboard{ID: "observe-dashboard", Name: "Observe dashboard", SourceHash: "revision"}
	if err := db.ReplaceDashboardDefinition(dashboard); err != nil {
		t.Fatal(err)
	}
	read("/api/dashboards/observe-dashboard?markSeen=false")
	if updated, err := db.DefinitionUpdated("dashboard", dashboard.ID, dashboard.SourceHash, dashboard.UpdatedAt); err != nil || !updated {
		t.Fatal("watch acknowledged dashboard", updated, err)
	}
	read("/api/dashboards/observe-dashboard")
	if updated, err := db.DefinitionUpdated("dashboard", dashboard.ID, dashboard.SourceHash, dashboard.UpdatedAt); err != nil || updated {
		t.Fatal("dashboard browser behavior changed", updated, err)
	}
}
