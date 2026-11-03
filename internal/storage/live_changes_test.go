package storage

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"
)

func TestDurableResourceNotifications(t *testing.T) {
	db := openTestDB(t)
	type change struct {
		resources []string
		reason    string
	}
	var changes []change
	db.SetChangeNotifier(func(resources []string, reason string) {
		changes = append(changes, change{append([]string{}, resources...), reason})
	})
	scoped := db.WithContext(context.Background())
	dashboard, err := scoped.CreateDashboard("live", "durable definition")
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || !slices.Contains(changes[0].resources, "dashboards") {
		t.Fatal("definition did not notify after commit")
	}
	if loaded, err := db.GetDashboard(dashboard.ID); err != nil || loaded.Name != "live" {
		t.Fatal("notified before definition was readable")
	}
	if err := scoped.DeleteDashboard(dashboard.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetDashboard(dashboard.ID); err == nil {
		t.Fatal("definition deletion not durable")
	}
	if changes[len(changes)-1].reason != "DeleteDashboard" {
		t.Fatal("definition removal notification missing")
	}
	rule := &AlertRule{ID: "live-rule", Name: "live"}
	if err := scoped.CreateAlertRule(rule); err != nil {
		t.Fatal(err)
	}
	before := len(changes)
	if err := scoped.CreateAlertRule(rule); err == nil {
		t.Fatal("duplicate insert should fail")
	}
	if len(changes) != before {
		t.Fatal("failed write emitted a durable change")
	}
	if err := scoped.UpsertAlertInstances([]*AlertInstance{{RuleID: rule.ID, GroupKey: "all", State: "pending"}}); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(changes[len(changes)-1].resources, "alerts") {
		t.Fatal("pending evaluator state not announced")
	}
	warning := &LintWarning{TraceID: "t", SpanID: "s", RuleID: "test-rule", Message: "durable lint", Severity: "warning"}
	if err := scoped.InsertLintWarning(warning); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range changes {
		if c.reason == "lint" && slices.Contains(c.resources, "lint") {
			found = true
		}
	}
	if !found {
		t.Fatal("asynchronous lint notification missing")
	}
}

func TestResetPublishesCommittedDeletionEvenWhenCheckpointFails(t *testing.T) {
	for _, mode := range []struct {
		name  string
		clear func(*DB) error
	}{
		{"telemetry", func(db *DB) error { return db.resetData(false) }},
		{"database", func(db *DB) error { return db.resetData(true) }},
		{"sessions", (*DB).deleteAllSessionData},
	} {
		t.Run(mode.name, func(t *testing.T) {
			for _, failCheckpoint := range []bool{false, true} {
				t.Run(fmt.Sprint(failCheckpoint), func(t *testing.T) {
					db := openTestDB(t)
					seedSession(t, db, "old", "old", time.Now().UnixNano(), false, 1)
					db.SetActiveSession("old", "old")
					observed := false
					db.SetChangeNotifier(func(resources []string, reason string) {
						if reason != "reset" {
							t.Fatalf("unexpected change %s", reason)
						}
						for _, want := range []string{"traces", "logs", "metrics", "lint", "issues", "active-session"} {
							if !slices.Contains(resources, want) {
								t.Errorf("missing reset resource %s", want)
							}
						}
						if db.ActiveSessionID() != "" || db.ActiveSessionLabel() != "" {
							t.Error("notified before shared active scope cleared")
						}
						observed = true
					})
					// Use a context copy created after notifier installation, just as HTTP does.
					scoped := db.WithContext(context.Background())
					checkpointErr := errors.New("checkpoint failed after commit")
					err := scoped.clearWithCheckpoint(mode.clear, func(isolated *DB) error {
						if failCheckpoint {
							return checkpointErr
						}
						return isolated.checkpointWithRetry()
					})
					if failCheckpoint && !errors.Is(err, checkpointErr) || !failCheckpoint && err != nil {
						t.Fatal(err)
					}
					if !observed {
						t.Fatal("durable reset not announced")
					}
					if rows, err := db.GetTrace("old-t0"); err != nil || len(rows) != 0 {
						t.Fatalf("reset was not durable: %v %v", rows, err)
					}
					if scoped.ActiveSessionID() != "" || db.ActiveSessionID() != "" {
						t.Fatal("WithContext retained a deleted session")
					}
				})
			}
		})
	}
}
