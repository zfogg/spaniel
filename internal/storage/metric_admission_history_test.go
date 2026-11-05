package storage

import (
	"path/filepath"
	"testing"
	"time"
)

func TestMetricAdmissionHistoryUsesIngestionTimeAndReset(t *testing.T) {
	d := openTestDB(t)
	before := time.Now().UnixNano()
	if _, err := d.RecordMetricSeries("s", "svc", "m", `{"http.route":"secret"}`, 1); err != nil {
		t.Fatal(err)
	}
	after := time.Now().UnixNano()
	h, err := d.ObserveMetricAdmissions("s", "svc", "m", before, 2000)
	if err != nil {
		t.Fatal(err)
	}
	if h.StartedAtNs != before || len(h.Series) != 1 || h.Series[0].AdmittedAtNs == nil || *h.Series[0].AdmittedAtNs < before || *h.Series[0].AdmittedAtNs > after {
		t.Fatalf("bad admission history: %+v", h)
	}
	h, err = d.ObserveMetricAdmissions("s", "svc", "m", after, 2000)
	if err != nil || h.StartedAtNs != before {
		t.Fatalf("observation start changed: %+v, %v", h, err)
	}
	if err := d.Reset(); err != nil {
		t.Fatal(err)
	}
	h, err = d.ObserveMetricAdmissions("s", "svc", "m", after, 2000)
	if err != nil || h.StartedAtNs != after || len(h.Series) != 0 {
		t.Fatalf("reset history: %+v, %v", h, err)
	}
}

func TestMetricAdmissionHistoryUpgradeLeavesPriorAdmissionsUnknown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upgrade.duckdb")
	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{
		`DROP VIEW telemetry_metric_series`,
		`DROP TABLE metric_series_catalog`,
		`DROP TABLE metric_admission_observations`,
		`DELETE FROM migrations WHERE id = '0043_metric_admission_history'`,
	} {
		if err := d.gorm.Exec(sql).Error; err != nil {
			_ = d.Close()
			t.Fatal(err)
		}
	}
	if err := execMigrationFile(d.gorm, "0017_metric_series_catalog.sql"); err != nil {
		_ = d.Close()
		t.Fatal(err)
	}
	for _, sql := range []string{
		`CREATE VIEW telemetry_metric_series AS SELECT * FROM metric_series_catalog`,
		`INSERT INTO metric_series_catalog VALUES ('s', 'svc', 'm', 'legacy', '{}', 1, 1, 1)`,
	} {
		if err := d.gorm.Exec(sql).Error; err != nil {
			_ = d.Close()
			t.Fatal(err)
		}
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	d, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	h, err := d.ObserveMetricAdmissions("s", "svc", "m", 123, 2000)
	if err != nil || len(h.Series) != 1 || h.Series[0].AdmittedAtNs != nil || h.StartedAtNs != 123 {
		t.Fatalf("upgrade history = %+v, %v", h, err)
	}
}

func TestMetricAdmissionHistorySessionIsolation(t *testing.T) {
	d := openTestDB(t)
	for _, id := range []string{"a", "b"} {
		if _, err := d.RecordMetricSeries(id, "svc", "m", `{}`, 1); err != nil {
			t.Fatal(err)
		}
		if _, err := d.ObserveMetricAdmissions(id, "svc", "m", 1, 2000); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.DeleteSession("a"); err != nil {
		t.Fatal(err)
	}
	a, err := d.ObserveMetricAdmissions("a", "svc", "m", 2, 2000)
	if err != nil || len(a.Series) != 0 || a.StartedAtNs != 2 {
		t.Fatalf("deleted session: %+v %v", a, err)
	}
	b, err := d.ObserveMetricAdmissions("b", "svc", "m", 2, 2000)
	if err != nil || len(b.Series) != 1 || b.StartedAtNs != 1 {
		t.Fatalf("other session: %+v %v", b, err)
	}
}

func TestMetricRetentionInvalidatesObservation(t *testing.T) {
	d := openTestDB(t)
	if err := d.InsertMetric(&Metric{Name: "m", ServiceName: "svc", SessionID: "s", TimestampNs: 1, Attributes: "{}"}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.RecordMetricSeries("s", "svc", "m", `{}`, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ObserveMetricAdmissions("s", "svc", "m", 1, 2000); err != nil {
		t.Fatal(err)
	}
	if err := d.PruneSignals(SignalRetention{MetricsDays: 1}); err != nil {
		t.Fatal(err)
	}
	h, err := d.ObserveMetricAdmissions("s", "svc", "m", 2, 2000)
	if err != nil || h.StartedAtNs != 2 || len(h.Series) != 1 {
		t.Fatalf("retained catalog after observation reset: %+v, %v", h, err)
	}
}
