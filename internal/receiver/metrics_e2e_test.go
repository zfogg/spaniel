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
	// Expect 3 entries: counter / gauge / histogram (all under "api").
	if len(catalog) != 3 {
		t.Fatalf("expected 3 catalog entries, got %d: %s", len(catalog), dumpJSON(catalog))
	}
	byName := map[string]*storage.MetricCatalogEntry{}
	for _, c := range catalog {
		byName[c.Name] = c
	}
	if got := byName["http.requests"]; got == nil || got.Type != "counter" || got.SampleCount != 2 {
		t.Errorf("counter wrong: %+v", got)
	}
	if got := byName["pool.in_use"]; got == nil || got.Type != "gauge" || got.SampleCount != 2 {
		t.Errorf("gauge wrong: %+v", got)
	}
	// Histogram → 3 rows (p50/p95/p99) per data point.
	if got := byName["http.dur"]; got == nil || got.Type != "histogram" || got.SampleCount != 3 {
		t.Errorf("histogram wrong: %+v", got)
	}

	// And query the histogram series — every percentile must be present.
	rows, err := db.GetMetricSeries(storage.MetricSeriesFilter{Name: "http.dur", SessionID: sess.ID})
	if err != nil {
		t.Fatalf("GetMetricSeries: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("histogram series len = %d, want 3", len(rows))
	}
	seen := map[string]bool{}
	for _, r := range rows {
		for _, pct := range []string{"p50", "p95", "p99"} {
			if bytes.Contains([]byte(r.Attributes), []byte(`"percentile":"`+pct+`"`)) {
				seen[pct] = true
			}
		}
	}
	if !seen["p50"] || !seen["p95"] || !seen["p99"] {
		t.Errorf("missing percentile rows: %+v", seen)
	}
}

func dumpJSON(v any) string {
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}
