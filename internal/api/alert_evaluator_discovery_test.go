package api

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/ws"
)

func TestEvaluateAlertRule_CreatesZeroValueInstanceForDiscoveredTarget(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "discovery.duckdb"))
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	rule := &storage.AlertRule{
		ID:                            "missing-service",
		Name:                          "Missing service errors",
		QuerySQL:                      "SELECT service_name, count(*) AS value FROM telemetry_spans WHERE 1 = 0 GROUP BY service_name",
		ConditionJSON:                 `{"kind":"threshold","operator":"<","value":1}`,
		GroupByJSON:                   `["service_name"]`,
		Enabled:                       true,
		InstanceDiscoverySQL:          "SELECT 'late-service' AS service_name",
		InstanceDiscoveryIntervalNs:   int64(time.Second),
		InstanceDiscoveryStaleAfterNs: int64(time.Hour),
	}
	if err := store.CreateAlertRule(rule); err != nil {
		t.Fatalf("create alert rule: %v", err)
	}

	now := time.Now()
	if err := evaluateAlertRule(context.Background(), store, ws.NewHub(), rule, []*storage.AlertRule{rule}, now); err != nil {
		t.Fatalf("evaluate rule: %v", err)
	}
	got, err := store.GetAlertRule(rule.ID)
	if err != nil {
		t.Fatalf("get evaluated rule: %v", err)
	}
	if len(got.Instances) != 1 {
		t.Fatalf("instances=%#v, want one discovered target", got.Instances)
	}
	instance := got.Instances[0]
	if instance.GroupKey != "service_name=late-service" || instance.Value == nil || *instance.Value != 0 || instance.State != "firing" {
		t.Fatalf("instance=%#v, want zero-valued firing discovered target", instance)
	}
	targets, err := store.ListAlertInstanceTargets(rule.ID, now.Add(-time.Minute).UnixNano())
	if err != nil {
		t.Fatalf("list discovery targets: %v", err)
	}
	if len(targets) != 1 || targets[0].GroupKey != instance.GroupKey {
		t.Fatalf("targets=%#v, want matching discovered target", targets)
	}
}

func TestValidateAlertDiscoveryColumns(t *testing.T) {
	if err := validateAlertDiscoveryColumns([]string{"service_name", "deployment_environment"}, []string{"service_name"}); err != nil {
		t.Fatalf("matching discovery columns: %v", err)
	}
	if err := validateAlertDiscoveryColumns([]string{"service_name"}, []string{"service_name", "host_name"}); err == nil {
		t.Fatal("missing group column should fail discovery validation")
	}
}

func TestAdvanceAlertInstance_SilenceSuppressesRepeat(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "silence-repeat.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	now := time.Now()
	previousNotification := now.Add(-2 * time.Minute).UnixNano()
	rule := &storage.AlertRule{ID: "repeat-rule", Name: "Repeat rule", QuerySQL: "SELECT 1 AS value", ConditionJSON: `{"kind":"threshold","operator":">","value":0}`, GroupByJSON: `[]`, Enabled: true, RepeatIntervalNs: int64(time.Minute)}
	if err := store.CreateAlertRule(rule); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertAlertInstance(&storage.AlertInstance{RuleID: rule.ID, GroupKey: "all", LabelsJSON: "{}", State: "firing", Value: func() *float64 { v := 1.0; return &v }(), LastNotifiedAt: &previousNotification}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateAlertSilence(&storage.AlertSilence{RuleID: rule.ID, StartsAt: now.Add(-time.Minute).UnixNano(), EndsAt: now.Add(time.Hour).UnixNano()}); err != nil {
		t.Fatal(err)
	}
	persisted, err := store.GetAlertRule(rule.ID)
	if err != nil {
		t.Fatal(err)
	}
	rule.Instances = persisted.Instances
	if err := advanceAlertInstance(store, ws.NewHub(), rule, "all", map[string]string{}, 1, true, now); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetAlertRule(rule.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Instances[0].LastNotifiedAt == nil || *got.Instances[0].LastNotifiedAt != previousNotification {
		t.Fatalf("silenced repeat updated last notification: %#v", got.Instances[0])
	}
}

func TestAdvanceAlertInstance_StartsRepeatDeliveryWithoutPriorNotification(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "repeat-recovery.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	now := time.Now()
	rule := &storage.AlertRule{ID: "repeat-recovery", Name: "Repeat recovery", QuerySQL: "SELECT 1 AS value", ConditionJSON: `{"kind":"threshold","operator":">","value":0}`, GroupByJSON: `[]`, Enabled: true, BrowserEnabled: true, RepeatIntervalNs: int64(time.Minute)}
	if err := store.CreateAlertRule(rule); err != nil {
		t.Fatal(err)
	}
	value := 1.0
	if err := store.UpsertAlertInstance(&storage.AlertInstance{RuleID: rule.ID, GroupKey: "all", LabelsJSON: "{}", State: "firing", Value: &value}); err != nil {
		t.Fatal(err)
	}
	persisted, err := store.GetAlertRule(rule.ID)
	if err != nil {
		t.Fatal(err)
	}
	rule.Instances = persisted.Instances
	if err := advanceAlertInstance(store, ws.NewHub(), rule, "all", map[string]string{}, value, true, now); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetAlertRule(rule.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Instances[0].LastNotifiedAt == nil {
		t.Fatalf("repeat without prior notification did not mark delivery: %#v", got.Instances[0])
	}
}
