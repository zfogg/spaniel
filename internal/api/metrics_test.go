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
	if len(resp.Data) != 1 || resp.Data[0].SampleCount != 2 || resp.Data[0].LastTimestampNs != 2 {
		t.Errorf("expected 1 grouped entry with 2 samples, got %+v", resp.Data)
	}
}

func TestGetMetricCardinality_UsesDurableAdmittedSeries(t *testing.T) {
	handler, db := setupRouter(t)
	if _, err := db.RecordMetricSeries("s1", "api", "http.requests", `{"http.route":"/cart"}`, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := db.RecordMetricSeries("s1", "api", "http.requests", `{"http.route":"/checkout"}`, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := db.RecordMetricSeries("other", "api", "http.requests", `{}`, 3); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/metrics/cardinality?sessionId=s1", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	var resp struct {
		Data []MetricCardinalityStream `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Data) != 1 || resp.Data[0].Active != 2 || resp.Data[0].Limit != 2000 {
		t.Fatalf("cardinality = %+v, want one stream with two identities", resp.Data)
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

func TestGetMetricSeries_EmptyWindowRetainsStreamMetadata(t *testing.T) {
	handler, db := setupRouter(t)
	if err := db.InsertMetric(&storage.Metric{
		Name: "memory.usage", Type: "gauge", Unit: "By", Description: "resident memory",
		TimestampNs: 100, Value: 42, Attributes: "{}", ServiceName: "api", SessionID: "s1",
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/metrics/series?name=memory.usage&service=api&sessionId=s1&from=200", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	var resp struct {
		Data MetricSeriesResponse `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Data.Type != "gauge" || resp.Data.Unit != "By" || resp.Data.Description != "resident memory" {
		t.Errorf("metadata = %+v, want stream metadata", resp.Data)
	}
	if len(resp.Data.Points) != 0 {
		t.Errorf("points = %+v, want empty", resp.Data.Points)
	}
}

func TestGetMetricSeries_SumsStaySeparatedByCompleteIndexedDimensions(t *testing.T) {
	handler, db := setupRouter(t)
	monotonic := true
	for _, row := range []storage.Metric{
		{Name: "signals", Type: "sum", AggregationTemporality: "Cumulative", IsMonotonic: &monotonic, TimestampNs: 100, Value: 10, Attributes: `{"result":"a"}`, SeriesAttributes: `{"result":"a"}`, SeriesKey: "a", ServiceName: "worker", SessionID: "s1"},
		{Name: "signals", Type: "sum", AggregationTemporality: "Cumulative", IsMonotonic: &monotonic, TimestampNs: 100, Value: 20, Attributes: `{"result":"b"}`, SeriesAttributes: `{"result":"b"}`, SeriesKey: "b", ServiceName: "worker", SessionID: "s1"},
		{Name: "signals", Type: "sum", AggregationTemporality: "Cumulative", IsMonotonic: &monotonic, TimestampNs: 200, Value: 13, Attributes: `{"result":"a"}`, SeriesAttributes: `{"result":"a"}`, SeriesKey: "a", ServiceName: "worker", SessionID: "s1"},
		{Name: "signals", Type: "sum", AggregationTemporality: "Cumulative", IsMonotonic: &monotonic, TimestampNs: 200, Value: 2, Attributes: `{"result":"b"}`, SeriesAttributes: `{"result":"b"}`, SeriesKey: "b", ServiceName: "worker", SessionID: "s1"},
	} {
		if err := db.InsertMetric(&row); err != nil {
			t.Fatal(err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/metrics/series?name=signals&service=worker&sessionId=s1&operation=delta", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	var resp struct {
		Data MetricSeriesResponse `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Data.Series) != 2 || len(resp.Data.Points) != 0 {
		t.Fatalf("series=%d points=%d, want two separate series and no merged points", len(resp.Data.Series), len(resp.Data.Points))
	}
	if got := resp.Data.Dimensions["result"]; len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("result dimensions = %#v", got)
	}
	if resp.Data.Aggregation != "per_complete_attribute_set" {
		t.Errorf("aggregation = %q", resp.Data.Aggregation)
	}
}

func TestGetMetricSeries_FiltersCompleteAttributeSetsOnServer(t *testing.T) {
	handler, db := setupRouter(t)
	for _, row := range []storage.Metric{
		{Name: "requests", Type: "sum", TimestampNs: 100, Value: 3, Attributes: `{"result":"ok","http.route":"/cart"}`, SeriesAttributes: `{"result":"ok","http.route":"/cart"}`, SeriesKey: "ok-cart", ServiceName: "api", SessionID: "s1"},
		{Name: "requests", Type: "sum", TimestampNs: 100, Value: 1, Attributes: `{"result":"error","http.route":"/cart"}`, SeriesAttributes: `{"result":"error","http.route":"/cart"}`, SeriesKey: "error-cart", ServiceName: "api", SessionID: "s1"},
	} {
		if err := db.InsertMetric(&row); err != nil {
			t.Fatal(err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/metrics/series?name=requests&service=api&sessionId=s1&attributes[result]=ok", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	var resp struct {
		Data MetricSeriesResponse `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Data.Series) != 1 || resp.Data.Series[0].Key != "ok-cart" {
		t.Fatalf("series = %+v, want only the complete ok-cart identity", resp.Data.Series)
	}
	if got := resp.Data.Dimensions["result"]; len(got) != 2 || got[0] != "error" || got[1] != "ok" {
		t.Fatalf("dimension menu = %v, want all stream values", got)
	}

	legacyReq := httptest.NewRequest(http.MethodGet, "/api/metrics/series?name=requests&service=api&sessionId=s1&attr.result=ok", nil)
	legacyW := httptest.NewRecorder()
	handler.ServeHTTP(legacyW, legacyReq)
	if legacyW.Code != http.StatusOK {
		t.Fatalf("legacy status: want 200, got %d (%s)", legacyW.Code, legacyW.Body.String())
	}
	var legacyResp struct {
		Data MetricSeriesResponse `json:"data"`
	}
	if err := json.Unmarshal(legacyW.Body.Bytes(), &legacyResp); err != nil {
		t.Fatal(err)
	}
	if len(legacyResp.Data.Series) != 2 {
		t.Fatalf("legacy attr filter matched %d series, want no filtering", len(legacyResp.Data.Series))
	}
}

func TestGetMetricSeries_HistogramReturnsRawBuckets(t *testing.T) {
	handler, db := setupRouter(t)
	count, sum := uint64(10), 120.0
	_ = db.InsertMetric(&storage.Metric{Name: "http.dur", Type: "histogram", Unit: "ms", TimestampNs: 100, HistogramCount: &count, HistogramSum: &sum, ExplicitBounds: `[10,20,50]`, BucketCounts: `[1,3,4,2]`, Attributes: `{}`, SeriesAttributes: `{}`, SeriesKey: "one", ServiceName: "api", SessionID: "s1"})

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
	if len(resp.Data.Points) != 1 || len(resp.Data.Points[0].Buckets) != 4 || resp.Data.Points[0].Sum == nil || *resp.Data.Points[0].Sum != 120 {
		t.Errorf("raw histogram = %+v", resp.Data.Points)
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
		Attributes:  `{}`,
		Exemplars:   "",
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
	insertRoot("trace-a", "GET /cart", 1, 1100, 1200)
	insertRoot("trace-b", "POST /checkout", 1, 1800, 1900)
	insertRoot("trace-c", "GET /promo", 2, 2500, 2600) // error
	insertRoot("trace-out", "outside", 1, 9000, 9100)  // out of window

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
	var resp struct {
		Data MetricSeriesResponse `json:"data"`
	}
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

func TestDeriveMetricSeries_HistogramAverageUsesSumAndCount(t *testing.T) {
	countTwo, countFive := uint64(2), uint64(5)
	sumOne, sumTwo := 120.0, 195.0
	points := []MetricSeriesPoint{
		{Value: 0, Count: &countTwo, Sum: &sumOne},
		{Value: 0, Count: &countFive, Sum: &sumTwo},
	}
	deriveMetricSeries(points, "histogram", "Cumulative", "avg")
	if points[0].Value != 60 || points[1].Value != 25 {
		t.Fatalf("histogram average = [%v, %v], want [60, 25]", points[0].Value, points[1].Value)
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
