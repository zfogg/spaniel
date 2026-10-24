package storage

import (
	"testing"
	"time"
)

func TestSignalRetentionPreservesBaselineAndFreshTrace(t *testing.T) {
	db := openTestDB(t)
	old := time.Now().Add(-30 * 24 * time.Hour).UnixNano()
	seedSession(t, db, "signal-old", "old", old, false, 2)
	seedSession(t, db, "signal-base", "base", old, true, 1)
	seedSession(t, db, "signal-fresh", "fresh", time.Now().UnixNano(), false, 1)
	if e := db.PruneSignals(SignalRetention{TracesDays: 7}); e != nil {
		t.Fatal(e)
	}
	for id, want := range map[string]int64{"signal-old": 0, "signal-base": 1, "signal-fresh": 1} {
		var count int64
		if e := db.gorm.Table("spans").Where("session_id = ?", id).Count(&count).Error; e != nil {
			t.Fatal(e)
		}
		if count != want {
			t.Fatalf("%s: %d want %d", id, count, want)
		}
	}
}

func TestSignalRetentionIndependentSignals(t *testing.T) {
	db := openTestDB(t)
	old := time.Now().Add(-30 * 24 * time.Hour).UnixNano()
	seedSession(t, db, "signals", "signals", old, false, 1)
	seedSession(t, db, "baseline-signals", "baseline", old, true, 1)
	for _, table := range []string{"logs", "metrics"} {
		for _, session := range []string{"signals", "baseline-signals"} {
			statement := "INSERT INTO " + table + " (timestamp_ns,session_id) VALUES (?,?)"
			if table == "metrics" {
				statement = "INSERT INTO metrics (timestamp_ns,session_id,name,type) VALUES (?,?,'test.metric','gauge')"
			}
			if e := db.gorm.Exec(statement, old, session).Error; e != nil {
				t.Fatal(e)
			}
		}
	}
	if e := db.PruneSignals(SignalRetention{LogsDays: 7}); e != nil {
		t.Fatal(e)
	}
	for table, want := range map[string]int64{"logs": 0, "metrics": 1, "spans": 1} {
		var n int64
		if e := db.gorm.Table(table).Where("session_id = ?", "signals").Count(&n).Error; e != nil {
			t.Fatal(e)
		}
		if n != want {
			t.Fatalf("%s count %d want %d", table, n, want)
		}
	}
	if e := db.PruneSignals(SignalRetention{MetricsDays: 7}); e != nil {
		t.Fatal(e)
	}
	for _, table := range []string{"logs", "metrics", "spans"} {
		var n int64
		if e := db.gorm.Table(table).Where("session_id = ?", "baseline-signals").Count(&n).Error; e != nil {
			t.Fatal(e)
		}
		if n != 1 {
			t.Fatal("baseline telemetry expired")
		}
	}
}
