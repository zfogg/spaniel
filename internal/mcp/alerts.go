package mcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"gopkg.in/yaml.v3"

	"github.com/zfogg/spaniel/internal/alertconfig"
	"github.com/zfogg/spaniel/internal/model"
	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/ws"
)

func (h *handler) registerAlertTools(s *mcpsdk.Server) {
	readOnly := &mcpsdk.ToolAnnotations{ReadOnlyHint: true}
	addTOONTool(s, &mcpsdk.Tool{Name: "list_alerts", Description: "List alert rules with lifecycle state, instances, evaluation health, severity, and optional filters.", Annotations: readOnly}, h.listAlerts)
	addTOONTool(s, &mcpsdk.Tool{Name: "get_alert", Description: "Get one alert rule with current instances, active and scheduled silences, and recent history.", Annotations: readOnly}, h.getAlert)
	addTOONTool(s, &mcpsdk.Tool{Name: "list_alert_history", Description: "Search the append-only alert event history by rule, group, kind, state, severity, text, and time range.", Annotations: readOnly}, h.listAlertHistory)
	addTOONTool(s, &mcpsdk.Tool{Name: "preview_alert", Description: "Evaluate a saved alert query without changing lifecycle state or sending notifications.", Annotations: readOnly}, h.previewAlert)
	addTOONTool(s, &mcpsdk.Tool{Name: "validate_alert", Description: "Validate a structured alert definition and execute its query without saving it.", Annotations: readOnly}, h.validateAlert)
	addTextTool(s, &mcpsdk.Tool{Name: "export_alert_yaml", Description: "Export one alert rule in Spaniel's portable YAML format.", Annotations: readOnly}, h.exportAlertYAML)

	if !h.opts.AllowWrites {
		return
	}
	addTOONTool(s, &mcpsdk.Tool{Name: "acknowledge_alert_instance", Description: "Acknowledge one pending or firing alert instance and record the operator note in alert history.", Annotations: &mcpsdk.ToolAnnotations{DestructiveHint: boolPtr(false), IdempotentHint: true}}, h.acknowledgeAlertInstance)
	addTOONTool(s, &mcpsdk.Tool{Name: "unacknowledge_alert_instance", Description: "Remove acknowledgement metadata from one alert instance and record the action.", Annotations: &mcpsdk.ToolAnnotations{DestructiveHint: boolPtr(false), IdempotentHint: true}}, h.unacknowledgeAlertInstance)
	addTOONTool(s, &mcpsdk.Tool{Name: "silence_alert", Description: "Create a bounded alert silence for a whole rule or one group key.", Annotations: &mcpsdk.ToolAnnotations{DestructiveHint: boolPtr(false)}}, h.silenceAlert)
	addTOONTool(s, &mcpsdk.Tool{Name: "remove_alert_silence", Description: "Remove one alert silence before it expires.", Annotations: &mcpsdk.ToolAnnotations{DestructiveHint: boolPtr(true), IdempotentHint: true}}, h.removeAlertSilence)
	addTOONTool(s, &mcpsdk.Tool{Name: "apply_alert", Description: "Create or replace a complete structured alert definition. Supports dry-run and optimistic updated_at checks.", Annotations: &mcpsdk.ToolAnnotations{DestructiveHint: boolPtr(false), IdempotentHint: true}}, h.applyAlert)
	addTOONTool(s, &mcpsdk.Tool{Name: "import_alert_yaml", Description: "Validate and import a portable alert YAML definition, or dry-run it without saving.", Annotations: &mcpsdk.ToolAnnotations{DestructiveHint: boolPtr(false)}}, h.importAlertYAML)
	addTOONTool(s, &mcpsdk.Tool{Name: "delete_alert", Description: "Delete a local alert rule and its runtime instances. File-managed alerts cannot be deleted.", Annotations: &mcpsdk.ToolAnnotations{DestructiveHint: boolPtr(true)}}, h.deleteAlert)
}

type ListAlertsInput struct {
	State    string `json:"state,omitempty" jsonschema:"all, attention, firing, pending, error, or resolved"`
	Severity string `json:"severity,omitempty" jsonschema:"info, warning, or critical"`
	Enabled  *bool  `json:"enabled,omitempty"`
	Search   string `json:"search,omitempty"`
	Page     int    `json:"page,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

type AlertSummary struct {
	Rule  *model.AlertRule `json:"rule"`
	State string           `json:"state"`
	Link  string           `json:"link"`
}

type ListAlertsOutput struct {
	Items          []AlertSummary `json:"items"`
	Total          int            `json:"total"`
	Page           int            `json:"page"`
	RuleCounts     map[string]int `json:"rule_counts"`
	InstanceCounts map[string]int `json:"instance_counts"`
}

func alertState(rule *model.AlertRule) string {
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
	if rule.LastError != "" {
		return "error"
	}
	if len(rule.Instances) > 0 && rule.Instances[0].State != "" {
		return rule.Instances[0].State
	}
	return "resolved"
}

func alertRank(state string) int {
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

func (h *handler) listAlerts(ctx context.Context, _ *mcpsdk.CallToolRequest, in ListAlertsInput) (*mcpsdk.CallToolResult, ListAlertsOutput, error) {
	rules, err := h.store.WithContext(ctx).ListAlertRules()
	if err != nil {
		return nil, ListAlertsOutput{}, fmt.Errorf("list alerts: %w", err)
	}
	out := ListAlertsOutput{Items: []AlertSummary{}, RuleCounts: map[string]int{}, InstanceCounts: map[string]int{}}
	needle := strings.ToLower(strings.TrimSpace(in.Search))
	for _, rule := range rules {
		state := alertState(rule)
		out.RuleCounts[state]++
		for _, instance := range rule.Instances {
			out.InstanceCounts[instance.State]++
		}
		matchesState := in.State == "" || in.State == "all" || state == in.State || in.State == "attention" && (state == "firing" || state == "pending")
		if !matchesState || in.Severity != "" && rule.Severity != in.Severity || in.Enabled != nil && rule.Enabled != *in.Enabled {
			continue
		}
		if needle != "" && !strings.Contains(strings.ToLower(rule.Name+"\n"+rule.QuerySQL), needle) {
			continue
		}
		out.Items = append(out.Items, AlertSummary{Rule: rule, State: state, Link: "/alerts?id=" + rule.ID})
	}
	sort.SliceStable(out.Items, func(i, j int) bool {
		li, lj := alertRank(out.Items[i].State), alertRank(out.Items[j].State)
		if li != lj {
			return li < lj
		}
		return out.Items[i].Rule.UpdatedAt > out.Items[j].Rule.UpdatedAt
	})
	out.Total = len(out.Items)
	page, limit := in.Page, in.Limit
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 15
	}
	start := min((page-1)*limit, len(out.Items))
	end := min(start+limit, len(out.Items))
	out.Items, out.Page = out.Items[start:end], page
	return nil, out, nil
}

type GetAlertInput struct {
	AlertID    string `json:"alert_id"`
	EventLimit int    `json:"event_limit,omitempty" jsonschema:"recent events to include; default 25, max 200"`
}

type AlertOutput struct {
	Rule     *model.AlertRule      `json:"rule"`
	State    string                `json:"state"`
	Events   []*model.AlertEvent   `json:"events"`
	Silences []*model.AlertSilence `json:"silences"`
	Link     string                `json:"link"`
}

func (h *handler) getAlert(ctx context.Context, _ *mcpsdk.CallToolRequest, in GetAlertInput) (*mcpsdk.CallToolResult, AlertOutput, error) {
	rule, err := h.store.WithContext(ctx).GetAlertRule(in.AlertID)
	if err != nil {
		return nil, AlertOutput{}, fmt.Errorf("alert not found")
	}
	limit := in.EventLimit
	if limit <= 0 || limit > 200 {
		limit = 25
	}
	events, err := h.store.WithContext(ctx).ListAlertEvents(rule.ID, "", limit)
	if err != nil {
		return nil, AlertOutput{}, err
	}
	silences, err := h.store.WithContext(ctx).ListAlertSilences(rule.ID)
	if err != nil {
		return nil, AlertOutput{}, err
	}
	return nil, AlertOutput{Rule: rule, State: alertState(rule), Events: events, Silences: silences, Link: "/alerts?id=" + rule.ID}, nil
}

type ListAlertHistoryInput struct {
	AlertID  string `json:"alert_id,omitempty"`
	GroupKey string `json:"group_key,omitempty"`
	Kind     string `json:"kind,omitempty"`
	State    string `json:"state,omitempty"`
	Severity string `json:"severity,omitempty"`
	Search   string `json:"search,omitempty"`
	FromNs   int64  `json:"from_ns,omitempty"`
	ToNs     int64  `json:"to_ns,omitempty"`
	Page     int    `json:"page,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

type AlertHistoryItem struct {
	Event    *model.AlertEvent `json:"event"`
	RuleName string            `json:"rule_name"`
	Severity string            `json:"severity"`
}

type AlertHistoryOutput struct {
	Items []AlertHistoryItem `json:"items"`
	Total int                `json:"total"`
	Page  int                `json:"page"`
}

func (h *handler) listAlertHistory(ctx context.Context, _ *mcpsdk.CallToolRequest, in ListAlertHistoryInput) (*mcpsdk.CallToolResult, AlertHistoryOutput, error) {
	rules, err := h.store.WithContext(ctx).ListAlertRules()
	if err != nil {
		return nil, AlertHistoryOutput{}, err
	}
	names, severities := map[string]string{}, map[string]string{}
	var ruleIDs []string
	for _, rule := range rules {
		names[rule.ID], severities[rule.ID] = rule.Name, rule.Severity
		if in.Severity == "" || rule.Severity == in.Severity {
			ruleIDs = append(ruleIDs, rule.ID)
		}
	}
	filter := storage.AlertEventFilter{RuleID: in.AlertID, GroupKey: in.GroupKey, Kind: in.Kind, State: in.State, Search: in.Search, From: in.FromNs, To: in.ToNs}
	if in.AlertID == "" && in.Severity != "" {
		filter.RuleIDs = ruleIDs
	}
	page, limit := in.Page, in.Limit
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 500 {
		limit = 50
	}
	events, total, err := h.store.WithContext(ctx).ListAlertEventsPage(filter, page, limit)
	if err != nil {
		return nil, AlertHistoryOutput{}, err
	}
	out := AlertHistoryOutput{Items: []AlertHistoryItem{}, Total: total, Page: page}
	for _, event := range events {
		out.Items = append(out.Items, AlertHistoryItem{Event: event, RuleName: names[event.RuleID], Severity: severities[event.RuleID]})
	}
	return nil, out, nil
}

type AlertPreviewOutput struct {
	AlertID   string          `json:"alert_id,omitempty"`
	Columns   []string        `json:"columns"`
	Rows      [][]any         `json:"rows"`
	RowCount  int             `json:"row_count"`
	Truncated bool            `json:"truncated"`
	Condition json.RawMessage `json:"condition"`
}

func (h *handler) previewAlert(ctx context.Context, _ *mcpsdk.CallToolRequest, in GetAlertInput) (*mcpsdk.CallToolResult, AlertPreviewOutput, error) {
	rule, err := h.store.WithContext(ctx).GetAlertRule(in.AlertID)
	if err != nil {
		return nil, AlertPreviewOutput{}, fmt.Errorf("alert not found")
	}
	return h.previewAlertRule(ctx, rule)
}

func (h *handler) previewAlertRule(ctx context.Context, rule *model.AlertRule) (*mcpsdk.CallToolResult, AlertPreviewOutput, error) {
	var condition struct {
		Kind string `json:"kind"`
	}
	_ = json.Unmarshal([]byte(rule.ConditionJSON), &condition)
	if condition.Kind == "any_of" || condition.Kind == "all_of" {
		return nil, AlertPreviewOutput{}, fmt.Errorf("composite alert previews are represented by their source rule states; inspect get_alert for each rule_id")
	}
	args := []any{}
	if strings.Contains(rule.QuerySQL, "$session_id") {
		args = append(args, sql.Named("session_id", h.store.ActiveSessionID()))
	}
	columns, rows, truncated, err := h.store.WithContext(ctx).ReadOnlyQueryArgs(ctx, rule.QuerySQL, args, 1000)
	if err != nil {
		return nil, AlertPreviewOutput{}, fmt.Errorf("preview alert: %w", err)
	}
	return nil, AlertPreviewOutput{AlertID: rule.ID, Columns: columns, Rows: rows, RowCount: len(rows), Truncated: truncated, Condition: json.RawMessage(rule.ConditionJSON)}, nil
}

type AlertConditionDefinition struct {
	Kind      string   `json:"kind,omitempty"`
	Operator  string   `json:"operator,omitempty"`
	Threshold *float64 `json:"threshold,omitempty"`
	Pattern   string   `json:"pattern,omitempty"`
	RuleIDs   []string `json:"rule_ids,omitempty"`
}

type AlertInstanceDiscoveryDefinition struct {
	Query      string `json:"query"`
	Every      string `json:"every,omitempty"`
	StaleAfter string `json:"stale_after,omitempty"`
}

type AlertDefinition struct {
	Name              string                            `json:"name"`
	Query             string                            `json:"query"`
	Condition         AlertConditionDefinition          `json:"condition"`
	GroupBy           []string                          `json:"group_by,omitempty"`
	PendingFor        string                            `json:"pending_for"`
	Cooldown          string                            `json:"cooldown"`
	RepeatInterval    string                            `json:"repeat_interval,omitempty"`
	Severity          string                            `json:"severity"`
	Enabled           *bool                             `json:"enabled,omitempty"`
	BrowserEnabled    *bool                             `json:"browser_enabled,omitempty"`
	PushoverEnabled   *bool                             `json:"pushover_enabled,omitempty"`
	InstanceDiscovery *AlertInstanceDiscoveryDefinition `json:"instance_discovery,omitempty"`
	Annotations       map[string]string                 `json:"annotations,omitempty"`
}

type ValidateAlertInput struct {
	Definition AlertDefinition `json:"definition"`
}

type AlertValidationOutput struct {
	Valid   bool     `json:"valid"`
	Columns []string `json:"columns"`
}

func (h *handler) validateAlert(ctx context.Context, _ *mcpsdk.CallToolRequest, in ValidateAlertInput) (*mcpsdk.CallToolResult, AlertValidationOutput, error) {
	_, rule, err := alertFromInput(in.Definition, "validation")
	if err != nil {
		return nil, AlertValidationOutput{}, err
	}
	out, err := h.validateAlertRule(ctx, rule)
	return nil, out, err
}

func (h *handler) validateAlertRule(ctx context.Context, rule *model.AlertRule) (AlertValidationOutput, error) {
	if err := storage.ValidateReadOnlySQL(rule.QuerySQL); err != nil {
		return AlertValidationOutput{}, err
	}
	if rule.InstanceDiscoverySQL != "" {
		if err := storage.ValidateReadOnlySQL(rule.InstanceDiscoverySQL); err != nil {
			return AlertValidationOutput{}, err
		}
		if _, _, _, err := h.store.WithContext(ctx).ReadOnlyQuery(ctx, rule.InstanceDiscoverySQL, 1); err != nil {
			return AlertValidationOutput{}, fmt.Errorf("instance discovery query: %w", err)
		}
	}
	var condition struct {
		Kind string `json:"kind"`
	}
	_ = json.Unmarshal([]byte(rule.ConditionJSON), &condition)
	if condition.Kind == "any_of" || condition.Kind == "all_of" {
		return AlertValidationOutput{Valid: true, Columns: []string{}}, nil
	}
	_, preview, err := h.previewAlertRule(ctx, rule)
	if err != nil {
		return AlertValidationOutput{}, err
	}
	return AlertValidationOutput{Valid: true, Columns: preview.Columns}, nil
}

type ApplyAlertInput struct {
	AlertID           string          `json:"alert_id,omitempty"`
	Definition        AlertDefinition `json:"definition"`
	ExpectedUpdatedAt string          `json:"expected_updated_at,omitempty" jsonschema:"exact decimal updated_at returned by get_alert; rejects a stale replacement"`
	DryRun            bool            `json:"dry_run,omitempty"`
}

type ApplyAlertOutput struct {
	Applied bool             `json:"applied"`
	Rule    *model.AlertRule `json:"rule"`
	Link    string           `json:"link"`
}

func (h *handler) applyAlert(ctx context.Context, _ *mcpsdk.CallToolRequest, in ApplyAlertInput) (*mcpsdk.CallToolResult, ApplyAlertOutput, error) {
	if _, _, err := h.validateAlert(ctx, nil, ValidateAlertInput{Definition: in.Definition}); err != nil {
		return nil, ApplyAlertOutput{}, err
	}
	id := in.AlertID
	if id == "" {
		id = uuid.NewString()
	} else {
		existing, err := h.store.WithContext(ctx).GetAlertRule(id)
		if err != nil {
			return nil, ApplyAlertOutput{}, fmt.Errorf("alert not found")
		}
		if existing.SourceFile != "" {
			return nil, ApplyAlertOutput{}, fmt.Errorf("alert is managed by YAML file %s", existing.SourceFile)
		}
		expected, err := parseExpectedUpdatedAt(in.ExpectedUpdatedAt)
		if err != nil {
			return nil, ApplyAlertOutput{}, err
		}
		if expected != 0 && existing.UpdatedAt != expected {
			return nil, ApplyAlertOutput{}, fmt.Errorf("alert changed since it was read: expected updated_at %d, got %d", expected, existing.UpdatedAt)
		}
	}
	_, rule, err := alertFromInput(in.Definition, id)
	if err != nil {
		return nil, ApplyAlertOutput{}, err
	}
	if in.DryRun {
		return nil, ApplyAlertOutput{Applied: false, Rule: rule, Link: "/alerts?id=" + id}, nil
	}
	if err := h.store.WithContext(ctx).ReplaceAlertDefinition(rule); err != nil {
		return nil, ApplyAlertOutput{}, fmt.Errorf("apply alert: %w", err)
	}
	saved, err := h.store.WithContext(ctx).GetAlertRule(id)
	if err != nil {
		return nil, ApplyAlertOutput{}, err
	}
	h.broadcastAlertSync(id, "definition_applied")
	return nil, ApplyAlertOutput{Applied: true, Rule: saved, Link: "/alerts?id=" + id}, nil
}

type ImportAlertYAMLInput struct {
	YAML   string `json:"yaml"`
	DryRun bool   `json:"dry_run,omitempty"`
}

func (h *handler) importAlertYAML(ctx context.Context, _ *mcpsdk.CallToolRequest, in ImportAlertYAMLInput) (*mcpsdk.CallToolResult, ApplyAlertOutput, error) {
	definition, err := alertconfig.Parse([]byte(in.YAML))
	if err != nil {
		return nil, ApplyAlertOutput{}, fmt.Errorf("invalid alert YAML: %w", err)
	}
	id := definition.ID
	if id == "" {
		id = uuid.NewString()
	}
	rule, err := definition.Alert(id)
	if err != nil {
		return nil, ApplyAlertOutput{}, err
	}
	if _, err := h.validateAlertRule(ctx, rule); err != nil {
		return nil, ApplyAlertOutput{}, err
	}
	if existing, err := h.store.WithContext(ctx).GetAlertRule(id); err == nil && existing.SourceFile != "" {
		return nil, ApplyAlertOutput{}, fmt.Errorf("alert is managed by YAML file %s", existing.SourceFile)
	}
	if in.DryRun {
		return nil, ApplyAlertOutput{Applied: false, Rule: rule, Link: "/alerts?id=" + id}, nil
	}
	if err := h.store.WithContext(ctx).ReplaceAlertDefinition(rule); err != nil {
		return nil, ApplyAlertOutput{}, err
	}
	saved, err := h.store.WithContext(ctx).GetAlertRule(id)
	if err != nil {
		return nil, ApplyAlertOutput{}, err
	}
	h.broadcastAlertSync(id, "yaml_imported")
	return nil, ApplyAlertOutput{Applied: true, Rule: saved, Link: "/alerts?id=" + id}, nil
}

func (h *handler) exportAlertYAML(ctx context.Context, _ *mcpsdk.CallToolRequest, in GetAlertInput) (string, error) {
	rule, err := h.store.WithContext(ctx).GetAlertRule(in.AlertID)
	if err != nil {
		return "", fmt.Errorf("alert not found")
	}
	data, err := alertconfig.Marshal(rule)
	return string(data), err
}

type AlertInstanceActionInput struct {
	AlertID  string `json:"alert_id"`
	GroupKey string `json:"group_key"`
	Note     string `json:"note,omitempty"`
}

func (h *handler) acknowledgeAlertInstance(ctx context.Context, _ *mcpsdk.CallToolRequest, in AlertInstanceActionInput) (*mcpsdk.CallToolResult, *model.AlertInstance, error) {
	instance, changed, err := h.store.WithContext(ctx).AcknowledgeAlertInstance(in.AlertID, in.GroupKey, in.Note)
	if err != nil {
		return nil, nil, fmt.Errorf("alert instance not found")
	}
	if changed {
		_ = h.store.WithContext(ctx).RecordAlertEvent(&storage.AlertEvent{RuleID: in.AlertID, GroupKey: in.GroupKey, Kind: "acknowledged", State: instance.State, Value: instance.Value, Detail: in.Note})
		h.broadcastAlertSync(in.AlertID, "acknowledged")
	}
	return nil, instance, nil
}

func (h *handler) unacknowledgeAlertInstance(ctx context.Context, _ *mcpsdk.CallToolRequest, in AlertInstanceActionInput) (*mcpsdk.CallToolResult, *model.AlertInstance, error) {
	instance, changed, err := h.store.WithContext(ctx).UnacknowledgeAlertInstance(in.AlertID, in.GroupKey)
	if err != nil {
		return nil, nil, fmt.Errorf("alert instance not found")
	}
	if changed {
		_ = h.store.WithContext(ctx).RecordAlertEvent(&storage.AlertEvent{RuleID: in.AlertID, GroupKey: in.GroupKey, Kind: "unacknowledged", State: instance.State, Value: instance.Value})
		h.broadcastAlertSync(in.AlertID, "unacknowledged")
	}
	return nil, instance, nil
}

type SilenceAlertInput struct {
	AlertID  string `json:"alert_id"`
	GroupKey string `json:"group_key,omitempty"`
	Comment  string `json:"comment"`
	StartsAt int64  `json:"starts_at,omitempty" jsonschema:"unix nanoseconds; defaults to now"`
	EndsAt   int64  `json:"ends_at" jsonschema:"unix nanoseconds; must be after starts_at"`
}

func (h *handler) silenceAlert(ctx context.Context, _ *mcpsdk.CallToolRequest, in SilenceAlertInput) (*mcpsdk.CallToolResult, *model.AlertSilence, error) {
	if _, err := h.store.WithContext(ctx).GetAlertRule(in.AlertID); err != nil {
		return nil, nil, fmt.Errorf("alert not found")
	}
	start := in.StartsAt
	if start == 0 {
		start = time.Now().UnixNano()
	}
	if in.EndsAt <= start {
		return nil, nil, fmt.Errorf("silence end must be after its start")
	}
	silence := &model.AlertSilence{RuleID: in.AlertID, GroupKey: in.GroupKey, Comment: in.Comment, StartsAt: start, EndsAt: in.EndsAt}
	if err := h.store.WithContext(ctx).CreateAlertSilence(silence); err != nil {
		return nil, nil, err
	}
	h.broadcastAlertSync(in.AlertID, "silence_created")
	return nil, silence, nil
}

type RemoveAlertSilenceInput struct {
	AlertID   string `json:"alert_id"`
	SilenceID string `json:"silence_id"`
}

func (h *handler) removeAlertSilence(ctx context.Context, _ *mcpsdk.CallToolRequest, in RemoveAlertSilenceInput) (*mcpsdk.CallToolResult, MutationOutput, error) {
	if err := h.store.WithContext(ctx).DeleteAlertSilence(in.AlertID, in.SilenceID); err != nil {
		return nil, MutationOutput{}, err
	}
	h.broadcastAlertSync(in.AlertID, "silence_deleted")
	return nil, MutationOutput{OK: true}, nil
}

type DeleteAlertInput struct {
	AlertID string `json:"alert_id"`
}

func (h *handler) deleteAlert(ctx context.Context, _ *mcpsdk.CallToolRequest, in DeleteAlertInput) (*mcpsdk.CallToolResult, MutationOutput, error) {
	rule, err := h.store.WithContext(ctx).GetAlertRule(in.AlertID)
	if err != nil {
		return nil, MutationOutput{}, fmt.Errorf("alert not found")
	}
	if rule.SourceFile != "" {
		return nil, MutationOutput{}, fmt.Errorf("alert is managed by YAML file %s", rule.SourceFile)
	}
	if err := h.store.WithContext(ctx).DeleteAlertRule(in.AlertID); err != nil {
		return nil, MutationOutput{}, err
	}
	h.broadcastAlertSync(in.AlertID, "deleted")
	return nil, MutationOutput{OK: true}, nil
}

func alertFromInput(in AlertDefinition, id string) (alertconfig.Definition, *model.AlertRule, error) {
	definition := alertconfig.Definition{Version: alertconfig.Version, ID: id, Name: in.Name, Query: in.Query, Condition: alertconfig.Condition{Kind: in.Condition.Kind, Operator: in.Condition.Operator, Threshold: in.Condition.Threshold, Pattern: in.Condition.Pattern, RuleIDs: in.Condition.RuleIDs}, GroupBy: in.GroupBy, PendingFor: in.PendingFor, Cooldown: in.Cooldown, RepeatInterval: in.RepeatInterval, Severity: in.Severity, Enabled: in.Enabled, BrowserEnabled: in.BrowserEnabled, PushoverEnabled: in.PushoverEnabled, Annotations: in.Annotations}
	if in.InstanceDiscovery != nil {
		definition.InstanceDiscovery = &alertconfig.InstanceDiscovery{Query: in.InstanceDiscovery.Query, Every: in.InstanceDiscovery.Every, StaleAfter: in.InstanceDiscovery.StaleAfter}
	}
	data, err := yaml.Marshal(definition)
	if err != nil {
		return definition, nil, err
	}
	definition, err = alertconfig.Parse(data)
	if err != nil {
		return definition, nil, fmt.Errorf("invalid alert: %w", err)
	}
	rule, err := definition.Alert(id)
	return definition, rule, err
}

func (h *handler) broadcastAlertSync(alertID, reason string) {
	if h.opts.Hub != nil {
		h.opts.Hub.Broadcast(&ws.Event{Type: "alert_sync", Timestamp: time.Now().UnixNano(), Payload: map[string]string{"ruleId": alertID, "reason": reason}})
	}
}
