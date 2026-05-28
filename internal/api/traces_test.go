package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"

	"github.com/zfogg/spaniel/internal/importer"
	"github.com/zfogg/spaniel/internal/storage"
)

func insertSpanForExport(t *testing.T, store *storage.DB, sessID, traceID, spanID, parent, svc, resource, name string, startNs, endNs int64, attrs string) {
	t.Helper()
	if err := store.InsertSpan(&storage.Span{
		TraceID:      traceID,
		SpanID:       spanID,
		ParentSpanID: parent,
		ServiceName:  svc,
		Name:         name,
		Kind:         1,
		StartNs:      startNs,
		EndNs:        endNs,
		StatusCode:   1,
		Attributes:   attrs,
		Resource:     resource,
		SessionID:    sessID,
		SessionLabel: sessID,
		ReceivedAt:   time.Now().UnixNano(),
	}); err != nil {
		t.Fatalf("InsertSpan: %v", err)
	}
}

func TestGetTraceExport_ReturnsOTLPJSON(t *testing.T) {
	handler, store := setupRouter(t)

	const traceID = "aabbccdd11223344aabbccdd11223344"
	svcARes := `{"service.name":"svc-a"}`
	svcBRes := `{"service.name":"svc-b"}`

	// 2 spans in svc-a, 1 span in svc-b → 2 resourceSpans blocks
	insertSpanForExport(t, store, "sess", traceID, "1122334455667788", "0000000000000000", "svc-a", svcARes, "root",   0, 1_000, "{}")
	insertSpanForExport(t, store, "sess", traceID, "aabbccddeeff0011", "1122334455667788", "svc-a", svcARes, "child",  0, 500,   "{}")
	insertSpanForExport(t, store, "sess", traceID, "99887766554433aa", "1122334455667788", "svc-b", svcBRes, "remote", 0, 300,   `{"db.statement":"SELECT 1"}`)

	req := httptest.NewRequest(http.MethodGet, "/api/traces/"+traceID+"/export", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}
	cd := w.Header().Get("Content-Disposition")
	if !strings.Contains(cd, "attachment") || !strings.Contains(cd, "trace-") {
		t.Errorf("unexpected Content-Disposition: %q", cd)
	}

	// Parse the response as OTLP JSON.
	exportReq := ptraceotlp.NewExportRequest()
	if err := exportReq.UnmarshalJSON(w.Body.Bytes()); err != nil {
		t.Fatalf("failed to parse OTLP JSON: %v", err)
	}

	td := exportReq.Traces()
	if td.ResourceSpans().Len() != 2 {
		t.Errorf("expected 2 resourceSpans, got %d", td.ResourceSpans().Len())
	}

	// Count total spans across all resourceSpans.
	total := 0
	for i := range td.ResourceSpans().Len() {
		rs := td.ResourceSpans().At(i)
		for j := range rs.ScopeSpans().Len() {
			total += rs.ScopeSpans().At(j).Spans().Len()
		}
	}
	if total != 3 {
		t.Errorf("expected 3 total spans, got %d", total)
	}
}

func TestGetTraceExport_NotFound(t *testing.T) {
	handler, _ := setupRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/api/traces/deadbeef00000000deadbeef00000000/export", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestExportImportRoundTrip(t *testing.T) {
	handler, store := setupRouter(t)

	const traceID = "cafebabe12345678cafebabe12345678"
	res := `{"service.name":"webapp","region":"us-east-1"}`
	attrs := `{"http.method":"GET","http.status_code":200}`
	now := time.Now().UnixNano()

	insertSpanForExport(t, store, "sess", traceID, "aaaa111122223333", "0000000000000000", "webapp", res, "GET /api", now, now+2_000_000, attrs)
	insertSpanForExport(t, store, "sess", traceID, "bbbb444455556666", "aaaa111122223333", "webapp", res, "db.query",  now+100_000, now+800_000, `{"db.system":"postgres"}`)

	// Export.
	req := httptest.NewRequest(http.MethodGet, "/api/traces/"+traceID+"/export", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("export returned %d: %s", w.Code, w.Body.String())
	}
	exported := w.Body.Bytes()

	// Import the exported JSON into a new session.
	result, err := importer.Import(store, "reimport", importer.FormatOTLP, exported)
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}
	if result.SpanCount != 2 {
		t.Errorf("expected 2 imported spans, got %d", result.SpanCount)
	}

	// Fetch the reimported spans by traceId.
	importedSpans, err := store.GetTrace(traceID)
	if err != nil {
		t.Fatalf("GetTrace after reimport: %v", err)
	}

	// Find the spans that belong to the imported session.
	var reimported []*storage.Span
	for _, s := range importedSpans {
		if s.SessionID == result.Session.ID {
			reimported = append(reimported, s)
		}
	}
	if len(reimported) != 2 {
		t.Fatalf("expected 2 reimported spans in new session, got %d", len(reimported))
	}

	// Compare name and duration for each reimported span.
	type summary struct{ name string; dur int64 }
	original := map[string]summary{
		"aaaa111122223333": {name: "GET /api",  dur: now + 2_000_000 - now},
		"bbbb444455556666": {name: "db.query",  dur: now + 800_000 - (now + 100_000)},
	}
	for _, s := range reimported {
		want, ok := original[s.SpanID]
		if !ok {
			t.Errorf("unexpected span_id %s in reimported set", s.SpanID)
			continue
		}
		if s.Name != want.name {
			t.Errorf("span %s: name = %q, want %q", s.SpanID, s.Name, want.name)
		}
		dur := s.EndNs - s.StartNs
		if dur != want.dur {
			t.Errorf("span %s: duration = %d, want %d", s.SpanID, dur, want.dur)
		}
		// Verify attributes survived the round-trip.
		if s.SpanID == "aaaa111122223333" {
			var a map[string]any
			if err := json.Unmarshal([]byte(s.Attributes), &a); err != nil {
				t.Fatalf("parse attrs: %v", err)
			}
			if a["http.method"] != "GET" {
				t.Errorf("attr http.method = %v, want GET", a["http.method"])
			}
		}
	}
}
