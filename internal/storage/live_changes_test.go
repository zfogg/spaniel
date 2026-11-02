package storage

import (
	"context"
	"slices"
	"testing"
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
