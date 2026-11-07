package storage

import (
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDashboardSampleHistoryBoundedAndIsolated(t *testing.T) {
	db := openTestDB(t)
	now := time.Unix(1700000000, 0).Truncate(time.Minute)
	points, err := db.DashboardSampleHistory("a", "panel", "session", 2, now)
	if err != nil || len(points) != 1 || points[0].Value != 2 {
		t.Fatalf("first: %#v %v", points, err)
	}
	points, err = db.DashboardSampleHistory("a", "panel", "session", 99, now.Add(10*time.Second))
	if err != nil || len(points) != 1 || points[0].Value != 2 {
		t.Fatalf("minute deduplication: %#v %v", points, err)
	}
	points, err = db.DashboardSampleHistory("a", "panel", "session", 4, now.Add(time.Minute))
	if err != nil || len(points) != 2 || points[1].Value != 4 {
		t.Fatalf("second point: %#v %v", points, err)
	}
	other, err := db.DashboardSampleHistory("b", "panel", "session", 7, now.Add(time.Minute))
	if err != nil || len(other) != 1 || other[0].Value != 7 {
		t.Fatalf("separate series: %#v %v", other, err)
	}
	db.SetFull(true)
	points, err = db.DashboardSampleHistory("a", "panel", "session", 5, now.Add(2*time.Minute))
	if err != nil || len(points) != 2 {
		t.Fatalf("full store must not record: %#v %v", points, err)
	}
	db.SetFull(false)
	points, err = db.DashboardSampleHistory("a", "panel", "session", 8, now.Add(25*time.Hour))
	if err != nil || len(points) != 1 || points[0].Value != 8 {
		t.Fatalf("expired history: %#v %v", points, err)
	}
}

func TestPressurePrunesDerivedHistoryBeforeTelemetry(t *testing.T) {
	db := openTestDB(t)
	sess, err := db.CreateSession("sample-test", false)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Truncate(time.Minute)
	if _, err := db.DashboardSampleHistory("a", "panel", sess.ID, 1, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DashboardSampleHistory("a", "panel", sess.ID, 2, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := db.InsertSpan(&Span{SessionID: sess.ID, TraceID: "trace", SpanID: "span", StartNs: 1, EndNs: 2, Attributes: "{}", Resource: "{}"}); err != nil {
		t.Fatal(err)
	}
	n, err := db.deleteOldestTelemetryBatch(sess.ID)
	if err != nil || n != 1 {
		t.Fatalf("prune history: %d %v", n, err)
	}
	q := db.query.DashboardPanelSample
	samples, err := q.Find()
	if err != nil || len(samples) != 1 || samples[0].Value != 2 {
		t.Fatalf("latest sample lost: %#v %v", samples, err)
	}
	spans, err := db.query.Span.Count()
	if err != nil || spans != 1 {
		t.Fatalf("source span lost: %d %v", spans, err)
	}
}

func TestDashboardHistoryIsRemovedWithPanelAndSession(t *testing.T) {
	db := openTestDB(t)
	sess, err := db.CreateSession("history-delete", false)
	if err != nil {
		t.Fatal(err)
	}
	dashboard, err := db.CreateDashboardWithPanels("history", "", []*DashboardPanel{{Title: "value", DisplayType: "single_value", QuerySQL: "SELECT 1 AS value"}})
	if err != nil {
		t.Fatal(err)
	}
	panelID := dashboard.Panels[0].ID
	if _, err := db.DashboardSampleHistory("a", panelID, sess.ID, 1, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteDashboardPanel("wrong-dashboard", panelID); err != nil {
		t.Fatal(err)
	}
	q := db.query.DashboardPanelSample
	count, err := q.Count()
	if err != nil || count != 1 {
		t.Fatalf("unrelated dashboard removed history: %d %v", count, err)
	}
	if err := db.DeleteSession(sess.ID); err != nil {
		t.Fatal(err)
	}
	count, err = q.Count()
	if err != nil || count != 0 {
		t.Fatalf("session history remained: %d %v", count, err)
	}
	if _, err := db.DashboardSampleHistory("b", panelID, "other", 1, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteDashboardPanel(dashboard.ID, panelID); err != nil {
		t.Fatal(err)
	}
	count, err = q.Count()
	if err != nil || count != 0 {
		t.Fatalf("panel history remained: %d %v", count, err)
	}
}

func TestSizePruningKeepsNewestSignalExamples(t *testing.T) {
	db := openTestDB(t)
	sess, err := db.CreateSession("signal-floor", false)
	if err != nil {
		t.Fatal(err)
	}
	for i := int64(1); i <= 2; i++ {
		id := fmt.Sprint(i)
		if err := db.InsertSpan(&Span{SessionID: sess.ID, TraceID: id, SpanID: id, StartNs: i, EndNs: i + 1, Attributes: "{}", Resource: "{}"}); err != nil {
			t.Fatal(err)
		}
		if err := db.InsertMetric(&Metric{SessionID: sess.ID, Name: "m", Type: "gauge", TimestampNs: i, ServiceName: "svc", Attributes: "{}", Resource: "{}"}); err != nil {
			t.Fatal(err)
		}
	}
	// The newest log belongs to the oldest trace; its trace must survive too.
	if err := db.InsertLog(&Log{SessionID: sess.ID, TraceID: "1", TimestampNs: 3, Body: "newest", Attributes: "{}"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		n, err := db.deleteOldestTelemetryBatch(sess.ID)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			break
		}
	}
	spans, err := db.query.Span.Count()
	if err != nil || spans < 1 {
		t.Fatalf("spans=%d err=%v", spans, err)
	}
	logs, err := db.query.Log.Count()
	if err != nil || logs < 1 {
		t.Fatalf("logs=%d err=%v", logs, err)
	}
	metrics, err := db.query.Metric.Count()
	if err != nil || metrics < 1 {
		t.Fatalf("metrics=%d err=%v", metrics, err)
	}
}

func TestDashboardHistoryConcurrentSamplingEmitsOneChange(t *testing.T) {
	db := openTestDB(t)
	var changes atomic.Int64
	db.SetChangeNotifier(func(resources []string, reason string) {
		if reason == "DashboardSample" {
			changes.Add(1)
		}
	})
	now := time.Now().Truncate(time.Minute)
	var wg sync.WaitGroup
	errors := make(chan error, 12)
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := db.DashboardSampleHistory("shared", "panel", "session", 1, now)
			errors <- err
		}()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	count, err := db.query.DashboardPanelSample.Count()
	if err != nil || count != 1 || changes.Load() != 1 {
		t.Fatalf("count=%d changes=%d err=%v", count, changes.Load(), err)
	}
}

func TestDashboardHistorySurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.duckdb")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Truncate(time.Minute)
	if _, err := db.DashboardSampleHistory("series", "panel", "session", 42, now); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	points, err := db.DashboardSampleHistory("series", "panel", "session", 99, now)
	if err != nil || len(points) != 1 || points[0].Value != 42 {
		t.Fatalf("reopened history=%#v err=%v", points, err)
	}
}

func TestPressureHistoryFloorIsPerPanelNotPerVariableSeries(t *testing.T) {
	db := openTestDB(t)
	now := time.Now().Truncate(time.Minute)
	for i, series := range []string{"old-a", "old-b", "current"} {
		if _, err := db.DashboardSampleHistory(series, "panel", "session", float64(i), now.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	n, err := db.deleteOldestTelemetryBatch("session")
	if err != nil || n != 2 {
		t.Fatalf("expected old series to be reclaimed: n=%d err=%v", n, err)
	}
	q := db.query.DashboardPanelSample
	rows, err := q.Find()
	if err != nil || len(rows) != 1 || rows[0].SeriesKey != "current" {
		t.Fatalf("panel current sample: %#v %v", rows, err)
	}
}

func TestAgePruningRemovesExpiredPanelHistoryInActiveSession(t *testing.T) {
	for _, automatic := range []bool{false, true} {
		t.Run(fmt.Sprint(automatic), func(t *testing.T) {
			db := openTestDB(t)
			sess, err := db.CreateSession("active", false)
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now().Truncate(time.Minute)
			if _, err := db.DashboardSampleHistory("series", "panel", sess.ID, 1, now.Add(-2*time.Hour)); err != nil {
				t.Fatal(err)
			}
			if _, err := db.DashboardSampleHistory("series", "panel", sess.ID, 2, now); err != nil {
				t.Fatal(err)
			}
			cfg := RetentionConfig{MaxAge: time.Hour}
			if automatic {
				cfg.MaxDuration = 10 * time.Second
			}
			if _, err := db.Prune(cfg, sess.ID); err != nil {
				t.Fatal(err)
			}
			q := db.query.DashboardPanelSample
			rows, err := q.Find()
			if err != nil || len(rows) != 1 || rows[0].Value != 2 {
				t.Fatalf("history after age pruning: %#v %v", rows, err)
			}
		})
	}
}

func TestBoundedReclamationIncludesPanelHistory(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "history-reclaim.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now().Truncate(time.Minute)
	for i := 0; i < 20; i++ {
		if _, err := db.DashboardSampleHistory("series", "panel", "session", float64(i), now.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	q := db.query.DashboardPanelSample
	if _, err := q.Where(q.BucketNs.Lt(now.Add(15 * time.Minute).UnixNano())).Delete(); err != nil {
		t.Fatal(err)
	}
	progress := reclaimProgress{OneTable: true}
	if err := db.withMaintenance(func(isolated *DB) error { return isolated.rebuildTelemetry(&progress) }); err != nil {
		t.Fatal(err)
	}
	if !progress.Rebuilt || !progress.HadTelemetryDeletes || progress.Visited&retentionReclaimBit("dashboard_panel_samples") == 0 {
		t.Fatalf("history was not reclaimed: %+v", progress)
	}
	rows, err := q.Find()
	if err != nil || len(rows) != 5 {
		t.Fatalf("survivors=%d err=%v", len(rows), err)
	}
}

func TestSizePruningKeepsNewestFindingsAndNotifications(t *testing.T) {
	db := openTestDB(t)
	sess, err := db.CreateSession("finding-floor", false)
	if err != nil {
		t.Fatal(err)
	}
	for i := int64(1); i <= 3; i++ {
		id := fmt.Sprint(i)
		if err := db.InsertSpan(&Span{SessionID: sess.ID, TraceID: id, SpanID: id, StartNs: i, EndNs: i + 1, Attributes: "{}", Resource: "{}"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.query.TraceIssue.Create(&TraceIssue{ID: "issue", SessionID: sess.ID, TraceID: "1", Kind: "slow_db", CreatedAt: 10}); err != nil {
		t.Fatal(err)
	}
	if err := db.query.LintWarning.Create(&LintWarning{SessionID: sess.ID, TraceID: "2", SpanID: "2", RuleID: "rule", CreatedAt: 10}); err != nil {
		t.Fatal(err)
	}
	// Newer orphans must not hide the latest finding with surviving evidence.
	if err := db.query.TraceIssue.Create(&TraceIssue{ID: "orphan", SessionID: sess.ID, TraceID: "gone", Kind: "slow_db", CreatedAt: 20}); err != nil {
		t.Fatal(err)
	}
	if err := db.query.LintWarning.Create(&LintWarning{SessionID: sess.ID, TraceID: "gone", SpanID: "gone", RuleID: "orphan", CreatedAt: 20}); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 2; i++ {
		if err := db.RecordNotification(&NotificationRecord{ID: fmt.Sprint("notice", i), Source: "alert", CreatedAt: int64(i)}); err != nil {
			t.Fatal(err)
		}
		if err := db.RecordAlertEvent(&AlertEvent{ID: fmt.Sprint("event", i), RuleID: "rule", GroupKey: "all", Kind: "firing", State: "firing", CreatedAt: int64(i)}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 20; i++ {
		n, err := db.deleteOldestTelemetryBatch(sess.ID)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			break
		}
	}
	for name, query := range map[string]string{"issue": "SELECT count(*) FROM trace_issues", "lint": "SELECT count(*) FROM lint_warnings", "notification": "SELECT count(*) FROM notification_records", "alert event": "SELECT count(*) FROM alert_events"} {
		var count int
		if err := db.SQL().QueryRow(query).Scan(&count); err != nil || count != 1 {
			t.Fatalf("%s count=%d err=%v", name, count, err)
		}
	}
}
