package ingestion

import (
	"strings"
	"testing"
	"time"

	"github.com/zfogg/spaniel/internal/storage"
)

func TestCardinalityFindingDeduplicatesAndOmitsLabelValues(t *testing.T) {
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	now := time.Now().UnixNano()
	if _, err := db.ObserveMetricAdmissions("s", "api", "requests", now-int64(2*time.Hour), 2); err != nil {
		t.Fatal(err)
	}
	for _, attrs := range []string{`{"http.route":"secret-a"}`, `{"http.route":"secret-b"}`} {
		if _, err := db.RecordMetricSeries("s", "api", "requests", attrs, 1); err != nil {
			t.Fatal(err)
		}
	}
	p := &Pipeline{store: db}
	p.evaluateMetricCardinalityWithLimit("s", "api", "requests", now, 2)
	p.evaluateMetricCardinalityWithLimit("s", "api", "requests", now+int64(time.Minute), 2)
	items, _, err := db.ListNotifications("issue", 1, 10)
	if err != nil || len(items) != 1 {
		t.Fatalf("notifications = %+v, %v", items, err)
	}
	if !strings.Contains(items[0].Body, "http.route") || strings.Contains(items[0].Body, "secret-") || !strings.Contains(items[0].Link, "metric=requests") {
		t.Fatalf("unsafe or incomplete finding: %+v", items[0])
	}
}

func TestMetricSeriesLimiterReloadsAfterReset(t *testing.T) {
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	p := &Pipeline{store: db, metricSeries: newMetricSeriesLimiter(2)}
	stream, attrs := "s\x00api\x00requests", `{"http.route":"a"}`
	if ok, fresh := p.metricSeries.Admit(stream, attrs); !ok || !fresh {
		t.Fatal("first admission failed")
	}
	if _, err := db.RecordMetricSeries("s", "api", "requests", attrs, 1); err != nil {
		t.Fatal(err)
	}
	if err := db.Reset(); err != nil {
		t.Fatal(err)
	}
	limiter, err := p.currentMetricSeriesLimiter()
	if err != nil {
		t.Fatal(err)
	}
	if ok, fresh := limiter.Admit(stream, attrs); !ok || !fresh {
		t.Fatal("reset left a stale admission in memory")
	}
}
