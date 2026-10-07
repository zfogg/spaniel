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
		evaluateAlerts(ctx, store, hub, time.Now(), interval)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				evaluateAlerts(ctx, store, hub, now, interval)
			}
		}
	}()
}

type alertCondition struct {
	Kind     string   `json:"kind"`
	Operator string   `json:"operator"`
	Value    float64  `json:"value"`
	Pattern  string   `json:"pattern"`
	RuleIDs  []string `json:"rule_ids"`
}

func evaluateAlerts(parent context.Context, store *storage.DB, hub *ws.Hub, now time.Time, interval time.Duration) {
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
		started := time.Now()
		err := evaluateAlertRule(ctx, store, hub, rule, rules, now)
		duration := time.Since(started).Nanoseconds()
		next := now.Add(interval).UnixNano()
		if err != nil {
			telemetry.Catalog().RecordAlertEvaluation(ctx, "error")
			// Evaluation errors are attached to an instance rather than silently
			// suppressing the rule. This makes malformed historic data observable.
			_ = store.UpsertAlertInstance(&storage.AlertInstance{
				RuleID: rule.ID, GroupKey: "__evaluation_error__", State: "error",
				LastEvaluatedAt: now.UnixNano(), LastError: err.Error(),
			})
			_ = store.RecordAlertEvent(&storage.AlertEvent{RuleID: rule.ID, GroupKey: "__evaluation_error__", Kind: "evaluation_error", State: "error", Detail: err.Error()})
			_ = store.RecordAlertEvaluation(rule.ID, now.UnixNano(), duration, next, err)
			emitAlertSync(hub, rule.ID, "evaluation_error", now)
			continue
		}
		if err := resolveEvaluationError(store, rule, now); err != nil {
			telemetry.Catalog().RecordAlertEvaluation(ctx, "error")
			_ = store.RecordAlertEvaluation(rule.ID, now.UnixNano(), duration, next, err)
			emitAlertSync(hub, rule.ID, "evaluation_error", now)
			continue
		}
		_ = store.RecordAlertEvaluation(rule.ID, now.UnixNano(), duration, next, nil)
		emitAlertSync(hub, rule.ID, "evaluation", now)
		telemetry.Catalog().RecordAlertEvaluation(ctx, "ok")
	}
}

func emitAlertSync(hub *ws.Hub, ruleID, reason string, now time.Time) {
	if hub == nil {
		return
	}
	hub.Broadcast(&ws.Event{
		Type:      "alert_sync",
		Timestamp: now.UnixNano(),
		Payload:   map[string]string{"ruleId": ruleID, "reason": reason},
	})
}

// resolveEvaluationError keeps historical failures in alert_events but removes
// their stale error status from the current instance board after a good run.
func resolveEvaluationError(store *storage.DB, rule *storage.AlertRule, now time.Time) error {
	for _, instance := range rule.Instances {
		if instance.GroupKey != "__evaluation_error__" || instance.State != "error" {
			continue
		}
		instance.State = "resolved"
		instance.ResolvedAt = ptrInt64(now.UnixNano())
		instance.LastEvaluatedAt = now.UnixNano()
		instance.LastError = ""
		if err := store.UpsertAlertInstance(instance); err != nil {
			return fmt.Errorf("resolve prior evaluation error: %w", err)
		}
		return store.RecordAlertEvent(&storage.AlertEvent{RuleID: rule.ID, GroupKey: instance.GroupKey, Kind: "evaluation_recovered", State: "resolved", Detail: "a later evaluation succeeded"})
	}
	return nil
}

func evaluateAlertRule(parent context.Context, store *storage.DB, hub *ws.Hub, rule *storage.AlertRule, allRules []*storage.AlertRule, now time.Time) error {
	var condition alertCondition
	if err := json.Unmarshal([]byte(rule.ConditionJSON), &condition); err != nil {
		return fmt.Errorf("read alert condition: %w", err)
	}
	if (condition.Kind != "threshold" && condition.Kind != "count" && condition.Kind != "no_data" && condition.Kind != "log_match" && condition.Kind != "any_of" && condition.Kind != "all_of") ||
		((condition.Kind == "threshold" || condition.Kind == "count") && !validAlertOperator(condition.Operator)) ||
		(condition.Kind == "log_match" && (condition.Pattern == "" || (condition.Operator != "" && !validAlertOperator(condition.Operator)))) {
		return fmt.Errorf("unsupported alert condition")
	}
	if condition.Kind == "any_of" || condition.Kind == "all_of" {
		if len(condition.RuleIDs) == 0 || len(alertGroupColumns(rule.GroupByJSON)) > 0 {
			return fmt.Errorf("composite alerts require source rules and cannot group results")
		}
		byID := make(map[string]*storage.AlertRule, len(allRules))
		for _, source := range allRules {
			byID[source.ID] = source
		}
		active := 0
		seen := make(map[string]bool, len(condition.RuleIDs))
		for _, id := range condition.RuleIDs {
			if id == rule.ID || seen[id] {
				return fmt.Errorf("composite alert source rules must be unique and cannot reference itself")
			}
			seen[id] = true
			source := byID[id]
			if source == nil {
				return fmt.Errorf("composite alert source rule %q does not exist", id)
			}
			if source.Enabled && hasFiringInstance(source) {
				active++
			}
		}
		value := float64(active)
		breached := active > 0
		if condition.Kind == "all_of" {
			breached = active == len(condition.RuleIDs)
		}
		return advanceAlertInstance(store, hub, rule, "all", map[string]string{}, value, breached, now)
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
	if err := validateAlertColumnsForCondition(columns, alertGroupColumns(rule.GroupByJSON), rule.ConditionJSON); err != nil {
		return err
	}
	rows := rowsForColumns(columns, values)
	groups := alertGroupColumns(rule.GroupByJSON)
	targets, err := discoverAlertInstanceTargets(ctx, store, rule, groups, now)
	if err != nil {
		return err
	}
	if condition.Kind == "no_data" {
		value := float64(len(rows))
		return advanceAlertInstance(store, hub, rule, "all", map[string]string{}, value, len(rows) == 0, now)
	}
	if condition.Kind == "log_match" {
		type logOutcome struct {
			labels map[string]string
			count  float64
		}
		outcomes := make(map[string]*logOutcome, len(rows))
		for _, row := range rows {
			key := "all"
			labels := map[string]string{}
			for _, column := range groups {
				if group, ok := row[column]; ok {
					labels[column] = fmt.Sprint(group)
				}
			}
			if len(labels) > 0 {
				key = StableAlertGroupKey(labels)
			}
			result := outcomes[key]
			if result == nil {
				result = &logOutcome{labels: labels}
				outcomes[key] = result
			}
			if strings.Contains(fmt.Sprint(row["message"]), condition.Pattern) {
				result.count++
			}
		}
		seen := make(map[string]bool, len(outcomes))
		for key, outcome := range outcomes {
			seen[key] = true
			breached := outcome.count > 0
			if condition.Operator != "" {
				breached = compareAlert(outcome.count, condition)
			}
			if err := advanceAlertInstance(store, hub, rule, key, outcome.labels, outcome.count, breached, now); err != nil {
				return err
			}
		}
		for _, target := range targets {
			if seen[target.GroupKey] {
				continue
			}
			labels, err := alertTargetLabels(target)
			if err != nil {
				return err
			}
			seen[target.GroupKey] = true
			breached := condition.Operator != "" && compareAlert(0, condition)
			if err := advanceAlertInstance(store, hub, rule, target.GroupKey, labels, 0, breached, now); err != nil {
				return err
			}
		}
		return resolveMissingAlertInstances(store, hub, rule, seen, now)
	}

	seen := make(map[string]bool, len(rows))
	for _, row := range rows {
		key := "all"
		labels := map[string]string{}
		for _, column := range groups {
			if group, ok := row[column]; ok {
				labels[column] = fmt.Sprint(group)
			}
		}
		if len(labels) > 0 {
			key = StableAlertGroupKey(labels)
		}
		value, breached := 0.0, false
		if condition.Kind != "log_match" {
			var ok bool
			value, ok = alertNumber(row["value"])
			if !ok {
				return fmt.Errorf("alert query returned a non-numeric value")
			}
			breached = compareAlert(value, condition)
		}
		seen[key] = true
		if err := advanceAlertInstance(store, hub, rule, key, labels, value, breached, now); err != nil {
			return err
		}
	}
	for _, target := range targets {
		if seen[target.GroupKey] {
			continue
		}
		labels, err := alertTargetLabels(target)
		if err != nil {
			return err
		}
		seen[target.GroupKey] = true
		if err := advanceAlertInstance(store, hub, rule, target.GroupKey, labels, 0, compareAlert(0, condition), now); err != nil {
			return err
		}
	}
	// A group that disappeared is no longer breaching. Resolve it rather than
	// leaving a stale firing alert behind.
	return resolveMissingAlertInstances(store, hub, rule, seen, now)
}

// discoverAlertInstanceTargets keeps the expected group universe separate from
// the condition query. This lets a service that has stopped reporting receive a
// zero-valued evaluation rather than falling out of the alert entirely.
func discoverAlertInstanceTargets(ctx context.Context, store *storage.DB, rule *storage.AlertRule, groups []string, now time.Time) ([]*storage.AlertInstanceTarget, error) {
	if rule.InstanceDiscoverySQL == "" {
		return nil, nil
	}
	if len(groups) == 0 {
		return nil, fmt.Errorf("instance discovery requires group_by columns")
	}
	staleAfter := rule.InstanceDiscoveryStaleAfterNs
	if staleAfter <= 0 {
		staleAfter = int64((24 * time.Hour).Nanoseconds())
	}
	if rule.InstanceDiscoveryLastRunAt == 0 || rule.InstanceDiscoveryIntervalNs <= 0 || now.UnixNano()-rule.InstanceDiscoveryLastRunAt >= rule.InstanceDiscoveryIntervalNs {
		columns, values, _, err := store.ReadOnlyQuery(ctx, rule.InstanceDiscoverySQL, 1000)
		if err != nil {
			return nil, fmt.Errorf("execute instance discovery query: %w", err)
		}
		if err := validateAlertDiscoveryColumns(columns, groups); err != nil {
			return nil, err
		}
		existing, err := store.ListAlertInstanceTargets(rule.ID, 0)
		if err != nil {
			return nil, err
		}
		known := make(map[string]*storage.AlertInstanceTarget, len(existing))
		for _, target := range existing {
			known[target.GroupKey] = target
		}
		for _, row := range rowsForColumns(columns, values) {
			labels := make(map[string]string, len(groups))
			for _, column := range groups {
				labels[column] = fmt.Sprint(row[column])
			}
			key := StableAlertGroupKey(labels)
			encoded, _ := json.Marshal(labels)
			discoveredAt := now.UnixNano()
			if old := known[key]; old != nil {
				discoveredAt = old.DiscoveredAt
			}
			if err := store.UpsertAlertInstanceTarget(&storage.AlertInstanceTarget{RuleID: rule.ID, GroupKey: key, LabelsJSON: string(encoded), DiscoveredAt: discoveredAt, LastSeenAt: now.UnixNano()}); err != nil {
				return nil, err
			}
		}
		if err := store.RecordAlertInstanceDiscoveryRun(rule.ID, now.UnixNano()); err != nil {
			return nil, err
		}
	}
	return store.ListAlertInstanceTargets(rule.ID, now.UnixNano()-staleAfter)
}

func alertTargetLabels(target *storage.AlertInstanceTarget) (map[string]string, error) {
	labels := map[string]string{}
	if err := json.Unmarshal([]byte(target.LabelsJSON), &labels); err != nil {
		return nil, fmt.Errorf("read discovered instance target %q: %w", target.GroupKey, err)
	}
	return labels, nil
}

func validateAlertDiscoveryColumns(columns, groups []string) error {
	for _, group := range groups {
		found := false
		for _, column := range columns {
			if strings.EqualFold(column, group) {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("instance discovery query does not return group column %q", group)
		}
	}
	return nil
}

func resolveMissingAlertInstances(store *storage.DB, hub *ws.Hub, rule *storage.AlertRule, seen map[string]bool, now time.Time) error {
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

func hasFiringInstance(rule *storage.AlertRule) bool {
	for _, instance := range rule.Instances {
		if instance.State == "firing" {
			return true
		}
	}
	return false
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

func validateAlertColumnsForCondition(columns, groups []string, rawCondition string) error {
	var condition alertCondition
	if err := json.Unmarshal([]byte(rawCondition), &condition); err != nil {
		return fmt.Errorf("read alert condition: %w", err)
	}
	if condition.Kind == "no_data" {
		if len(groups) > 0 {
			return fmt.Errorf("no_data alerts cannot group results")
		}
		return nil
	}
	if condition.Kind == "log_match" {
		hasMessage := false
		for _, column := range columns {
			if strings.EqualFold(column, "message") {
				hasMessage = true
				break
			}
		}
		if !hasMessage {
			return fmt.Errorf("log_match alert query must return a message column")
		}
		for _, group := range groups {
			found := false
			for _, column := range columns {
				if strings.EqualFold(column, group) {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("alert query does not return group column %q", group)
			}
		}
		return nil
	}
	return validateAlertColumns(columns, groups)
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
		if current.State != "resolved" {
			current.State = "resolved"
			current.ResolvedAt = ptrInt64(now.UnixNano())
		}
		if err := store.UpsertAlertInstance(current); err != nil {
			return err
		}
		if prior != "" && prior != "resolved" {
			emitAlert(hub, rule, current, "resolved", now, true)
			_ = store.RecordAlertEvent(&storage.AlertEvent{RuleID: rule.ID, GroupKey: key, Kind: "resolved", State: current.State, Value: current.Value})
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
		silenced, err := store.IsAlertSilenced(rule.ID, key, now.UnixNano())
		if err != nil {
			return err
		}
		if !silenced {
			emitAlert(hub, rule, current, "firing", now, false)
			_ = store.RecordAlertEvent(&storage.AlertEvent{RuleID: rule.ID, GroupKey: key, Kind: "firing", State: current.State, Value: current.Value})
		} else {
			_ = store.RecordAlertEvent(&storage.AlertEvent{RuleID: rule.ID, GroupKey: key, Kind: "notification_suppressed", State: current.State, Value: current.Value, Detail: "silenced"})
		}
		if err := store.UpsertAlertInstance(current); err != nil {
			return err
		}
	}
	if prior == "firing" && current.State == "firing" && rule.RepeatIntervalNs > 0 && current.LastNotifiedAt != nil && now.UnixNano()-*current.LastNotifiedAt >= rule.RepeatIntervalNs {
		// Acknowledgement is operator metadata, not a notification mute: a
		// firing incident may still repeat. Use an explicit silence to suppress
		// both its initial and repeat deliveries.
		silenced, err := store.IsAlertSilenced(rule.ID, key, now.UnixNano())
		if err != nil {
			return err
		}
		if silenced {
			_ = store.RecordAlertEvent(&storage.AlertEvent{RuleID: rule.ID, GroupKey: key, Kind: "notification_suppressed", State: current.State, Value: current.Value, Detail: "silenced repeat"})
		} else {
			emitAlert(hub, rule, current, "repeat", now, true)
			_ = store.RecordAlertEvent(&storage.AlertEvent{RuleID: rule.ID, GroupKey: key, Kind: "repeat", State: current.State, Value: current.Value})
		}
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
	d := currentAlertDelivery()
	browser := rule.BrowserEnabled && (d.BrowserEnabled == nil || d.BrowserEnabled())
	template := "{rule}: {transition} ({value})"
	if d.BrowserTemplate != nil && d.BrowserTemplate() != "" {
		template = d.BrowserTemplate()
	}
	body := renderAlertTemplate(template, rule, instance, transition, strings.Trim(instance.GroupKey, "all"))
	payload := map[string]any{"ruleId": rule.ID, "ruleName": rule.Name, "severity": rule.Severity, "state": instance.State, "transition": transition, "groupLabels": labels, "currentValue": instance.Value, "threshold": condition.Value, "operator": condition.Operator, "link": "/alerts", "browser": browser, "body": body}
	return &ws.Event{Type: "alert", Timestamp: now.UnixNano(), Payload: payload}
}

// emitAlert enforces notification cooldown without stopping state evaluation or
// persistence. Resolutions are always delivered so an acknowledged/firing
// incident has a visible conclusion.
func emitAlert(hub *ws.Hub, rule *storage.AlertRule, instance *storage.AlertInstance, transition string, now time.Time, force bool) {
	d := currentAlertDelivery()
	record := func(kind, detail string) {
		if d.RecordEvent != nil {
			d.RecordEvent(&storage.AlertEvent{RuleID: rule.ID, GroupKey: instance.GroupKey, Kind: kind, State: instance.State, Value: instance.Value, Detail: detail})
		}
	}
	if hub == nil {
		telemetry.Catalog().RecordAlertNotification(context.Background(), transition, "suppressed")
		record("notification_suppressed", "notification transport unavailable")
		return
	}
	if !force && rule.CooldownNs > 0 && instance.LastNotifiedAt != nil && now.UnixNano()-*instance.LastNotifiedAt < rule.CooldownNs {
		telemetry.Catalog().RecordAlertNotification(context.Background(), transition, "suppressed")
		record("notification_suppressed", "cooldown active")
		return
	}
	n := now.UnixNano()
	instance.LastNotifiedAt = &n
	// Event delivery is best effort; state is already durable and is never made
	// conditional on a websocket client being present.
	event := alertEvent(rule, instance, transition, now)
	hub.Broadcast(event)
	payload, _ := event.Payload.(map[string]any)
	if payload["browser"] == true && d.RecordEvent != nil {
		d.RecordEvent(&storage.AlertEvent{RuleID: rule.ID, GroupKey: instance.GroupKey, Kind: "notification_browser", State: instance.State, Value: instance.Value, Detail: fmt.Sprint(payload["body"])})
	} else {
		record("notification_browser_suppressed", "disabled for this rule or globally")
	}
	if !rule.PushoverEnabled || d.PushoverEnabled == nil || !d.PushoverEnabled() || d.PushoverUserKey == "" || d.PushoverAPIToken == "" {
		record("notification_pushover_suppressed", "disabled or not configured")
	} else if err := deliverPushover(rule, instance, transition); err != nil {
		telemetry.Catalog().RecordAlertNotification(context.Background(), transition, "error")
	}
	telemetry.Catalog().RecordAlertNotification(context.Background(), transition, "sent")
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
	case int8:
		return float64(v), true
	case int16:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case int:
		return float64(v), true
	case uint8:
		return float64(v), true
	case uint16:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	case uint:
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
