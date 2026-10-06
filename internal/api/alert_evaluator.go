package api

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/telemetry"
	"github.com/zfogg/spaniel/internal/ws"
)

// StartAlertEvaluator evaluates persisted enabled rules until ctx is cancelled.
// Keeping this outside the HTTP router means alerts continue to transition when
// nobody happens to be looking at the alerts page.
func StartAlertEvaluator(ctx context.Context, store *storage.DB, hub *ws.Hub, interval time.Duration) {
	if interval <= 0 {
		interval = 15 * time.Second
	}
	go func() {
		evaluateAlerts(ctx, store, hub, time.Now())
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				evaluateAlerts(ctx, store, hub, now)
			}
		}
	}()
}

type alertCondition struct {
	Kind     string  `json:"kind"`
	Operator string  `json:"operator"`
	Value    float64 `json:"value"`
}

func evaluateAlerts(parent context.Context, store *storage.DB, hub *ws.Hub, now time.Time) {
	ctx, span := otel.Tracer("spaniel/alerts").Start(parent, "spaniel.alerts.evaluate", trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()

	store = store.WithContext(ctx)
	rules, err := store.ListAlertRules()
	if err != nil {
		telemetry.Catalog().RecordAlertEvaluation(ctx, "error")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return
	}
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		if err := evaluateAlertRule(ctx, store, hub, rule, now); err != nil {
			telemetry.Catalog().RecordAlertEvaluation(ctx, "error")
			// Evaluation errors are attached to an instance rather than silently
			// suppressing the rule. This makes malformed historic data observable.
			_ = store.UpsertAlertInstance(&storage.AlertInstance{
				RuleID: rule.ID, GroupKey: "__evaluation_error__", State: "error",
				LastEvaluatedAt: now.UnixNano(), LastError: err.Error(),
			})
			continue
		}
		telemetry.Catalog().RecordAlertEvaluation(ctx, "ok")
	}
}

func evaluateAlertRule(parent context.Context, store *storage.DB, hub *ws.Hub, rule *storage.AlertRule, now time.Time) error {
	var condition alertCondition
	if err := json.Unmarshal([]byte(rule.ConditionJSON), &condition); err != nil {
		return fmt.Errorf("read alert condition: %w", err)
	}
	if condition.Kind != "threshold" || !validAlertOperator(condition.Operator) {
		return fmt.Errorf("unsupported alert condition")
	}
	if err := storage.ValidateReadOnlySQL(rule.QuerySQL); err != nil {
		return fmt.Errorf("validate alert query: %w", err)
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	columns, values, _, err := store.ReadOnlyQuery(ctx, rule.QuerySQL, 1000)
	if err != nil {
		return fmt.Errorf("execute alert query: %w", err)
	}
	if err := validateAlertColumns(columns, alertGroupColumns(rule.GroupByJSON)); err != nil {
		return err
	}
	rows := rowsForColumns(columns, values)

	seen := make(map[string]bool, len(rows))
	for _, row := range rows {
		key := "all"
		labels := map[string]string{}
		for _, column := range alertGroupColumns(rule.GroupByJSON) {
			if group, ok := row[column]; ok {
				labels[column] = fmt.Sprint(group)
			}
		}
		if len(labels) > 0 {
			key = StableAlertGroupKey(labels)
		}
		value, ok := alertNumber(row["value"])
		if !ok {
			return fmt.Errorf("alert query returned a non-numeric value")
		}
		seen[key] = true
		if err := advanceAlertInstance(store, hub, rule, key, labels, value, compareAlert(value, condition), now); err != nil {
			return err
		}
	}
	// A group that disappeared is no longer breaching. Resolve it rather than
	// leaving a stale firing alert behind.
	for _, instance := range rule.Instances {
		if instance.GroupKey == "__evaluation_error__" || seen[instance.GroupKey] || instance.State == "resolved" {
			continue
		}
		instance.State = "resolved"
		instance.ResolvedAt = ptrInt64(now.UnixNano())
		instance.LastEvaluatedAt = now.UnixNano()
		instance.LastError = ""
		if err := store.UpsertAlertInstance(instance); err != nil {
			return err
		}
		emitAlert(hub, rule, instance, "resolved", now, true)
		if err := store.UpsertAlertInstance(instance); err != nil {
			return err
		}
	}
	return nil
}

func alertGroupColumns(raw string) []string {
	var columns []string
	_ = json.Unmarshal([]byte(raw), &columns)
	return columns
}

func validateAlertColumns(columns, groups []string) error {
	has := func(name string) bool {
		for _, column := range columns {
			if strings.EqualFold(column, name) {
				return true
			}
		}
		return false
	}
	if !has("value") {
		return fmt.Errorf("alert query must return a numeric value column")
	}
	for _, group := range groups {
		if !has(group) {
			return fmt.Errorf("alert query does not return group column %q", group)
		}
	}
	return nil
}

func advanceAlertInstance(store *storage.DB, hub *ws.Hub, rule *storage.AlertRule, key string, labels map[string]string, value float64, breached bool, now time.Time) error {
	var current *storage.AlertInstance
	for _, instance := range rule.Instances {
		if instance.GroupKey == key {
			current = instance
			break
		}
	}
	if current == nil {
		current = &storage.AlertInstance{RuleID: rule.ID, GroupKey: key}
	}
	labelJSON, _ := json.Marshal(labels)
	current.LabelsJSON = string(labelJSON)
	current.Value = &value
	current.LastEvaluatedAt = now.UnixNano()
	current.LastError = ""
	prior := current.State
	if !breached {
		if current.State != "" && current.State != "resolved" {
			current.State = "resolved"
			current.ResolvedAt = ptrInt64(now.UnixNano())
		}
		if err := store.UpsertAlertInstance(current); err != nil {
			return err
		}
		if prior != "" && prior != "resolved" {
			emitAlert(hub, rule, current, "resolved", now, true)
			if err := store.UpsertAlertInstance(current); err != nil {
				return err
			}
		}
		return nil
	}
	if current.State == "" || current.State == "resolved" {
		current.State = "pending"
		current.FirstPendingAt = ptrInt64(now.UnixNano())
		current.ResolvedAt = nil
		current.AcknowledgedAt = nil
	}
	if current.State == "pending" && (rule.PendingForNs <= 0 || now.UnixNano()-*current.FirstPendingAt >= rule.PendingForNs) {
		current.State = "firing"
		current.FiredAt = ptrInt64(now.UnixNano())
	}
	if err := store.UpsertAlertInstance(current); err != nil {
		return err
	}
	if prior != "firing" && current.State == "firing" {
		emitAlert(hub, rule, current, "firing", now, false)
		if err := store.UpsertAlertInstance(current); err != nil {
			return err
		}
	}
	return nil
}

func alertEvent(rule *storage.AlertRule, instance *storage.AlertInstance, transition string, now time.Time) *ws.Event {
	labels := map[string]string{}
	_ = json.Unmarshal([]byte(instance.LabelsJSON), &labels)
	var condition alertCondition
	_ = json.Unmarshal([]byte(rule.ConditionJSON), &condition)
	payload := map[string]any{"ruleId": rule.ID, "ruleName": rule.Name, "severity": rule.Severity, "state": instance.State, "transition": transition, "groupLabels": labels, "currentValue": instance.Value, "threshold": condition.Value, "operator": condition.Operator, "link": "/alerts"}
	return &ws.Event{Type: "alert", Timestamp: now.UnixNano(), Payload: payload}
}

// emitAlert enforces notification cooldown without stopping state evaluation or
// persistence. Resolutions are always delivered so an acknowledged/firing
// incident has a visible conclusion.
func emitAlert(hub *ws.Hub, rule *storage.AlertRule, instance *storage.AlertInstance, transition string, now time.Time, force bool) {
	if hub == nil {
		return
	}
	if !force && rule.CooldownNs > 0 && instance.LastNotifiedAt != nil && now.UnixNano()-*instance.LastNotifiedAt < rule.CooldownNs {
		return
	}
	n := now.UnixNano()
	instance.LastNotifiedAt = &n
	// Event delivery is best effort; state is already durable and is never made
	// conditional on a websocket client being present.
	hub.Broadcast(alertEvent(rule, instance, transition, now))
}

func validAlertOperator(operator string) bool {
	switch operator {
	case ">", ">=", "<", "<=", "=", "!=":
		return true
	}
	return false
}
func compareAlert(value float64, c alertCondition) bool {
	switch c.Operator {
	case ">":
		return value > c.Value
	case ">=":
		return value >= c.Value
	case "<":
		return value < c.Value
	case "<=":
		return value <= c.Value
	case "=":
		return value == c.Value
	case "!=":
		return value != c.Value
	}
	return false
}
func alertNumber(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int64:
		return float64(v), true
	case int:
		return float64(v), true
	case []byte:
		n, e := strconv.ParseFloat(string(v), 64)
		return n, e == nil
	case string:
		n, e := strconv.ParseFloat(v, 64)
		return n, e == nil
	default:
		return 0, false
	}
}
func ptrInt64(v int64) *int64 { return &v }

// StableAlertGroupKey is kept small and deterministic when a caller builds a
// composite group from alert labels.
func StableAlertGroupKey(labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+labels[k])
	}
	return strings.Join(parts, ",")
}
