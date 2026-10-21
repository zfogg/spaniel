package api

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRespondErrBoundsRecursiveDatabaseFailure(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/traces/a", nil)
	message := strings.Repeat("Failed: database has been invalidated because of a previous fatal error. Original error: \"", 10000) + "Could not find node in column segment tree" + strings.Repeat("\"", 10000)
	respondErr(w, req, 500, message)
	if w.Code != 500 || w.Body.Len() > 4500 || !strings.Contains(w.Body.String(), "Could not find node in column segment tree") {
		t.Fatalf("status=%d bytes=%d", w.Code, w.Body.Len())
	}
}
