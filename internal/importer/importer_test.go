package importer

import (
	"strings"
	"testing"

	_ "github.com/marcboeker/go-duckdb"
	"github.com/zfogg/spaniel/internal/storage"
)

func openDB(t *testing.T) *storage.DB {
	t.Helper()
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// ── detect ────────────────────────────────────────────────────────────────────

func TestDetectOTLP(t *testing.T) {
	data := []byte(`{"resourceSpans":[]}`)
	if got := detect(data); got != FormatOTLP {
		t.Errorf("expected FormatOTLP, got %q", got)
	}
}

func TestDetectJaeger(t *testing.T) {
	data := []byte(`{"data":[{"spans":[],"processes":{}}]}`)
	if got := detect(data); got != FormatJaeger {
		t.Errorf("expected FormatJaeger, got %q", got)
	}
}

func TestDetectFallbackToOTLP(t *testing.T) {
	data := []byte(`{"unknown":"field"}`)
	if got := detect(data); got != FormatOTLP {
		t.Errorf("expected FormatOTLP fallback, got %q", got)
	}
}

func TestDetectInvalidJSONFallbackToOTLP(t *testing.T) {
	if got := detect([]byte(`not json`)); got != FormatOTLP {
		t.Errorf("expected FormatOTLP for invalid JSON, got %q", got)
	}
}

// ── Import: OTLP ──────────────────────────────────────────────────────────────

const otlpJSON = `{
  "resourceSpans": [{
    "resource": {"attributes": [{"key": "service.name", "value": {"stringValue": "frontend"}}]},
    "scopeSpans": [{"spans": [{
      "traceId": "aabbccddeeff00112233445566778899",
      "spanId":  "0011223344556677",
      "name":    "GET /api/traces",
      "kind":    2,
      "startTimeUnixNano": "1000000000",
      "endTimeUnixNano":   "2000000000",
      "status": {"code": 1}
    }]}]
  }]
}`

func TestImportOTLP(t *testing.T) {
	db := openDB(t)
	result, err := Import(db, "test-otlp", FormatOTLP, []byte(otlpJSON))
	if err != nil {
		t.Fatalf("Import OTLP: %v", err)
	}
	if result.SpanCount != 1 {
		t.Errorf("expected 1 span, got %d", result.SpanCount)
	}
	if result.TraceCount != 1 {
		t.Errorf("expected 1 trace, got %d", result.TraceCount)
	}
	if result.Session == nil {
		t.Fatal("expected non-nil session")
	}
	if result.Session.Label != "test-otlp" {
		t.Errorf("expected label=test-otlp, got %q", result.Session.Label)
	}
}

func TestImportOTLPMultipleSpans(t *testing.T) {
	db := openDB(t)
	payload := `{
	  "resourceSpans": [{
	    "resource": {"attributes": [{"key": "service.name", "value": {"stringValue": "svc"}}]},
	    "scopeSpans": [{"spans": [
	      {"traceId": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "spanId": "1111111111111111", "name": "op-1",
	       "startTimeUnixNano": "1000000", "endTimeUnixNano": "2000000"},
	      {"traceId": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "spanId": "2222222222222222", "name": "op-2",
	       "startTimeUnixNano": "1500000", "endTimeUnixNano": "2500000"},
	      {"traceId": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "spanId": "3333333333333333", "name": "op-3",
	       "startTimeUnixNano": "1000000", "endTimeUnixNano": "2000000"}
	    ]}]
	  }]
	}`
	result, err := Import(db, "multi", FormatOTLP, []byte(payload))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.SpanCount != 3 {
		t.Errorf("expected 3 spans, got %d", result.SpanCount)
	}
	if result.TraceCount != 2 {
		t.Errorf("expected 2 traces, got %d", result.TraceCount)
	}
}

// ── Import: Jaeger ────────────────────────────────────────────────────────────

const jaegerJSON = `{
  "data": [{
    "traceID": "aabbccddeeff0011",
    "spans": [{
      "traceID": "aabbccddeeff0011",
      "spanID":  "0011223344556677",
      "operationName": "HTTP GET",
      "references": [],
      "startTime": 1000000,
      "duration": 500000,
      "tags": [{"key": "http.status_code", "type": "int64", "value": 200}],
      "processID": "p1"
    }],
    "processes": {
      "p1": {"serviceName": "api-gateway", "tags": []}
    }
  }]
}`

func TestImportJaeger(t *testing.T) {
	db := openDB(t)
	result, err := Import(db, "test-jaeger", FormatJaeger, []byte(jaegerJSON))
	if err != nil {
		t.Fatalf("Import Jaeger: %v", err)
	}
	if result.SpanCount != 1 {
		t.Errorf("expected 1 span, got %d", result.SpanCount)
	}
	if result.TraceCount != 1 {
		t.Errorf("expected 1 trace, got %d", result.TraceCount)
	}
}

// ── Import: auto-detect ───────────────────────────────────────────────────────

func TestImportAutoDetectsOTLP(t *testing.T) {
	db := openDB(t)
	result, err := Import(db, "auto-otlp", FormatAuto, []byte(otlpJSON))
	if err != nil {
		t.Fatalf("Import auto OTLP: %v", err)
	}
	if result.SpanCount != 1 {
		t.Errorf("expected 1 span, got %d", result.SpanCount)
	}
}

func TestImportAutoDetectsJaeger(t *testing.T) {
	db := openDB(t)
	result, err := Import(db, "auto-jaeger", FormatAuto, []byte(jaegerJSON))
	if err != nil {
		t.Fatalf("Import auto Jaeger: %v", err)
	}
	if result.SpanCount != 1 {
		t.Errorf("expected 1 span, got %d", result.SpanCount)
	}
}

// ── Import: error paths ───────────────────────────────────────────────────────

func TestImportBadOTLPJSON(t *testing.T) {
	db := openDB(t)
	_, err := Import(db, "bad", FormatOTLP, []byte(`not json`))
	if err == nil {
		t.Error("expected error for invalid OTLP JSON, got nil")
	}
}

func TestImportBadJaegerJSON(t *testing.T) {
	db := openDB(t)
	_, err := Import(db, "bad", FormatJaeger, []byte(`not json`))
	if err == nil {
		t.Error("expected error for invalid Jaeger JSON, got nil")
	}
}

func TestImportUnsupportedFormat(t *testing.T) {
	db := openDB(t)
	_, err := Import(db, "bad", Format("zipkin"), []byte(`{}`))
	if err == nil {
		t.Error("expected error for unsupported format, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported format") {
		t.Errorf("expected 'unsupported format' in error, got %q", err.Error())
	}
}
