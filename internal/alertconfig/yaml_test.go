package alertconfig

import (
	"strings"
	"testing"
)

func TestAlertYAMLRoundTripPreservesLifecycleAndDiscovery(t *testing.T) {
	definition, err := Parse([]byte(`version: 1
id: missing-service-errors
name: Missing service errors
query: SELECT service_name, count(*) AS value FROM telemetry_spans GROUP BY service_name
group_by:
  - service_name
condition:
  kind: threshold
  operator: "<"
  threshold: 1
pending_for: 15s
cooldown: 5m
repeat_interval: 15m
severity: warning
enabled: true
browser_enabled: true
pushover_enabled: false
instance_discovery:
  query: SELECT DISTINCT service_name FROM telemetry_spans
  every: 15s
  stale_after: 1h
annotations:
  runbook: https://example.test/runbook
`))
	if err != nil {
		t.Fatal(err)
	}
	rule, err := definition.Alert("fallback-id")
	if err != nil {
		t.Fatal(err)
	}
	if rule.ID != "missing-service-errors" || rule.PendingForNs != 15_000_000_000 || rule.CooldownNs != 300_000_000_000 || rule.InstanceDiscoverySQL == "" || rule.InstanceDiscoveryIntervalNs != 15_000_000_000 {
		t.Fatalf("YAML settings were not preserved: %#v", rule)
	}
	data, err := Marshal(rule)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{"id: missing-service-errors", "threshold: 1", "pending_for: 15s", "cooldown: 5m0s", "repeat_interval: 15m0s", "instance_discovery:", "every: 15s", "stale_after: 1h0m0s", "runbook:"} {
		if !strings.Contains(text, want) {
			t.Errorf("export missing %q:\n%s", want, text)
		}
	}
}

func TestAlertYAMLRejectsRetiredOwnershipFields(t *testing.T) {
	_, err := Parse([]byte(`version: 1
name: No owners
query: SELECT 1 AS value
condition:
  operator: ">"
  threshold: 1
pending_for: 0s
cooldown: 0s
severity: warning
owner: a-team
`))
	if err == nil || !strings.Contains(err.Error(), "owner") {
		t.Fatalf("retired owner field should be rejected, got %v", err)
	}
}
