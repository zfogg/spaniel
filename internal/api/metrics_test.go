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

func TestGetMetricSeries_ExemplarsReturned(t *testing.T) {
	handler, db := setupRouter(t)
	// Insert a counter with exemplars linking to a trace
	err := db.InsertMetric(&storage.Metric{
		Name: "http.requests", Type: "counter", Unit: "req",
		TimestampNs: 1000, Value: 42,
		Attributes:  `{}`,
		Exemplars:   `[{"trace_id":"abc123def456789","span_id":"span0123"}]`,
		ServiceName: "api", SessionID: "s1",
	})
	if err != nil {
		t.Fatalf("insert metric: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/metrics/series?name=http.requests&service=api&sessionId=s1", nil)
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
	if len(resp.Data.Points) != 1 {
		t.Fatalf("expected 1 point, got %d", len(resp.Data.Points))
	}
	point := resp.Data.Points[0]
	if len(point.Exemplars) != 1 {
		t.Errorf("expected 1 exemplar, got %d", len(point.Exemplars))
	}
	if point.Exemplars[0].TraceID != "abc123def456789" || point.Exemplars[0].SpanID != "span0123" {
		t.Errorf("exemplar mismatch: got=%+v", point.Exemplars[0])
	}
}

func TestGetMetricSeries_NoExemplars(t *testing.T) {
	handler, db := setupRouter(t)
	// Insert a counter without exemplars
	_ = db.InsertMetric(&storage.Metric{
		Name: "http.requests", Type: "counter", Unit: "req",
		TimestampNs: 1000, Value: 42,
		Attributes: `{}`,
		Exemplars:  "",
		ServiceName: "api", SessionID: "s1",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/metrics/series?name=http.requests&service=api&sessionId=s1", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	var resp struct {
		Data MetricSeriesResponse `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Data.Points) == 0 {
		t.Fatalf("expected points, got 0")
	}
	point := resp.Data.Points[0]
	if len(point.Exemplars) > 0 {
		t.Errorf("expected no exemplars, got %d", len(point.Exemplars))
	}
}

func TestGetMetricSeries_WithTracesOverlay(t *testing.T) {
	handler, db := setupRouter(t)
	// One metric data point at t=2000 anchors the window.
	_ = db.InsertMetric(&storage.Metric{
		Name: "http.req", Type: "counter", Unit: "req",
		TimestampNs: 2000, Value: 5, Attributes: "{}",
		ServiceName: "api", SessionID: "s1",
	})
	// Three root spans inside [1000, 3000]; one outside (should NOT come back).
	insertRoot := func(traceID, op string, statusCode int, startNs, endNs int64) {
		_ = db.InsertSpan(&storage.Span{
			TraceID: traceID, SpanID: "root-" + traceID, ParentSpanID: "",
			ServiceName: "api", Name: op, StatusCode: statusCode,
			StartNs: startNs, EndNs: endNs,
			Attributes: "{}", Resource: "{}", SessionID: "s1", SessionLabel: "s1",
		})
	}
	insertRoot("trace-a", "GET /cart",     1, 1100, 1200)
	insertRoot("trace-b", "POST /checkout", 1, 1800, 1900)
	insertRoot("trace-c", "GET /promo",    2, 2500, 2600) // error
	insertRoot("trace-out", "outside",     1, 9000, 9100) // out of window

	req := httptest.NewRequest(http.MethodGet,
		"/api/metrics/series?name=http.req&service=api&sessionId=s1&with_traces=1&from=1000&to=3000", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	var resp struct {
		Data MetricSeriesResponse `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Data.Traces) != 3 {
		t.Fatalf("expected 3 in-window traces, got %d (%+v)", len(resp.Data.Traces), resp.Data.Traces)
	}
	// Ascending by start_ns + op resolved from root span name.
	wantOps := []string{"GET /cart", "POST /checkout", "GET /promo"}
	for i, want := range wantOps {
		if resp.Data.Traces[i].Op != want {
			t.Errorf("traces[%d].op: want %q, got %q", i, want, resp.Data.Traces[i].Op)
		}
	}
	if resp.Data.Traces[2].StatusCode != 2 {
		t.Errorf("expected status_code=2 carried for the error trace, got %d", resp.Data.Traces[2].StatusCode)
	}
}

func TestGetMetricSeries_WithTracesOverlay_DefaultsToPointsWindow(t *testing.T) {
	// No explicit from/to → server falls back to min/max of returned points.
	handler, db := setupRouter(t)
	for _, ts := range []int64{1500, 2500} {
		_ = db.InsertMetric(&storage.Metric{
			Name: "g", Type: "gauge", TimestampNs: ts, Value: 1,
			Attributes: "{}", ServiceName: "api", SessionID: "s1",
		})
	}
	_ = db.InsertSpan(&storage.Span{
		TraceID: "in", SpanID: "root-in", ServiceName: "api", Name: "in-window",
		StartNs: 2000, EndNs: 2100, Attributes: "{}", Resource: "{}",
		SessionID: "s1", SessionLabel: "s1",
	})
	_ = db.InsertSpan(&storage.Span{
		TraceID: "out", SpanID: "root-out", ServiceName: "api", Name: "out-window",
		StartNs: 9000, EndNs: 9100, Attributes: "{}", Resource: "{}",
		SessionID: "s1", SessionLabel: "s1",
	})

	req := httptest.NewRequest(http.MethodGet,
		"/api/metrics/series?name=g&service=api&sessionId=s1&with_traces=1", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: %d (%s)", w.Code, w.Body.String())
	}
	var resp struct{ Data MetricSeriesResponse `json:"data"` }
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Data.Traces) != 1 || resp.Data.Traces[0].Op != "in-window" {
		t.Errorf("expected single in-window trace, got %+v", resp.Data.Traces)
	}
}

func TestGetMetricSeries_TracesEmptyArrayWhenFlagAbsent(t *testing.T) {
	// Without ?with_traces=1, the field must serialize as [] (not null) so
	// the frontend type stays non-optional.
	handler, db := setupRouter(t)
	_ = db.InsertMetric(&storage.Metric{
		Name: "g", Type: "gauge", TimestampNs: 100, Value: 1,
		Attributes: "{}", ServiceName: "api", SessionID: "s1",
	})

	req := httptest.NewRequest(http.MethodGet,
		"/api/metrics/series?name=g&service=api&sessionId=s1", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: %d", w.Code)
	}
	if !contains(w.Body.String(), `"traces":[]`) {
		t.Errorf("expected `\"traces\":[]` in response, got: %s", w.Body.String())
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
