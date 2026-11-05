package ingestion

import (
	"context"
	"testing"
	"time"

	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/ws"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
)

func TestPoolMetricIngestionAfterCommit(t *testing.T) {
	db := openTestDB(t)
	session, err := db.CreateSession("pool", false)
	if err != nil {
		t.Fatal(err)
	}
	db.SetActiveSession(session.ID, session.Label)
	p := NewPipeline(db, ws.NewHub())
	now := time.Now().UnixNano()
	a, b := now-int64(30*time.Second), now-int64(10*time.Second)
	if a/poolMetricWindow != b/poolMetricWindow {
		b = now/poolMetricWindow*poolMetricWindow - int64(time.Second)
		a = b - int64(20*time.Second)
	}
	md := pmetric.NewMetrics()
	rm := md.ResourceMetrics().AppendEmpty()
	rm.Resource().Attributes().PutStr("service.name", "api")
	m := rm.ScopeMetrics().AppendEmpty().Metrics().AppendEmpty()
	m.SetName("db.client.connection.pending_requests")
	points := m.SetEmptyGauge().DataPoints()
	for _, timestamp := range []int64{a, b} {
		dp := points.AppendEmpty()
		dp.SetTimestamp(pcommon.Timestamp(timestamp))
		dp.SetIntValue(1)
		dp.Attributes().PutStr("db.client.connection.pool.name", "orders")
	}
	if err := p.IngestMetrics(context.Background(), md); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		p.poolMetrics.mu.Lock()
		running := p.poolMetrics.running
		p.poolMetrics.mu.Unlock()
		if !running {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	p.poolMetrics.mu.Lock()
	running := p.poolMetrics.running
	p.poolMetrics.mu.Unlock()
	if running {
		t.Fatal("metric evaluation did not finish")
	}
	findings, err := db.ListLintWarnings(db.ActiveSessionID())
	if err != nil || len(findings) != 1 || findings[0].RuleID != "pool_saturation_metric" {
		points, queryErr := db.GetMetricSeries(storage.MetricSeriesFilter{Name: "db.client.connection.pending_requests", Limit: 10})
		t.Fatalf("post-commit finding: %v %v; session=%q points=%+v query=%v", findings, err, db.ActiveSessionID(), points, queryErr)
	}
}

func TestPoolMetricPersistenceAndLateData(t *testing.T) {
	db := openTestDB(t)
	p := NewPipeline(db, ws.NewHub())
	now := int64(32 * time.Minute)
	window := now / poolMetricWindow * poolMetricWindow
	key := poolMetricKey{"session", "api", "orders", "db.client.connection.pending_requests", window}
	row := func(ts int64, value float64) *storage.Metric {
		return &storage.Metric{Name: key.name, Type: "gauge", TimestampNs: ts, Value: value, Attributes: `{"db.client.connection.pool.name":"orders"}`, Resource: `{}`, ServiceName: key.service, SessionID: key.session}
	}
	if err := db.InsertMetric(row(now-int64(time.Minute), 1)); err != nil {
		t.Fatal(err)
	}
	p.evaluatePoolMetric(key, now)
	findings, err := db.ListLintWarnings(key.session)
	if err != nil || len(findings) != 0 {
		t.Fatalf("single point: %v %v", findings, err)
	}
	// The second point arrives later but has an earlier event timestamp.
	if err := db.InsertMetric(row(now-int64(90*time.Second), 2)); err != nil {
		t.Fatal(err)
	}
	p.evaluatePoolMetric(key, now)
	p.evaluatePoolMetric(key, now)
	findings, err = db.ListLintWarnings(key.session)
	if err != nil || len(findings) != 1 || findings[0].RuleID != "pool_saturation_metric" || findings[0].TraceID != "" {
		t.Fatalf("findings: %v %v", findings, err)
	}
	notices, total, err := db.ListNotifications("issue", 1, 10)
	if err != nil || total != 1 || len(notices) != 1 || notices[0].Link != "/metrics?metric=db.client.connection.pending_requests&service=api&session=session" {
		t.Fatalf("notifications: %v %d %v", notices, total, err)
	}
}
