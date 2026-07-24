package storage

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// seedSession inserts a session with a specific created_at (ns) and N spans.
func seedSession(t *testing.T, d *DB, id, label string, createdNs int64, isBaseline bool, spanCount int) {
	t.Helper()
	if err := d.gorm.Exec(
		`INSERT INTO sessions (id, label, created_at, is_baseline, span_count, services) VALUES (?, ?, ?, ?, ?, '[]')`,
		id, label, createdNs, isBaseline, spanCount,
	).Error; err != nil {
		t.Fatalf("seed session %s: %v", id, err)
	}
	for i := range spanCount {
		if err := d.InsertSpan(&Span{
			TraceID:     fmt.Sprintf("%s-t%d", id, i),
			SpanID:      fmt.Sprintf("%s-s%d", id, i),
			ServiceName: "svc",
			Name:        "op",
			StartNs:     createdNs,
			EndNs:       createdNs + 1000,
			Attributes:  "{}",
			Resource:    "{}",
			SessionID:   id,
			ReceivedAt:  createdNs,
		}); err != nil {
			t.Fatalf("insert span: %v", err)
		}
	}
}

func TestPrune_DeleteByAge_KeepsActiveAndBaseline(t *testing.T) {
	d := openTestDB(t)
	now := time.Now().UnixNano()
	oldNs := time.Now().Add(-30 * 24 * time.Hour).UnixNano()

	seedSession(t, d, "old", "old", oldNs, false, 2)
	seedSession(t, d, "old-baseline", "old-baseline", oldNs, true, 2)
	seedSession(t, d, "old-active", "old-active", oldNs, false, 2)
	seedSession(t, d, "fresh", "fresh", now, false, 2)

	res, err := d.Prune(RetentionConfig{MaxAge: 7 * 24 * time.Hour}, "old-active")
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if res.DeletedByAge != 1 {
		t.Errorf("expected 1 deletion by age (only 'old'), got %d", res.DeletedByAge)
	}

	for id, wantPresent := range map[string]bool{
		"old":          false,
		"old-baseline": true,
		"old-active":   true,
		"fresh":        true,
	} {
		s, _ := d.GetSession(id)
		got := s != nil
		if got != wantPresent {
			t.Errorf("session %s: present=%v, want %v", id, got, wantPresent)
		}
	}
}

func TestPrune_DeleteByCount_OldestFirstPreserveBaselineAndActive(t *testing.T) {
	d := openTestDB(t)
	base := time.Now().UnixNano()

	// 5 sessions, oldest-to-newest: s0..s4. Mark s2 baseline, s4 active.
	seedSession(t, d, "s0", "s0", base-int64(5*time.Hour), false, 1)
	seedSession(t, d, "s1", "s1", base-int64(4*time.Hour), false, 1)
	seedSession(t, d, "s2", "s2", base-int64(3*time.Hour), true, 1) // baseline
	seedSession(t, d, "s3", "s3", base-int64(2*time.Hour), false, 1)
	seedSession(t, d, "s4", "s4", base-int64(1*time.Hour), false, 1) // active

	res, err := d.Prune(RetentionConfig{MaxSessions: 3}, "s4")
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if res.DeletedByCount != 2 {
		t.Errorf("expected 2 deletions by count, got %d", res.DeletedByCount)
	}
	for id, wantPresent := range map[string]bool{
		"s0": false, // oldest deletable
		"s1": false, // next oldest deletable
		"s2": true,  // baseline preserved
		"s3": true,
		"s4": true,
	} {
		s, _ := d.GetSession(id)
		got := s != nil
		if got != wantPresent {
			t.Errorf("session %s: present=%v want=%v", id, got, wantPresent)
		}
	}
}

func TestPrune_DeleteBySize_ShrinksToCap(t *testing.T) {
	// File-backed DB so file-size measurement works.
	tmp := filepath.Join(t.TempDir(), "spaniel.duckdb")
	d, err := Open(tmp)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })

	base := time.Now().UnixNano()
	// 5 sessions of 200 spans each — meaningful weight on disk.
	for i := range 5 {
		seedSession(t, d, fmt.Sprintf("s%d", i), "x", base-int64((5-i)*int(time.Hour)), false, 200)
	}
	// Flush so the file actually reflects the writes.
	d.checkpoint()

	before := d.FileSize()
	if before == 0 {
		t.Fatalf("file size unexpectedly 0")
	}
	// Cap to half of current — forces deletion of some oldest sessions.
	res, err := d.Prune(RetentionConfig{MaxDBSizeBytes: before / 2}, "s4")
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if res.DeletedBySize == 0 {
		t.Errorf("expected at least one deletion by size, got 0 (before=%d after=%d)", before, d.FileSize())
	}
	if s, _ := d.GetSession("s4"); s == nil {
		t.Errorf("active session was deleted — must always be preserved")
	}
}

func TestPrune_NoLimits_NoOp(t *testing.T) {
	d := openTestDB(t)
	seedSession(t, d, "s1", "s1", time.Now().UnixNano(), false, 1)

	res, err := d.Prune(RetentionConfig{}, "s1")
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if res.DeletedByAge+res.DeletedByCount+res.DeletedBySize != 0 {
		t.Errorf("expected no deletions with zero config, got %+v", res)
	}
	if res.FinalSessions != 1 {
		t.Errorf("expected 1 session remaining, got %d", res.FinalSessions)
	}
}

func TestReset_WipesAllTables(t *testing.T) {
	d := openTestDB(t)
	seedSession(t, d, "s1", "s1", time.Now().UnixNano(), false, 3)
	d.SetActiveSession("s1", "s1")
	_ = d.InsertLog(&Log{TraceID: "t", SpanID: "s", Body: "hi", Attributes: "{}", ServiceName: "svc", SessionID: "s1", ReceivedAt: 1})
	_ = d.InsertLintWarning(&LintWarning{SpanID: "x", TraceID: "t", SessionID: "s1", RuleID: "r", Message: "m", Severity: "warning", CreatedAt: 1})

	if err := d.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}

	sessions, _ := d.ListSessions()
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions after Reset, got %d", len(sessions))
	}
	stats, _ := d.GetStats("")
	if stats.SpanCount != 0 || stats.LogCount != 0 {
		t.Errorf("expected 0 spans/logs after Reset, got spans=%d logs=%d", stats.SpanCount, stats.LogCount)
	}
	warns, _ := d.ListLintWarnings("")
	if len(warns) != 0 {
		t.Errorf("expected 0 lint warnings after Reset, got %d", len(warns))
	}
	if d.ActiveSessionID() != "" {
		t.Errorf("expected active session cleared, got %q", d.ActiveSessionID())
	}
}

func TestGetStats_IncludesSessionCountAndOldest(t *testing.T) {
	d := openTestDB(t)
	first := time.Now().Add(-2 * time.Hour).UnixNano()
	second := time.Now().UnixNano()
	seedSession(t, d, "old", "old", first, false, 1)
	seedSession(t, d, "new", "new", second, false, 1)

	stats, err := d.GetStats("")
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if stats.SessionCount != 2 {
		t.Errorf("SessionCount = %d, want 2", stats.SessionCount)
	}
	if stats.OldestSessionAt != first {
		t.Errorf("OldestSessionAt = %d, want %d", stats.OldestSessionAt, first)
	}
}
