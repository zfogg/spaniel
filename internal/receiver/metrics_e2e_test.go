package receiver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zfogg/spaniel/internal/ingestion"
	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/ws"
)

// TestOTLPMetricsEndToEnd drives the full receive → pipeline → storage path
// through a real HTTPReceiver mounted on httptest. Uses a free OS-chosen port,
// so it's immune to whatever else happens to be bound on 4318 locally.
func TestOTLPMetricsEndToEnd(t *testing.T) {
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	sess, _ := db.CreateSession("e2e-metrics", false)
	db.SetActiveSession(sess.ID, sess.Label)

	pipeline := ingestion.NewPipeline(db, ws.NewHub())
	rcv := NewHTTPReceiver(pipeline)

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/metrics", rcv.HandleMetrics)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	payload := []byte(`{
		"resourceMetrics":[{
			"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"api"}}]},
			"scopeMetrics":[{"metrics":[
				{"name":"http.requests","description":"req count","unit":"req",
				 "sum":{"aggregationTemporality":1,"isMonotonic":true,"dataPoints":[
					{"timeUnixNano":"1000000000","asInt":"5"},
					{"timeUnixNano":"2000000000","asInt":"8"}
				 ]}},
				{"name":"pool.in_use","description":"connections","unit":"conn",
				 "gauge":{"dataPoints":[
					{"timeUnixNano":"1000000000","asDouble":4.0},
					{"timeUnixNano":"2000000000","asDouble":7.5}
				 ]}},
				{"name":"http.dur","description":"latency","unit":"ms",
				 "histogram":{"aggregationTemporality":1,"dataPoints":[
					{"timeUnixNano":"1000000000","count":"10","sum":120,
					 "bucketCounts":["1","3","4","2"],"explicitBounds":[10,20,50]}
				 ]}}
			]}]
		}]
	}`)

	resp, err := http.Post(srv.URL+"/v1/metrics", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("POST /v1/metrics: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	catalog, err := db.ListMetricCatalog(sess.ID)
	if err != nil {
		t.Fatalf("ListMetricCatalog: %v", err)
	}
	// Expect the original three OTLP streams, all under "api".
	if len(catalog) != 3 {
		t.Fatalf("expected 3 catalog entries, got %d: %s", len(catalog), dumpJSON(catalog))
	}
	byName := map[string]*storage.MetricCatalogEntry{}
	for _, c := range catalog {
		byName[c.Name] = c
	}
	if got := byName["http.requests"]; got == nil || got.Type != "sum" || got.AggregationTemporality != "Delta" || got.IsMonotonic == nil || !*got.IsMonotonic || got.SampleCount != 2 {
		t.Errorf("sum metadata wrong: %+v", got)
	}
	if got := byName["pool.in_use"]; got == nil || got.Type != "gauge" || got.SampleCount != 2 {
		t.Errorf("gauge wrong: %+v", got)
	}
	if got := byName["http.dur"]; got == nil || got.Type != "histogram" || got.AggregationTemporality != "Delta" || got.SampleCount != 1 {
		t.Errorf("histogram wrong: %+v", got)
	}

	// The histogram is one lossless point with its original buckets, not three
	// precomputed percentile approximations.
	rows, err := db.GetMetricSeries(storage.MetricSeriesFilter{Name: "http.dur", SessionID: sess.ID})
	if err != nil {
		t.Fatalf("GetMetricSeries: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("histogram series len = %d, want 1", len(rows))
	}
	if rows[0].HistogramCount == nil || *rows[0].HistogramCount != 10 || rows[0].HistogramSum == nil || *rows[0].HistogramSum != 120 || rows[0].ExplicitBounds != `[10,20,50]` || rows[0].BucketCounts != `[1,3,4,2]` {
		t.Errorf("histogram point not preserved: %+v", rows[0])
	}
}

func dumpJSON(v any) string {
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}
