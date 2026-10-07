package api

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/gregdel/pushover"
	"github.com/zfogg/spaniel/internal/storage"
)

// AlertDelivery keeps transport policy separate from evaluation. Browser events
// are still broadcast for live data; the browser flag marks whether the client
// should present a notification. Pushover is intentionally best-effort.
type AlertDelivery struct {
	BrowserEnabled   func() bool
	PushoverEnabled  func() bool
	BrowserTemplate  func() string
	PushoverTemplate func() string
	PushoverUserKey  string
	PushoverAPIToken string
	// PushoverTransport exists so delivery can be tested without a network
	// dependency. Nil uses the production Pushover client.
	PushoverTransport  func(title, body string, priority int) error
	RecordEvent        func(*storage.AlertEvent)
	RecordNotification func(*storage.NotificationRecord)
}

var alertDelivery struct {
	sync.RWMutex
	value AlertDelivery
}

func ConfigureAlertDelivery(d AlertDelivery) {
	alertDelivery.Lock()
	alertDelivery.value = d
	alertDelivery.Unlock()
}
func currentAlertDelivery() AlertDelivery {
	alertDelivery.RLock()
	defer alertDelivery.RUnlock()
	return alertDelivery.value
}

func deliverPushover(rule *storage.AlertRule, instance *storage.AlertInstance, transition string) error {
	d := currentAlertDelivery()
	if !rule.PushoverEnabled || d.PushoverEnabled == nil || !d.PushoverEnabled() || d.PushoverUserKey == "" || d.PushoverAPIToken == "" {
		return nil
	}
	labels := strings.Trim(instance.GroupKey, "all")
	template := "{rule} is {transition}{group} (value {value})"
	if d.PushoverTemplate != nil && d.PushoverTemplate() != "" {
		template = d.PushoverTemplate()
	}
	body := renderAlertTemplate(template, rule, instance, transition, labels)
	m := pushover.NewMessageWithTitle(body, "Spaniel · "+strings.ToUpper(rule.Severity))
	switch rule.Severity {
	case "critical":
		m.Priority = pushover.PriorityHigh
	case "info":
		m.Priority = pushover.PriorityLow
	}
	if d.RecordEvent != nil {
		d.RecordEvent(&storage.AlertEvent{RuleID: rule.ID, GroupKey: instance.GroupKey, Kind: "notification_pushover_attempted", State: instance.State, Value: instance.Value, Detail: body})
	}
	if d.PushoverTransport != nil {
		err := d.PushoverTransport(m.Title, body, int(m.Priority))
		if err != nil {
			log.Printf("alert pushover delivery failed rule=%q group=%q transition=%s: %v", rule.ID, instance.GroupKey, transition, err)
			if d.RecordEvent != nil {
				d.RecordEvent(&storage.AlertEvent{RuleID: rule.ID, GroupKey: instance.GroupKey, Kind: "notification_pushover_failed", State: instance.State, Value: instance.Value, Detail: err.Error()})
			}
			return err
		}
	} else {
		_, err := pushover.New(d.PushoverAPIToken).SendMessage(m, pushover.NewRecipient(d.PushoverUserKey))
		if err != nil {
			log.Printf("alert pushover delivery failed rule=%q group=%q transition=%s: %v", rule.ID, instance.GroupKey, transition, err)
			if d.RecordEvent != nil {
				d.RecordEvent(&storage.AlertEvent{RuleID: rule.ID, GroupKey: instance.GroupKey, Kind: "notification_pushover_failed", State: instance.State, Value: instance.Value, Detail: err.Error()})
			}
			return err
		}
	}
	log.Printf("alert pushover delivery sent rule=%q group=%q transition=%s", rule.ID, instance.GroupKey, transition)
	if d.RecordEvent != nil {
		d.RecordEvent(&storage.AlertEvent{RuleID: rule.ID, GroupKey: instance.GroupKey, Kind: "notification_pushover", State: instance.State, Value: instance.Value, Detail: body})
	}
	return nil
}

// renderAlertTemplate intentionally keeps templates small and predictable. The
// same tokens work for browser and Pushover destinations.
func renderAlertTemplate(template string, rule *storage.AlertRule, instance *storage.AlertInstance, transition, group string) string {
	value := "—"
	if instance.Value != nil {
		value = fmt.Sprintf("%.4g", *instance.Value)
	}
	threshold := "—"
	var c struct {
		Value float64 `json:"value"`
	}
	_ = json.Unmarshal([]byte(rule.ConditionJSON), &c)
	threshold = fmt.Sprintf("%.4g", c.Value)
	replacements := map[string]string{"{rule}": rule.Name, "{severity}": rule.Severity, "{transition}": transition, "{state}": instance.State, "{group}": func() string {
		if group == "" {
			return ""
		}
		return " — " + group
	}(), "{value}": value, "{threshold}": threshold}
	for token, value := range replacements {
		template = strings.ReplaceAll(template, token, value)
	}
	return template
}
