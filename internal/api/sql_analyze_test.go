package api

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSQLAnalyzeAPIEditorAndTelemetryBoundaries(t *testing.T) {
	router := NewRouter(nil, nil, nil)
	for _, tc := range []struct {
		body   string
		status int
		raw    bool
	}{
		{`{"sql":"SELECT \"😀\",\r\n(1","source":"original","dialect":"postgresql"}`, 200, true},
		{`{"sql":"SELECT 'private-value","source":"telemetry","dialect":"postgresql"}`, 200, false},
		{`{"sql":"SELECT 1","source":"telemetry","diagnostics":[{}]}`, 400, false},
		{`{"sql":"SELECT 1","source":"telemetry","diagnostics":[],"diagnostic_schema_version":9}`, 400, false},
		{`{"sql":"SELECT 1","source":"typo"}`, 400, false},
	} {
		req := httptest.NewRequest("POST", "/api/sql/analyze", strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != tc.status {
			t.Fatalf("want %d got %d: %s", tc.status, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "private-value") {
			t.Fatal("telemetry SQL secret in response")
		}
		if tc.raw {
			var out struct {
				Data struct {
					SQL         string           `json:"sql"`
					Diagnostics []map[string]any `json:"diagnostics"`
				} `json:"data"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
			if out.Data.SQL != "SELECT \"😀\",\r\n(1" || len(out.Data.Diagnostics) != 1 || out.Data.Diagnostics[0]["display_start"] == nil {
				t.Fatal("editor map contract missing")
			}
		}
	}
}
