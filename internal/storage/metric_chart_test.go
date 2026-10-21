package storage

import (
	"reflect"
	"strings"
	"testing"
)

func TestMetricChartProjectionPreservesHistoryAndRawInspection(t *testing.T) {
	db := openTestDB(t)
	for i := range 5 {
		if err := db.AppendMetric(&Metric{Name: "duration", ServiceName: "api", SessionID: "session", TimestampNs: int64(i),
			Type: "histogram", Attributes: `{"raw":"` + strings.Repeat("detail", 1000) + `"}`, Resource: `{"host.name":"private-host"}`,
			SeriesAttributes: `{"http.route":"/cart"}`, SeriesKey: "cart", ScopeAttributes: `{"nested":{"value":1}}`,
			ExplicitBounds: "[1,10]", BucketCounts: "[1,2,0]", Exemplars: `[{"trace_id":"trace","span_id":"span"}]`,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.FlushBatch(); err != nil {
		t.Fatal(err)
	}
	for _, limit := range []int{0, 2} {
		f := MetricSeriesFilter{Name: "duration", Service: "api", SessionID: "session", FromNs: 1, ToNs: 3, Limit: limit}
		raw, err := db.GetMetricSeries(f)
		if err != nil {
			t.Fatal(err)
		}
		chart, err := db.GetMetricChartSeries(f)
		if err != nil {
			t.Fatal(err)
		}
		want := 3
		if limit > 0 {
			want = limit
		}
		if len(raw) != want || len(chart) != want {
			t.Fatalf("point counts raw=%d chart=%d want=%d", len(raw), len(chart), want)
		}
		for i, row := range raw {
			if row.Attributes == "" || row.Resource == "" {
				t.Fatal("raw inspection lost stored metadata")
			}
			expected := *row
			expected.Attributes, expected.Resource = "", ""
			if !reflect.DeepEqual(&expected, chart[i]) {
				t.Fatalf("chart point changed observations: raw=%+v chart=%+v", expected, chart[i])
			}
		}
	}
}
