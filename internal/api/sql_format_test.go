package api

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFormatSQLAPI(t *testing.T) {
	router := NewRouter(nil, nil, nil)
	for _, tc := range []struct {
		body   string
		status int
	}{
		{`{"sql":"-- keep\nselect a,b from spans where id=$id; -- trailing  "}`, 200},
		{`{"sql":"select ?1,:name from t","dialect":"sqlite"}`, 200},
		{`{"sql":"select id from t where id=?","dialect":"mysql"}`, 200},
		{`{"sql":"select 'unfinished"}`, 400},
		{`{"sql":"select 1","dialect":"typo"}`, 400},
	} {
		req := httptest.NewRequest("POST", "/api/sql/format", strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != tc.status {
			t.Fatalf("%s: %d %s", tc.body, w.Code, w.Body.String())
		}
		if w.Code == 200 {
			var out struct {
				Data struct {
					SQL string `json:"sql"`
				} `json:"data"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.Data.SQL, "\n") {
				t.Fatal(out.Data.SQL)
			}
			if strings.Contains(tc.body, "-- keep") && (!strings.Contains(out.Data.SQL, "-- keep") || !strings.Contains(out.Data.SQL, "$id") || !strings.Contains(out.Data.SQL, "-- trailing  ")) {
				t.Fatal(out.Data.SQL)
			}
		}
	}
}
