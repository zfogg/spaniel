package api

import (
	"encoding/json"
	"fmt"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/zfogg/spaniel/internal/alertconfig"
	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/ws"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

func (r *Router) exportAlertConfig(w http.ResponseWriter, q *http.Request) {
	x, e := r.store.WithContext(q.Context()).GetAlertRule(chi.URLParam(q, "id"))
	if e != nil {
		respondErr(w, q, 404, "alert not found")
		return
	}
	data, e := alertconfig.Marshal(x)
	if e != nil {
		respondErr(w, q, 500, e.Error())
		return
	}
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	_, _ = w.Write(data)
}
func (r *Router) importAlertConfig(w http.ResponseWriter, q *http.Request) {
	data, e := io.ReadAll(http.MaxBytesReader(w, q.Body, 1<<20))
	if e != nil {
		respondErr(w, q, 400, e.Error())
		return
	}
	d, e := alertconfig.Parse(data)
	if e != nil {
		respondErr(w, q, 400, "invalid alert YAML: "+e.Error())
		return
	}
	x, e := d.Alert(uuid.NewString())
	if e != nil {
		respondErr(w, q, 400, e.Error())
		return
	}
	if e = storage.ValidateReadOnlySQL(x.QuerySQL); e != nil {
		respondErr(w, q, 400, e.Error())
		return
	}
	if existing, err := r.store.WithContext(q.Context()).GetAlertRule(x.ID); err == nil && existing.SourceFile != "" {
		respondErr(w, q, http.StatusConflict, "alert is managed by YAML file "+existing.SourceFile+"; edit the file and reload it")
		return
	}
	if e = r.store.WithContext(q.Context()).ReplaceAlertDefinition(x); e != nil {
		respondErr(w, q, 500, e.Error())
		return
	}
	respond(w, x, 1, 1)
}

type alertInput struct {
	Name              string                  `json:"name" validate:"required,max=120"`
	QuerySQL          string                  `json:"query_sql" validate:"required,max=16000"`
	Condition         map[string]any          `json:"condition"`
	GroupBy           []string                `json:"group_by"`
	PendingForNs      int64                   `json:"pending_for_ns"`
	CooldownNs        int64                   `json:"cooldown_ns"`
	RepeatIntervalNs  int64                   `json:"repeat_interval_ns"`
	Owner             string                  `json:"owner" validate:"max=120"`
	Team              string                  `json:"team" validate:"max=120"`
	Severity          string                  `json:"severity" validate:"omitempty,oneof=info warning critical"`
	Enabled           *bool                   `json:"enabled"`
	BrowserEnabled    *bool                   `json:"browser_enabled"`
	PushoverEnabled   *bool                   `json:"pushover_enabled"`
	InstanceDiscovery *instanceDiscoveryInput `json:"instance_discovery"`
	Annotations       map[string]string       `json:"annotations"`
}

// instanceDiscoveryInput supplies the expected label sets for a grouped alert.
// The evaluator periodically records its rows, then evaluates missing targets
// as zero rather than silently omitting them from the instance board.
type instanceDiscoveryInput struct {
	Query        string `json:"query" validate:"max=16000"`
	EveryNs      int64  `json:"every_ns"`
	StaleAfterNs int64  `json:"stale_after_ns"`
}

func alertModel(in alertInput) (*storage.AlertRule, error) {
	if e := storage.ValidateReadOnlySQL(in.QuerySQL); e != nil {
		return nil, e
	}
	var condition alertCondition
	conditionJSON, _ := json.Marshal(in.Condition)
	if err := json.Unmarshal(conditionJSON, &condition); err != nil ||
		((condition.Kind != "threshold" && condition.Kind != "count" && condition.Kind != "no_data" && condition.Kind != "log_match" && condition.Kind != "any_of" && condition.Kind != "all_of") ||
			((condition.Kind == "threshold" || condition.Kind == "count") && !validAlertOperator(condition.Operator)) ||
			(condition.Kind == "log_match" && strings.TrimSpace(condition.Pattern) == "") ||
			((condition.Kind == "any_of" || condition.Kind == "all_of") && len(condition.RuleIDs) == 0)) {
		return nil, &dslError{"alert condition must be threshold, count, no_data, log_match, any_of, or all_of"}
	}
	condition.Pattern = strings.TrimSpace(condition.Pattern)
	groups := make([]string, 0, len(in.GroupBy))
	for _, group := range in.GroupBy {
		group = strings.TrimSpace(group)
		if group != "" {
			groups = append(groups, group)
		}
	}
	discoverySQL, discoveryEvery, discoveryStale := "", int64(0), int64(0)
	if in.InstanceDiscovery != nil && strings.TrimSpace(in.InstanceDiscovery.Query) != "" {
		discoverySQL = strings.TrimSpace(in.InstanceDiscovery.Query)
		if err := storage.ValidateReadOnlySQL(discoverySQL); err != nil {
			return nil, fmt.Errorf("instance discovery query: %w", err)
		}
		if len(groups) == 0 || condition.Kind == "no_data" || condition.Kind == "any_of" || condition.Kind == "all_of" {
			return nil, &dslError{"instance discovery requires a non-composite grouped numeric or log_match alert"}
		}
		if in.InstanceDiscovery.EveryNs < 0 || in.InstanceDiscovery.StaleAfterNs < 0 {
			return nil, &dslError{"instance discovery durations must not be negative"}
		}
		discoveryEvery = in.InstanceDiscovery.EveryNs
		discoveryStale = in.InstanceDiscovery.StaleAfterNs
	}
	c, _ := json.Marshal(condition)
	g, _ := json.Marshal(groups)
	a, _ := json.Marshal(in.Annotations)
	sev := in.Severity
	if sev == "" {
		sev = "warning"
	}
	on := true
	if in.Enabled != nil {
		on = *in.Enabled
	}
	browser, pushover := true, true
	if in.BrowserEnabled != nil {
		browser = *in.BrowserEnabled
	}
	if in.PushoverEnabled != nil {
		pushover = *in.PushoverEnabled
	}
	return &storage.AlertRule{Name: in.Name, QuerySQL: in.QuerySQL, QueryVersion: dashboardQueryVersion, ConditionJSON: string(c), GroupByJSON: string(g), PendingForNs: in.PendingForNs, CooldownNs: in.CooldownNs, RepeatIntervalNs: in.RepeatIntervalNs, Owner: in.Owner, Team: in.Team, Severity: sev, Enabled: on, BrowserEnabled: browser, PushoverEnabled: pushover, InstanceDiscoverySQL: discoverySQL, InstanceDiscoveryIntervalNs: discoveryEvery, InstanceDiscoveryStaleAfterNs: discoveryStale, AnnotationsJSON: string(a)}, nil
}

type alertListSummary struct {
	RuleCounts     map[string]int `json:"rule_counts"`
	InstanceCounts map[string]int `json:"instance_counts"`
}

type alertListResponse struct {
	Items   []*storage.AlertRule `json:"items"`
	Summary alertListSummary     `json:"summary"`
}

func alertListState(rule *storage.AlertRule) string {
	for _, instance := range rule.Instances {
		if instance.State == "firing" {
			return "firing"
		}
	}
	for _, instance := range rule.Instances {
		if instance.State == "pending" {
			return "pending"
		}
	}
	if len(rule.Instances) > 0 {
		return rule.Instances[0].State
	}
	return "resolved"
}

func alertListRank(state string) int {
	switch state {
	case "firing":
		return 0
	case "pending":
		return 1
	case "error":
		return 2
	default:
		return 3
	}
}

func alertListActivity(rule *storage.AlertRule, state string) int64 {
	var latest int64
	for _, instance := range rule.Instances {
		if state == "firing" && instance.FiredAt != nil {
			latest = max(latest, *instance.FiredAt)
		}
		if state == "pending" && instance.FirstPendingAt != nil {
			latest = max(latest, *instance.FirstPendingAt)
		}
	}
	if latest != 0 {
		return latest
	}
	if rule.LastEvaluatedAt != 0 {
		return rule.LastEvaluatedAt
	}
	return rule.UpdatedAt
}

func (r *Router) listAlerts(w http.ResponseWriter, q *http.Request) {
	x, e := r.store.WithContext(q.Context()).ListAlertRules()
	if e != nil {
		respondErr(w, q, 500, e.Error())
		return
	}
	state := q.URL.Query().Get("state")
	search := strings.ToLower(strings.TrimSpace(q.URL.Query().Get("search")))
	summary := alertListSummary{RuleCounts: map[string]int{}, InstanceCounts: map[string]int{}}
	filtered := make([]*storage.AlertRule, 0, len(x))
	for _, rule := range x {
		ruleState := alertListState(rule)
		summary.RuleCounts[ruleState]++
		for _, instance := range rule.Instances {
			summary.InstanceCounts[instance.State]++
		}
		matchesState := state == "" || state == "all" ||
			(state == "attention" && (ruleState == "firing" || ruleState == "pending")) ||
			ruleState == state
		matchesSearch := search == "" || strings.Contains(strings.ToLower(rule.Name+" "+rule.QuerySQL), search)
		if matchesState && matchesSearch {
			filtered = append(filtered, rule)
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		left, right := alertListState(filtered[i]), alertListState(filtered[j])
		if alertListRank(left) != alertListRank(right) {
			return alertListRank(left) < alertListRank(right)
		}
		leftActivity, rightActivity := alertListActivity(filtered[i], left), alertListActivity(filtered[j], right)
		if leftActivity != rightActivity {
			return leftActivity > rightActivity
		}
		return filtered[i].ID < filtered[j].ID
	})
	page, limit := positiveQueryInt(q, "page", 1), positiveQueryInt(q, "limit", 15)
	if limit > 100 {
		limit = 100
	}
	start := (page - 1) * limit
	if start > len(filtered) {
		start = len(filtered)
	}
	end := start + limit
	if end > len(filtered) {
		end = len(filtered)
	}
	respond(w, alertListResponse{Items: filtered[start:end], Summary: summary}, len(filtered), page)
}
func (r *Router) getAlert(w http.ResponseWriter, q *http.Request) {
	x, e := r.store.WithContext(q.Context()).GetAlertRule(chi.URLParam(q, "id"))
	if e != nil {
		respondErr(w, q, 404, "alert not found")
		return
	}
	respond(w, x, 1, 1)
}
func (r *Router) createAlert(w http.ResponseWriter, q *http.Request) {
	var in alertInput
	if !decodeAndValidate(w, q, &in) {
		return
	}
	x, e := alertModel(in)
	if e != nil {
		respondErr(w, q, 400, e.Error())
		return
	}
	if e = r.store.WithContext(q.Context()).CreateAlertRule(x); e != nil {
		respondErr(w, q, 500, e.Error())
		return
	}
	respond(w, x, 1, 1)
}
func (r *Router) patchAlert(w http.ResponseWriter, q *http.Request) {
	var in alertInput
	if !decodeAndValidate(w, q, &in) {
		return
	}
	x, e := alertModel(in)
	if e != nil {
		respondErr(w, q, 400, e.Error())
		return
	}
	x.ID = chi.URLParam(q, "id")
	if existing, err := r.store.WithContext(q.Context()).GetAlertRule(x.ID); err != nil {
		respondErr(w, q, 404, "alert not found")
		return
	} else if existing.SourceFile != "" {
		respondErr(w, q, http.StatusConflict, "alert is managed by YAML file "+existing.SourceFile+"; edit the file and reload it")
		return
	}
	if e = r.store.WithContext(q.Context()).UpdateAlertRule(x); e != nil {
		respondErr(w, q, 500, e.Error())
		return
	}
	respond(w, x, 1, 1)
}
func (r *Router) duplicateAlert(w http.ResponseWriter, q *http.Request) {
	x, err := r.store.WithContext(q.Context()).DuplicateAlertRule(chi.URLParam(q, "id"))
	if err != nil {
		respondErr(w, q, 404, "alert not found")
		return
	}
	respond(w, x, 1, 1)
}

type alertTestNotificationInput struct {
	Destination string `json:"destination" validate:"required,oneof=browser pushover"`
}

// testAlertNotification is intentional operator action, not a simulated
// lifecycle transition. It uses the production renderer/transports and leaves
// no alert instance behind, while recording a durable delivery test event.
func (r *Router) testAlertNotification(w http.ResponseWriter, q *http.Request) {
	var in alertTestNotificationInput
	if !decodeAndValidate(w, q, &in) {
		return
	}
	rule, err := r.store.WithContext(q.Context()).GetAlertRule(chi.URLParam(q, "id"))
	if err != nil {
		respondErr(w, q, 404, "alert not found")
		return
	}
	value := 0.0
	instance := &storage.AlertInstance{RuleID: rule.ID, GroupKey: "test", LabelsJSON: `{"test":"true"}`, State: "firing", Value: &value}
	now := time.Now()
	if in.Destination == "browser" {
		event := alertEvent(rule, instance, "test", now)
		payload, _ := event.Payload.(map[string]any)
		if r.hub != nil {
			r.hub.Broadcast(event)
		}
		_ = r.store.WithContext(q.Context()).RecordAlertEvent(&storage.AlertEvent{RuleID: rule.ID, GroupKey: "test", Kind: "notification_browser_test", State: "firing", Value: &value, Detail: fmt.Sprint(payload["body"])})
		respond(w, map[string]any{"destination": "browser", "status": "sent", "body": payload["body"]}, 1, 1)
		return
	}
	d := currentAlertDelivery()
	if !rule.PushoverEnabled || d.PushoverEnabled == nil || !d.PushoverEnabled() || d.PushoverUserKey == "" || d.PushoverAPIToken == "" {
		_ = r.store.WithContext(q.Context()).RecordAlertEvent(&storage.AlertEvent{RuleID: rule.ID, GroupKey: "test", Kind: "notification_pushover_test_suppressed", State: "firing", Value: &value, Detail: "Pushover is disabled or not configured"})
		respond(w, map[string]any{"destination": "pushover", "status": "suppressed", "body": "Pushover is disabled or not configured"}, 1, 1)
		return
	}
	if err := deliverPushover(rule, instance, "test"); err != nil {
		respondErr(w, q, 502, "pushover test failed: "+err.Error())
		return
	}
	respond(w, map[string]any{"destination": "pushover", "status": "sent"}, 1, 1)
}
func (r *Router) deleteAlert(w http.ResponseWriter, q *http.Request) {
	if existing, err := r.store.WithContext(q.Context()).GetAlertRule(chi.URLParam(q, "id")); err != nil {
		respondErr(w, q, 404, "alert not found")
		return
	} else if existing.SourceFile != "" {
		respondErr(w, q, http.StatusConflict, "alert is managed by YAML file "+existing.SourceFile+"; remove the file and reload alerts")
		return
	}
	if e := r.store.WithContext(q.Context()).DeleteAlertRule(chi.URLParam(q, "id")); e != nil {
		respondErr(w, q, 500, e.Error())
		return
	}
	respond(w, map[string]bool{"ok": true}, 1, 1)
}

func (r *Router) reloadAlertDefinitions(w http.ResponseWriter, q *http.Request) {
	if r.settings == nil || r.settings.ReloadAlerts == nil {
		respondErr(w, q, http.StatusNotImplemented, "alert directory reload is not configured")
		return
	}
	if err := r.settings.ReloadAlerts(); err != nil {
		respondErr(w, q, http.StatusBadRequest, "alert YAML reload failed: "+err.Error())
		return
	}
	if r.hub != nil {
		r.hub.Broadcast(&ws.Event{Type: "alert", Timestamp: time.Now().UnixNano(), Payload: map[string]any{"transition": "reloaded"}})
	}
	respond(w, map[string]bool{"ok": true}, 1, 1)
}
func (r *Router) acknowledgeAlert(w http.ResponseWriter, q *http.Request) {
	store := r.store.WithContext(q.Context())
	id := chi.URLParam(q, "id")
	changed, e := store.AcknowledgeAlert(id)
	if e != nil {
		respondErr(w, q, 500, e.Error())
		return
	}
	if rule, err := store.GetAlertRule(id); err == nil && r.hub != nil {
		for _, instance := range changed {
			r.hub.Broadcast(alertEvent(rule, instance, "acknowledged", time.Now()))
			_ = store.RecordAlertEvent(&storage.AlertEvent{RuleID: rule.ID, GroupKey: instance.GroupKey, Kind: "acknowledged", State: instance.State, Value: instance.Value})
		}
	}
	respond(w, map[string]bool{"ok": true}, 1, 1)
}

type instanceActionInput struct {
	GroupKey string `json:"group_key" validate:"required,max=4096"`
	Note     string `json:"note" validate:"max=1000"`
}

func (r *Router) acknowledgeAlertInstance(w http.ResponseWriter, q *http.Request) {
	var in instanceActionInput
	if !decodeAndValidate(w, q, &in) {
		return
	}
	store := r.store.WithContext(q.Context())
	id := chi.URLParam(q, "id")
	instance, changed, err := store.AcknowledgeAlertInstance(id, in.GroupKey, in.Note)
	if err != nil {
		respondErr(w, q, 404, "alert instance not found")
		return
	}
	if changed {
		if rule, e := store.GetAlertRule(id); e == nil {
			_ = store.RecordAlertEvent(&storage.AlertEvent{RuleID: id, GroupKey: in.GroupKey, Kind: "acknowledged", State: instance.State, Value: instance.Value, Detail: in.Note})
			if r.hub != nil {
				r.hub.Broadcast(alertEvent(rule, instance, "acknowledged", time.Now()))
			}
		}
	}
	respond(w, instance, 1, 1)
}
func (r *Router) unacknowledgeAlertInstance(w http.ResponseWriter, q *http.Request) {
	var in instanceActionInput
	if !decodeAndValidate(w, q, &in) {
		return
	}
	store := r.store.WithContext(q.Context())
	id := chi.URLParam(q, "id")
	instance, changed, err := store.UnacknowledgeAlertInstance(id, in.GroupKey)
	if err != nil {
		respondErr(w, q, 404, "alert instance not found")
		return
	}
	if changed {
		_ = store.RecordAlertEvent(&storage.AlertEvent{RuleID: id, GroupKey: in.GroupKey, Kind: "unacknowledged", State: instance.State, Value: instance.Value})
	}
	respond(w, instance, 1, 1)
}
func (r *Router) previewAlert(w http.ResponseWriter, q *http.Request) {
	store := r.store.WithContext(q.Context())
	x, e := store.GetAlertRule(chi.URLParam(q, "id"))
	if e != nil {
		respondErr(w, q, 404, "alert not found")
		return
	}
	if preview, ok, err := compositeAlertPreview(store, x); err != nil {
		respondErr(w, q, 400, err.Error())
		return
	} else if ok {
		preview["notification_preview"] = notificationPreview(x)
		respond(w, preview, len(preview["rows"].([]map[string]any)), 1)
		return
	}
	cols, values, _, e := store.ReadOnlyQuery(q.Context(), x.QuerySQL, 1000)
	if e != nil {
		respondErr(w, q, 500, e.Error())
		return
	}
	if e := validateAlertColumnsForCondition(cols, alertGroupColumns(x.GroupByJSON), x.ConditionJSON); e != nil {
		respondErr(w, q, 400, e.Error())
		return
	}
	rows := rowsForColumns(cols, values)
	respond(w, map[string]any{"columns": cols, "rows": rows, "condition": json.RawMessage(x.ConditionJSON), "notification_preview": notificationPreview(x)}, len(rows), 1)
}

// previewAlertDraft evaluates exactly the submitted unsaved definition so an
// operator can test a change without first activating it.
func (r *Router) previewAlertDraft(w http.ResponseWriter, q *http.Request) {
	var in alertInput
	if !decodeAndValidate(w, q, &in) {
		return
	}
	x, err := alertModel(in)
	if err != nil {
		respondErr(w, q, 400, err.Error())
		return
	}
	if preview, ok, err := compositeAlertPreview(r.store.WithContext(q.Context()), x); err != nil {
		respondErr(w, q, 400, err.Error())
		return
	} else if ok {
		preview["notification_preview"] = notificationPreview(x)
		respond(w, preview, len(preview["rows"].([]map[string]any)), 1)
		return
	}
	cols, values, _, err := r.store.WithContext(q.Context()).ReadOnlyQuery(q.Context(), x.QuerySQL, 1000)
	if err != nil {
		respondErr(w, q, 400, err.Error())
		return
	}
	if err := validateAlertColumnsForCondition(cols, alertGroupColumns(x.GroupByJSON), x.ConditionJSON); err != nil {
		respondErr(w, q, 400, err.Error())
		return
	}
	respond(w, map[string]any{"columns": cols, "rows": rowsForColumns(cols, values), "condition": json.RawMessage(x.ConditionJSON), "notification_preview": notificationPreview(x)}, len(values), 1)
}

func notificationPreview(rule *storage.AlertRule) []map[string]string {
	delivery := currentAlertDelivery()
	browser := map[string]string{"destination": "browser", "status": "would_send"}
	if !rule.BrowserEnabled {
		browser["status"], browser["reason"] = "suppressed", "disabled for this rule"
	} else if delivery.BrowserEnabled != nil && !delivery.BrowserEnabled() {
		browser["status"], browser["reason"] = "suppressed", "disabled globally"
	}
	push := map[string]string{"destination": "pushover", "status": "would_send"}
	if !rule.PushoverEnabled {
		push["status"], push["reason"] = "suppressed", "disabled for this rule"
	} else if delivery.PushoverEnabled == nil || !delivery.PushoverEnabled() {
		push["status"], push["reason"] = "suppressed", "disabled globally"
	} else if delivery.PushoverUserKey == "" || delivery.PushoverAPIToken == "" {
		push["status"], push["reason"] = "suppressed", "Pushover credentials are not configured"
	}
	return []map[string]string{browser, push}
}

func compositeAlertPreview(store *storage.DB, rule *storage.AlertRule) (map[string]any, bool, error) {
	var condition alertCondition
	if err := json.Unmarshal([]byte(rule.ConditionJSON), &condition); err != nil {
		return nil, false, err
	}
	if condition.Kind != "any_of" && condition.Kind != "all_of" {
		return nil, false, nil
	}
	rules, err := store.ListAlertRules()
	if err != nil {
		return nil, true, err
	}
	byID := make(map[string]*storage.AlertRule, len(rules))
	for _, source := range rules {
		byID[source.ID] = source
	}
	rows := make([]map[string]any, 0, len(condition.RuleIDs))
	for _, id := range condition.RuleIDs {
		source := byID[id]
		if source == nil {
			return nil, true, fmt.Errorf("composite alert source rule %q does not exist", id)
		}
		rows = append(rows, map[string]any{"rule_id": id, "name": source.Name, "state": ruleState(source), "active": source.Enabled && hasFiringInstance(source)})
	}
	return map[string]any{"columns": []string{"rule_id", "name", "state", "active"}, "rows": rows, "condition": json.RawMessage(rule.ConditionJSON)}, true, nil
}

func ruleState(rule *storage.AlertRule) string {
	for _, instance := range rule.Instances {
		if instance.State == "firing" {
			return "firing"
		}
	}
	for _, instance := range rule.Instances {
		if instance.State == "pending" {
			return "pending"
		}
	}
	return "resolved"
}
func (r *Router) listAlertEvents(w http.ResponseWriter, q *http.Request) {
	page, limit := positiveQueryInt(q, "page", 1), positiveQueryInt(q, "limit", 15)
	x, total, err := r.store.WithContext(q.Context()).ListAlertEventsPage(storage.AlertEventFilter{
		RuleID:   chi.URLParam(q, "id"),
		GroupKey: q.URL.Query().Get("group_key"),
	}, page, limit)
	if err != nil {
		respondErr(w, q, 500, err.Error())
		return
	}
	respond(w, x, total, page)
}
func (r *Router) listAlertHistory(w http.ResponseWriter, q *http.Request) {
	rules, _ := r.store.WithContext(q.Context()).ListAlertRules()
	names := map[string]string{}
	severityIDs := make([]string, 0, len(rules))
	searchRuleIDs := make([]string, 0, len(rules))
	search := strings.TrimSpace(q.URL.Query().Get("search"))
	severity := strings.TrimSpace(q.URL.Query().Get("severity"))
	for _, rule := range rules {
		names[rule.ID] = rule.Name
		if severity == "" || rule.Severity == severity {
			severityIDs = append(severityIDs, rule.ID)
		}
		if search != "" && strings.Contains(strings.ToLower(rule.Name), strings.ToLower(search)) {
			searchRuleIDs = append(searchRuleIDs, rule.ID)
		}
	}
	page, limit := positiveQueryInt(q, "page", 1), positiveQueryInt(q, "limit", 50)
	from, _ := strconv.ParseInt(q.URL.Query().Get("from"), 10, 64)
	to, _ := strconv.ParseInt(q.URL.Query().Get("to"), 10, 64)
	filter := storage.AlertEventFilter{
		RuleID:   q.URL.Query().Get("rule_id"),
		GroupKey: q.URL.Query().Get("group_key"),
		Kind:     q.URL.Query().Get("kind"),
		State:    q.URL.Query().Get("state"),
		From:     from,
		To:       to,
	}
	// Severity is a rule property. Rule names participate in the event text
	// search, so an operator can find either a named rule or its delivery and
	// evaluation details with one query.
	if severity != "" {
		filter.RuleIDs = severityIDs
	}
	if search != "" {
		filter.Search = search
		filter.SearchRuleIDs = searchRuleIDs
	}
	x, total, err := r.store.WithContext(q.Context()).ListAlertEventsPage(filter, page, limit)
	if err != nil {
		respondErr(w, q, 500, err.Error())
		return
	}
	out := make([]map[string]any, 0, len(x))
	for _, event := range x {
		out = append(out, map[string]any{"id": event.ID, "rule_id": event.RuleID, "rule_name": names[event.RuleID], "group_key": event.GroupKey, "kind": event.Kind, "state": event.State, "value": event.Value, "detail": event.Detail, "created_at": event.CreatedAt})
	}
	respond(w, out, total, page)
}

func positiveQueryInt(q *http.Request, name string, fallback int) int {
	value, err := strconv.Atoi(q.URL.Query().Get(name))
	if err != nil || value < 1 {
		return fallback
	}
	return value
}

type silenceInput struct {
	EndsAt   int64  `json:"ends_at"`
	Comment  string `json:"comment"`
	GroupKey string `json:"group_key" validate:"max=4096"`
	StartsAt int64  `json:"starts_at"`
}

func (r *Router) createAlertSilence(w http.ResponseWriter, q *http.Request) {
	var in silenceInput
	if !decodeAndValidate(w, q, &in) {
		return
	}
	now := time.Now().UnixNano()
	start := in.StartsAt
	if start == 0 {
		start = now
	}
	if in.EndsAt <= start {
		respondErr(w, q, 400, "silence end must be after its start")
		return
	}
	x := &storage.AlertSilence{RuleID: chi.URLParam(q, "id"), GroupKey: in.GroupKey, Comment: in.Comment, StartsAt: start, EndsAt: in.EndsAt}
	if err := r.store.WithContext(q.Context()).CreateAlertSilence(x); err != nil {
		respondErr(w, q, 500, err.Error())
		return
	}
	r.broadcastAlertSync(x.RuleID, "silence_created")
	respond(w, x, 1, 1)
}
func (r *Router) listAlertSilences(w http.ResponseWriter, q *http.Request) {
	x, e := r.store.WithContext(q.Context()).ListAlertSilences(chi.URLParam(q, "id"))
	if e != nil {
		respondErr(w, q, 500, e.Error())
		return
	}
	respond(w, x, len(x), 1)
}
func (r *Router) updateAlertSilence(w http.ResponseWriter, q *http.Request) {
	var in silenceInput
	if !decodeAndValidate(w, q, &in) {
		return
	}
	if in.EndsAt <= in.StartsAt {
		respondErr(w, q, 400, "silence end must be after its start")
		return
	}
	x, err := r.store.WithContext(q.Context()).UpdateAlertSilence(
		chi.URLParam(q, "id"),
		chi.URLParam(q, "silenceID"),
		&storage.AlertSilence{GroupKey: in.GroupKey, Comment: in.Comment, StartsAt: in.StartsAt, EndsAt: in.EndsAt},
	)
	if err != nil {
		respondErr(w, q, 404, "silence not found")
		return
	}
	r.broadcastAlertSync(chi.URLParam(q, "id"), "silence_updated")
	respond(w, x, 1, 1)
}
func (r *Router) deleteAlertSilence(w http.ResponseWriter, q *http.Request) {
	if err := r.store.WithContext(q.Context()).DeleteAlertSilence(chi.URLParam(q, "id"), chi.URLParam(q, "silenceID")); err != nil {
		respondErr(w, q, 500, err.Error())
		return
	}
	r.broadcastAlertSync(chi.URLParam(q, "id"), "silence_deleted")
	respond(w, map[string]bool{"ok": true}, 1, 1)
}

func (r *Router) broadcastAlertSync(ruleID, reason string) {
	if r.hub == nil {
		return
	}
	r.hub.Broadcast(&ws.Event{
		Type:      "alert_sync",
		Timestamp: time.Now().UnixNano(),
		Payload:   map[string]string{"ruleId": ruleID, "reason": reason},
	})
}
