package storage

import (
	"testing"
)

func TestInsertAndListMetricCatalog(t *testing.T) {
	d := openTestDB(t)
	_ = d.InsertMetric(&Metric{
		Name: "http.req.count", Description: "reqs", Unit: "req", Type: "counter",
		TimestampNs: 100, Value: 5, Attributes: "{}",
		ServiceName: "api", SessionID: "s1",
	})
	_ = d.InsertMetric(&Metric{
		Name: "http.req.count", Description: "reqs", Unit: "req", Type: "counter",
		TimestampNs: 200, Value: 6, Attributes: "{}",
		ServiceName: "api", SessionID: "s1",
	})
	_ = d.InsertMetric(&Metric{
		Name: "pg.pool.in_use", Description: "conns", Unit: "conn", Type: "gauge",
		TimestampNs: 100, Value: 8, Attributes: "{}",
		ServiceName: "postgres", SessionID: "s1",
	})

	entries, err := d.ListMetricCatalog("s1")
	if err != nil {
		t.Fatalf("ListMetricCatalog: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 catalog entries, got %d (%+v)", len(entries), entries)
	}
	// Sorted by (service, name): api < postgres.
	if entries[0].ServiceName != "api" || entries[0].SampleCount != 2 {
		t.Errorf("api entry wrong: %+v", entries[0])
	}
	if entries[1].ServiceName != "postgres" || entries[1].Type != "gauge" {
		t.Errorf("postgres entry wrong: %+v", entries[1])
	}
}

func TestGetMetricSeries_Filters(t *testing.T) {
	d := openTestDB(t)
	for i, v := range []float64{1, 2, 3, 4, 5} {
		_ = d.InsertMetric(&Metric{
			Name: "m", Type: "gauge", TimestampNs: int64((i + 1) * 100),
			Value: v, Attributes: "{}", ServiceName: "api", SessionID: "s1",
		})
	}
	// Inserted from another service — must be filtered out.
	_ = d.InsertMetric(&Metric{
		Name: "m", Type: "gauge", TimestampNs: 999, Value: 99,
		Attributes: "{}", ServiceName: "other", SessionID: "s1",
	})

	rows, err := d.GetMetricSeries(MetricSeriesFilter{Name: "m", Service: "api", SessionID: "s1"})
	if err != nil {
		t.Fatalf("GetMetricSeries: %v", err)
	}
	if len(rows) != 5 {
		t.Fatalf("expected 5 rows, got %d", len(rows))
	}
	// Ascending by timestamp.
	for i := 1; i < len(rows); i++ {
		if rows[i].TimestampNs < rows[i-1].TimestampNs {
			t.Errorf("rows not ordered ascending at %d", i)
		}
	}

	// from/to bounds.
	rows, _ = d.GetMetricSeries(MetricSeriesFilter{
		Name: "m", Service: "api", SessionID: "s1",
		FromNs: 200, ToNs: 400,
	})
	if len(rows) != 3 {
		t.Errorf("expected 3 rows in [200,400], got %d", len(rows))
	}
}

func TestReset_ClearsMetrics(t *testing.T) {
	d := openTestDB(t)
	_ = d.InsertMetric(&Metric{
		Name: "m", Type: "gauge", Value: 1, Attributes: "{}",
		ServiceName: "x", SessionID: "s1",
	})
	if err := d.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	rows, _ := d.GetMetricSeries(MetricSeriesFilter{})
	if len(rows) != 0 {
		t.Errorf("expected metrics cleared, got %d rows", len(rows))
	}
}
