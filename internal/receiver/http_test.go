package receiver

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	_ "github.com/marcboeker/go-duckdb"
	"github.com/zfogg/spaniel/internal/forwarder"
	"github.com/zfogg/spaniel/internal/ingestion"
	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/ws"
)

func newTestReceiver(t *testing.T) (*HTTPReceiver, *storage.DB) {
	t.Helper()
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	sess, _ := db.CreateSession("test", false)
	db.SetActiveSession(sess.ID, sess.Label)

	pipeline := ingestion.NewPipeline(db, ws.NewHub())
	return NewHTTPReceiver(pipeline), db
}

func post(handler http.HandlerFunc, contentType string, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()
	handler(w, req)
	return w
}

// ── Traces ────────────────────────────────────────────────────────────────────

const tracesJSON = `{
  "resourceSpans": [{
    "resource": {"attributes": [{"key": "service.name", "value": {"stringValue": "svc"}}]},
    "scopeSpans": [{"spans": [{
      "traceId": "aabbccddeeff00112233445566778899",
      "spanId":  "0011223344556677",
      "name":    "GET /",
      "startTimeUnixNano": "1000000000",
      "endTimeUnixNano":   "2000000000"
    }]}]
  }]
}`

func TestHandleTracesJSON(t *testing.T) {
	rcv, _ := newTestReceiver(t)
	w := post(rcv.HandleTraces, "application/json", []byte(tracesJSON))
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleTracesBadBody(t *testing.T) {
	rcv, _ := newTestReceiver(t)
	w := post(rcv.HandleTraces, "application/json", []byte(`not json`))
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for bad body, got %d", w.Code)
	}
}

// ── Logs ──────────────────────────────────────────────────────────────────────

const logsJSON = `{
  "resourceLogs": [{
    "resource": {"attributes": [{"key": "service.name", "value": {"stringValue": "svc"}}]},
    "scopeLogs": [{"logRecords": [{
      "timeUnixNano": "1000000000",
      "body": {"stringValue": "hello world"},
      "severityNumber": 9
    }]}]
  }]
}`

func TestHandleLogsJSON(t *testing.T) {
	rcv, _ := newTestReceiver(t)
	w := post(rcv.HandleLogs, "application/json", []byte(logsJSON))
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleLogsBadBody(t *testing.T) {
	rcv, _ := newTestReceiver(t)
	w := post(rcv.HandleLogs, "application/json", []byte(`not json`))
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for bad body, got %d", w.Code)
	}
}

// ── Forwarder integration ─────────────────────────────────────────────────────

func TestHandleTracesCallsForwarder(t *testing.T) {
	rcv, _ := newTestReceiver(t)

	forwarded := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwarded <- r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	rcv.SetForwarder(forwarder.New([]string{upstream.URL}, 1.0))

	post(rcv.HandleTraces, "application/json", []byte(tracesJSON))

	select {
	case path := <-forwarded:
		if path != "/v1/traces" {
			t.Errorf("expected /v1/traces, got %q", path)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("forwarder upstream never received request")
	}
}

// ── Logs forwarder ────────────────────────────────────────────────────────────

func TestHandleLogsCallsForwarder(t *testing.T) {
	rcv, _ := newTestReceiver(t)

	forwarded := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwarded <- r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	rcv.SetForwarder(forwarder.New([]string{upstream.URL}, 1.0))

	post(rcv.HandleLogs, "application/json", []byte(logsJSON))

	select {
	case path := <-forwarded:
		if path != "/v1/logs" {
			t.Errorf("expected /v1/logs, got %q", path)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("forwarder upstream never received request for logs")
	}
}

// ── Metrics ───────────────────────────────────────────────────────────────────

const metricsJSON = `{
  "resourceMetrics": [{
    "resource": {"attributes": [{"key": "service.name", "value": {"stringValue": "svc"}}]},
    "scopeMetrics": [{"metrics": [{
      "name": "http.server.duration",
      "gauge": {
        "dataPoints": [{
          "asDouble": 1.5,
          "timeUnixNano": "1000000000"
        }]
      }
    }]}]
  }]
}`

func TestHandleMetricsJSON(t *testing.T) {
	rcv, _ := newTestReceiver(t)
	w := post(rcv.HandleMetrics, "application/json", []byte(metricsJSON))
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleMetricsBadBody(t *testing.T) {
	rcv, _ := newTestReceiver(t)
	w := post(rcv.HandleMetrics, "application/json", []byte(`not json`))
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for bad metrics body, got %d", w.Code)
	}
}

// ── Concurrent ingest ─────────────────────────────────────────────────────────

func TestConcurrentTraceIngest(t *testing.T) {
	rcv, _ := newTestReceiver(t)

	const n = 10
	results := make(chan int, n)

	for i := 0; i < n; i++ {
		go func() {
			w := post(rcv.HandleTraces, "application/json", []byte(tracesJSON))
			results <- w.Code
		}()
	}

	for i := 0; i < n; i++ {
		code := <-results
		if code != http.StatusOK {
			t.Errorf("goroutine %d: expected 200, got %d", i, code)
		}
	}
}
