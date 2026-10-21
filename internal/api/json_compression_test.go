package api

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/zfogg/spaniel/internal/storage"
)

func TestChartJSONCompressionPreservesEveryPoint(t *testing.T) {
	handler, db := setupRouter(t)
	for i := range 200 {
		if err := db.AppendMetric(&storage.Metric{Name: "latency", ServiceName: "api", SessionID: "chart", Type: "gauge", TimestampNs: int64(i + 1), Value: float64(i),
			SeriesAttributes: `{"http.route":"/cart"}`, SeriesKey: "cart", ScopeAttributes: `{"version":"stable","nested":{"value":1}}`, Exemplars: `[{"trace_id":"trace","span_id":"span"}]`,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.FlushBatch(); err != nil {
		t.Fatal(err)
	}
	read := func(encoding string) ([]byte, *httptest.ResponseRecorder) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/metrics/series?name=latency&sessionId=chart", nil)
		if encoding != "" {
			req.Header.Set("Accept-Encoding", encoding)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("status %d: %s", w.Code, w.Body.String())
		}
		body := w.Body.Bytes()
		if encoding == "gzip" {
			if w.Header().Get("Content-Encoding") != "gzip" {
				t.Fatal("gzip was not negotiated")
			}
			if !strings.Contains(w.Header().Get("Vary"), "Accept-Encoding") {
				t.Fatal("missing cache negotiation header")
			}
			reader, err := gzip.NewReader(bytes.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			body, err = io.ReadAll(reader)
			if err != nil {
				t.Fatal(err)
			}
			_ = reader.Close()
		} else if w.Header().Get("Content-Encoding") != "" {
			t.Fatal("compressed a client that did not request it")
		}
		return body, w
	}
	plain, _ := read("")
	compressed, w := read("gzip")
	var a, b MetricSeriesResponse
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(plain, &envelope); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(envelope.Data, &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(compressed, &envelope); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(envelope.Data, &b); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) || len(b.Series) != 1 || len(b.Series[0].Points) != 200 || len(b.Points) != 200 {
		t.Fatal("compression changed chart observations")
	}
	if w.Body.Len() >= len(plain)/3 {
		t.Fatalf("ineffective compression: %d of %d bytes", w.Body.Len(), len(plain))
	}
	for i, p := range b.Points {
		if p.Value != float64(i) || len(p.Exemplars) != 1 || p.ScopeAttributes["version"] != "stable" {
			t.Fatalf("point %d lost metadata", i)
		}
	}
}
