package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zfogg/spaniel/internal/storage"
)

func TestListMetrics_Empty(t *testing.T) {
	handler, _ := setupRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/api/metrics", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		Data []storage.MetricCatalogEntry `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Data) != 0 {
		t.Errorf("expected empty catalog, got %+v", resp.Data)
	}
}

func TestListMetrics_GroupsByServiceAndName(t *testing.T) {
	handler, db := setupRouter(t)
	_ = db.InsertMetric(&storage.Metric{
		Name: "http.req", Type: "counter", Unit: "req",
		TimestampNs: 1, Value: 1, Attributes: "{}",
		ServiceName: "api", SessionID: "s1",
	})
	_ = db.InsertMetric(&storage.Metric{
		Name: "http.req", Type: "counter", Unit: "req",
		TimestampNs: 2, Value: 2, Attributes: "{}",
		ServiceName: "api", SessionID: "s1",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/metrics?sessionId=s1", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	var resp struct {
		Data []storage.MetricCatalogEntry `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Data) != 1 || resp.Data[0].SampleCount != 2 {
		t.Errorf("expected 1 grouped entry with 2 samples, got %+v", resp.Data)
	}
}

func TestGetMetricSeries_MissingName(t *testing.T) {
	handler, _ := setupRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/api/metrics/series", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 without name, got %d", w.Code)
	}
}

func TestGetMetricSeries_HistogramSplitsByPercentile(t *testing.T) {
	handler, db := setupRouter(t)
	// Insert one histogram data point as three rows (p50/p95/p99).
	for _, pct := range []struct {
		name string
		v    float64
	}{{"p50", 10}, {"p95", 30}, {"p99", 60}} {
		_ = db.InsertMetric(&storage.Metric{
			Name: "http.dur", Type: "histogram", Unit: "ms",
			TimestampNs: 100, Value: pct.v,
			Attributes:  `{"percentile":"` + pct.name + `"}`,
			ServiceName: "api", SessionID: "s1",
		})
	}

	req := httptest.NewRequest(http.MethodGet, "/api/metrics/series?name=http.dur&service=api&sessionId=s1", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	var resp struct {
		Data MetricSeriesResponse `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Data.Type != "histogram" || resp.Data.Unit != "ms" {
		t.Errorf("expected histogram/ms, got type=%s unit=%s", resp.Data.Type, resp.Data.Unit)
	}
	if len(resp.Data.Points) != 3 {
		t.Fatalf("expected 3 points, got %d", len(resp.Data.Points))
	}
	pcts := map[string]float64{}
	for _, p := range resp.Data.Points {
		pcts[p.Percentile] = p.Value
	}
	if pcts["p50"] != 10 || pcts["p95"] != 30 || pcts["p99"] != 60 {
		t.Errorf("percentile values wrong: %+v", pcts)
	}
}
