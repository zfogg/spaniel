package api

import (
	"errors"
	"testing"
	"time"

	"github.com/zfogg/spaniel/internal/storage"
)

func TestEmitAlertRetriesFailedPushoverWithoutHub(t *testing.T) {
	previous := currentAlertDelivery()
	t.Cleanup(func() { ConfigureAlertDelivery(previous) })
	value := 4.0
	rule := &storage.AlertRule{ID: "rule", Name: "Rule", Severity: "critical", PushoverEnabled: true}
	instance := &storage.AlertInstance{RuleID: rule.ID, GroupKey: "all", State: "firing", Value: &value}
	var events []*storage.AlertEvent
	attempts := 0
	ConfigureAlertDelivery(AlertDelivery{
		PushoverEnabled:  func() bool { return true },
		PushoverUserKey:  "user",
		PushoverAPIToken: "token",
		PushoverTransport: func(_, _ string, _ int) error {
			attempts++
			return errors.New("offline")
		},
		RecordEvent: func(event *storage.AlertEvent) { events = append(events, event) },
	})

	emitAlert(nil, rule, instance, "firing", time.Now(), false)
	if attempts != 1 || instance.LastPushoverNotifiedAt != nil || instance.LastNotifiedAt != nil {
		t.Fatalf("failed delivery must remain retryable: attempts=%d instance=%#v", attempts, instance)
	}
	if len(events) != 2 || events[0].Kind != "notification_pushover_attempted" || events[1].Kind != "notification_pushover_failed" {
		t.Fatalf("want attempted then failed events, got %#v", events)
	}
}
