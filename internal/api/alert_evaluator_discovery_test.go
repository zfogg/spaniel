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
