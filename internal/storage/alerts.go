package storage

import (
	"fmt"
	"github.com/google/uuid"
	"github.com/zfogg/spaniel/internal/model"
	"github.com/zfogg/spaniel/internal/storage/querygen"
	"gorm.io/gen"
	"gorm.io/gen/field"
	"time"
)

type AlertRule = model.AlertRule
type AlertInstance = model.AlertInstance
type AlertInstanceTarget = model.AlertInstanceTarget
type AlertEvent = model.AlertEvent
type AlertSilence = model.AlertSilence

func (d *DB) ListAlertRules() ([]*AlertRule, error) {
	rules := d.namedQuery("storage.ListAlertRules")
	xs, err := rules.AlertRule.Order(rules.AlertRule.UpdatedAt.Desc()).Find()
	if err != nil {
		return nil, err
	}
	if len(xs) == 0 {
		return xs, nil
	}
	ruleIDs := make([]string, len(xs))
	for i, x := range xs {
		ruleIDs[i] = x.ID
	}
	instancesQuery := d.namedQuery("storage.ListAlertRuleInstances")
	instances, err := instancesQuery.AlertInstance.Where(instancesQuery.AlertInstance.RuleID.In(ruleIDs...)).Order(instancesQuery.AlertInstance.LastEvaluatedAt.Desc()).Find()
	if err != nil {
		return nil, err
	}
	byRuleID := make(map[string][]*AlertInstance, len(xs))
	for _, instance := range instances {
		byRuleID[instance.RuleID] = append(byRuleID[instance.RuleID], instance)
	}
	for _, rule := range xs {
		rule.Instances = byRuleID[rule.ID]
	}
	return xs, nil
}
func (d *DB) GetAlertRule(id string) (*AlertRule, error) {
	x, err := d.query.AlertRule.Where(d.query.AlertRule.ID.Eq(id)).First()
	if err != nil {
		return nil, err
	}
	instances, err := d.query.AlertInstance.Where(d.query.AlertInstance.RuleID.Eq(id)).Order(d.query.AlertInstance.LastEvaluatedAt.Desc()).Find()
	if err != nil {
		return nil, err
	}
	x.Instances = instances
	return x, nil
}
func (d *DB) CreateAlertRule(r *AlertRule) error {
	now := time.Now().UnixNano()
	if r.ID == "" {
		r.ID = uuid.NewString()
	}
	r.CreatedAt = now
	r.UpdatedAt = now
	return d.namedQuery("storage.CreateAlertRule").AlertRule.Create(r)
}

// DuplicateAlertRule copies only the definition. Runtime state, file ownership,
// and evaluator health belong to the original and must never leak into a new rule.
func (d *DB) DuplicateAlertRule(id string) (*AlertRule, error) {
	source, err := d.GetAlertRule(id)
	if err != nil {
		return nil, err
	}
	copy := *source
	copy.ID = uuid.NewString()
	copy.Name = "Copy of " + source.Name
	copy.SourceFile, copy.SourceHash = "", ""
	copy.LastEvaluatedAt, copy.LastSuccessAt, copy.LastDurationNs, copy.NextEvaluationAt = 0, 0, 0, 0
	copy.LastError = ""
	copy.Instances = nil
	if err := d.CreateAlertRule(&copy); err != nil {
		return nil, err
	}
	return &copy, nil
}
func (d *DB) ReplaceAlertDefinition(r *AlertRule) error {
	if old, e := d.GetAlertRule(r.ID); e == nil {
		r.CreatedAt = old.CreatedAt
		return d.UpdateAlertRule(r)
	}
	now := time.Now().UnixNano()
	r.CreatedAt = now
	r.UpdatedAt = now
	return d.query.AlertRule.Create(r)
}
func (d *DB) UpdateAlertRule(r *AlertRule) error {
	// Rule edits are definition changes. Preserve evaluator-owned health state
	// so saving a title or query never erases the reliability record.
	if old, err := d.GetAlertRule(r.ID); err == nil {
		r.LastEvaluatedAt = old.LastEvaluatedAt
		r.LastSuccessAt = old.LastSuccessAt
		r.LastDurationNs = old.LastDurationNs
		r.NextEvaluationAt = old.NextEvaluationAt
		r.LastError = old.LastError
		// UI/API updates intentionally leave these blank and must not detach a
		// file-managed rule. Directory reconciliation supplies fresh provenance.
		if r.SourceFile == "" {
			r.SourceFile = old.SourceFile
			r.SourceHash = old.SourceHash
		}
	}
	r.UpdatedAt = time.Now().UnixNano()
	return d.namedQuery("storage.UpdateAlertRule").AlertRule.Save(r)
}

func (d *DB) RecordAlertEvaluation(ruleID string, evaluatedAt, durationNs, nextAt int64, evaluationErr error) error {
	updates := map[string]any{
		"last_evaluated_at":  evaluatedAt,
		"last_duration_ns":   durationNs,
		"next_evaluation_at": nextAt,
	}
	if evaluationErr != nil {
		updates["last_error"] = evaluationErr.Error()
	} else {
		updates["last_success_at"] = evaluatedAt
		updates["last_error"] = ""
	}
	_, err := d.query.AlertRule.Where(d.query.AlertRule.ID.Eq(ruleID)).Updates(updates)
	return err
}

// RecordAlertEvaluations persists an evaluator pass in one GORM Gen batch.
// Each rule was read at the start of the same pass, so Save preserves its
// definition while atomically replacing its evaluator-owned fields.
func (d *DB) RecordAlertEvaluations(rules []*AlertRule) error {
	if len(rules) == 0 {
		return nil
	}
	return d.namedQuery("storage.RecordAlertEvaluations").AlertRule.Save(rules...)
}
func (d *DB) UpsertAlertInstance(x *AlertInstance) error {
	return d.UpsertAlertInstances([]*AlertInstance{x})
}

func (d *DB) UpsertAlertInstances(instances []*AlertInstance) error {
	if len(instances) == 0 {
		return nil
	}
	q := d.namedQuery("storage.UpsertAlertInstances")
	if err := q.AlertInstance.Save(instances...); err != nil {
		return fmt.Errorf("upsert alert instances: %w", err)
	}
	return nil
}

func (d *DB) ListAlertInstanceTargets(ruleID string, activeSince int64) ([]*AlertInstanceTarget, error) {
	q := d.query.AlertInstanceTarget.Where(d.query.AlertInstanceTarget.RuleID.Eq(ruleID))
	if activeSince > 0 {
		q = q.Where(d.query.AlertInstanceTarget.LastSeenAt.Gte(activeSince))
	}
	return q.Order(d.query.AlertInstanceTarget.GroupKey).Find()
}

func (d *DB) UpsertAlertInstanceTarget(target *AlertInstanceTarget) error {
	return d.UpsertAlertInstanceTargets([]*AlertInstanceTarget{target})
}

// UpsertAlertInstanceTargets replaces a discovery batch through GORM Gen.
// DuckDB rejects ON CONFLICT updates to last_seen_at while its composite
// primary-key index is present, so deletion and batched creation are required.
func (d *DB) UpsertAlertInstanceTargets(targets []*AlertInstanceTarget) error {
	if len(targets) == 0 {
		return nil
	}
	q := d.namedQuery("storage.UpsertAlertInstanceTargets")
	if _, err := q.AlertInstanceTarget.Delete(targets...); err != nil {
		return fmt.Errorf("delete alert instance targets for replacement: %w", err)
	}
	if err := q.AlertInstanceTarget.CreateInBatches(targets, len(targets)); err != nil {
		return fmt.Errorf("create alert instance targets: %w", err)
	}
	return nil
}

func (d *DB) RecordAlertInstanceDiscoveryRun(ruleID string, at int64) error {
	_, err := d.query.AlertRule.Where(d.query.AlertRule.ID.Eq(ruleID)).Update(
		d.query.AlertRule.InstanceDiscoveryLastRunAt,
		at,
	)
	return err
}

// AcknowledgeAlert returns only the instances whose acknowledgement metadata
// changed. Callers use that list to emit one genuine acknowledgement event per
// transition; repeating the request is intentionally a no-op.
func (d *DB) AcknowledgeAlert(id string) ([]*AlertInstance, error) {
	now := time.Now().UnixNano()
	// Acknowledgement is operator metadata, not an alert lifecycle state.  A
	// firing condition must remain firing so a later evaluation can resolve it
	// correctly (and so the UI can still communicate the actual condition).
	var changed []*AlertInstance
	err := d.namedQuery("storage.AcknowledgeAlert").Transaction(func(tx *querygen.Query) error {
		q := tx.AlertInstance.Where(
			tx.AlertInstance.RuleID.Eq(id),
			tx.AlertInstance.State.In("pending", "firing"),
			tx.AlertInstance.AcknowledgedAt.IsNull(),
		)
		var err error
		changed, err = q.Find()
		if err != nil {
			return err
		}
		if len(changed) == 0 {
			return nil
		}
		_, err = q.Update(tx.AlertInstance.AcknowledgedAt, now)
		return err
	})
	if err != nil {
		return nil, err
	}
	for _, instance := range changed {
		instance.AcknowledgedAt = &now
	}
	return changed, nil
}

// AcknowledgeAlertInstance records acknowledgement metadata for one label-set.
// It deliberately does not change the lifecycle state: a firing instance must
// remain firing until evaluation resolves it.
func (d *DB) AcknowledgeAlertInstance(ruleID, groupKey, note string) (*AlertInstance, bool, error) {
	instance, err := d.query.AlertInstance.Where(d.query.AlertInstance.RuleID.Eq(ruleID), d.query.AlertInstance.GroupKey.Eq(groupKey)).First()
	if err != nil {
		return nil, false, err
	}
	if instance.State != "pending" && instance.State != "firing" || instance.AcknowledgedAt != nil {
		return instance, false, nil
	}
	now := time.Now().UnixNano()
	if _, err := d.query.AlertInstance.Where(d.query.AlertInstance.RuleID.Eq(ruleID), d.query.AlertInstance.GroupKey.Eq(groupKey)).Updates(map[string]any{"acknowledged_at": now, "acknowledgement_note": note}); err != nil {
		return nil, false, err
	}
	instance.AcknowledgedAt = &now
	instance.AcknowledgementNote = note
	return instance, true, nil
}

func (d *DB) UnacknowledgeAlertInstance(ruleID, groupKey string) (*AlertInstance, bool, error) {
	instance, err := d.query.AlertInstance.Where(d.query.AlertInstance.RuleID.Eq(ruleID), d.query.AlertInstance.GroupKey.Eq(groupKey)).First()
	if err != nil {
		return nil, false, err
	}
	if instance.AcknowledgedAt == nil {
		return instance, false, nil
	}
	if _, err := d.query.AlertInstance.Where(d.query.AlertInstance.RuleID.Eq(ruleID), d.query.AlertInstance.GroupKey.Eq(groupKey)).Updates(map[string]any{"acknowledged_at": nil, "acknowledgement_note": ""}); err != nil {
		return nil, false, err
	}
	instance.AcknowledgedAt = nil
	instance.AcknowledgementNote = ""
	return instance, true, nil
}
func (d *DB) DeleteAlertRule(id string) error {
	return d.namedQuery("storage.DeleteAlertRule").Transaction(func(tx *querygen.Query) error {
		if _, err := tx.AlertInstanceTarget.Where(tx.AlertInstanceTarget.RuleID.Eq(id)).Delete(); err != nil {
			return err
		}
		if _, err := tx.AlertInstance.Where(tx.AlertInstance.RuleID.Eq(id)).Delete(); err != nil {
			return err
		}
		_, err := tx.AlertRule.Where(tx.AlertRule.ID.Eq(id)).Delete()
		return err
	})
}

func (d *DB) RecordAlertEvent(e *AlertEvent) error {
	return d.RecordAlertEvents([]*AlertEvent{e})
}

func (d *DB) RecordAlertEvents(events []*AlertEvent) error {
	if len(events) == 0 {
		return nil
	}
	for _, e := range events {
		e.ID = uuid.NewString()
		if e.CreatedAt == 0 {
			e.CreatedAt = time.Now().UnixNano()
		}
	}
	return d.namedQuery("storage.RecordAlertEvents").AlertEvent.CreateInBatches(events, len(events))
}

type AlertEventFilter struct {
	RuleID  string
	RuleIDs []string
	// SearchRuleIDs are included in the textual search OR clause so a rule
	// name match remains discoverable alongside event detail and label sets.
	SearchRuleIDs []string
	GroupKey      string
	Kind          string
	State         string
	Search        string
	From          int64
	To            int64
}

// ListAlertEventsPage is deliberately backed by the append-only event stream,
// rather than alert_instances.  The latter is only the current state and
// cannot truthfully answer an operator's historical questions.
func (d *DB) ListAlertEventsPage(filter AlertEventFilter, page, limit int) ([]*AlertEvent, int, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if page < 1 {
		page = 1
	}
	q := d.query.AlertEvent
	conditions := make([]gen.Condition, 0, 8)
	if filter.RuleID != "" {
		conditions = append(conditions, q.RuleID.Eq(filter.RuleID))
	} else if filter.RuleIDs != nil {
		if len(filter.RuleIDs) == 0 {
			return []*AlertEvent{}, 0, nil
		}
		conditions = append(conditions, q.RuleID.In(filter.RuleIDs...))
	}
	if filter.GroupKey != "" {
		conditions = append(conditions, q.GroupKey.Eq(filter.GroupKey))
	}
	if filter.Kind != "" {
		conditions = append(conditions, q.Kind.Eq(filter.Kind))
	}
	if filter.State != "" {
		conditions = append(conditions, q.State.Eq(filter.State))
	}
	if filter.Search != "" {
		needle := "%" + filter.Search + "%"
		searchConditions := []field.Expr{q.GroupKey.Like(needle), q.Detail.Like(needle)}
		if len(filter.SearchRuleIDs) > 0 {
			searchConditions = append(searchConditions, q.RuleID.In(filter.SearchRuleIDs...))
		}
		conditions = append(conditions, field.Or(searchConditions...))
	}
	if filter.From > 0 {
		conditions = append(conditions, q.CreatedAt.Gte(filter.From))
	}
	if filter.To > 0 {
		conditions = append(conditions, q.CreatedAt.Lte(filter.To))
	}
	filtered := q.Where(conditions...)
	total, err := filtered.Count()
	if err != nil {
		return nil, 0, err
	}
	items, err := filtered.Order(q.CreatedAt.Desc()).Offset((page - 1) * limit).Limit(limit).Find()
	return items, int(total), err
}

func (d *DB) ListAlertEvents(ruleID, groupKey string, limit int) ([]*AlertEvent, error) {
	items, _, err := d.ListAlertEventsPage(AlertEventFilter{RuleID: ruleID, GroupKey: groupKey}, 1, limit)
	return items, err
}

func (d *DB) CreateAlertSilence(s *AlertSilence) error {
	s.ID = uuid.NewString()
	s.CreatedAt = time.Now().UnixNano()
	return d.query.AlertSilence.Create(s)
}
func (d *DB) ListAlertSilences(ruleID string) ([]*AlertSilence, error) {
	q := d.query.AlertSilence
	if ruleID != "" {
		return q.Where(q.RuleID.Eq(ruleID)).Order(q.EndsAt.Desc()).Find()
	}
	return q.Order(q.EndsAt.Desc()).Find()
}
func (d *DB) DeleteAlertSilence(ruleID, id string) error {
	_, err := d.query.AlertSilence.Where(d.query.AlertSilence.RuleID.Eq(ruleID), d.query.AlertSilence.ID.Eq(id)).Delete()
	return err
}

// UpdateAlertSilence permits an operator to reschedule, extend, or narrow an
// existing silence without revoking it and losing the reason/audit context.
func (d *DB) UpdateAlertSilence(ruleID, id string, update *AlertSilence) (*AlertSilence, error) {
	q := d.query.AlertSilence.Where(
		d.query.AlertSilence.RuleID.Eq(ruleID),
		d.query.AlertSilence.ID.Eq(id),
	)
	current, err := q.First()
	if err != nil {
		return nil, err
	}
	_, err = q.Updates(map[string]any{
		"group_key": update.GroupKey,
		"comment":   update.Comment,
		"starts_at": update.StartsAt,
		"ends_at":   update.EndsAt,
	})
	if err != nil {
		return nil, err
	}
	current.GroupKey = update.GroupKey
	current.Comment = update.Comment
	current.StartsAt = update.StartsAt
	current.EndsAt = update.EndsAt
	return current, nil
}
func (d *DB) IsAlertSilenced(ruleID, groupKey string, now int64) (bool, error) {
	n, err := d.query.AlertSilence.Where(d.query.AlertSilence.RuleID.Eq(ruleID), d.query.AlertSilence.GroupKey.In("", groupKey), d.query.AlertSilence.StartsAt.Lte(now), d.query.AlertSilence.EndsAt.Gt(now)).Count()
	return n > 0, err
}
