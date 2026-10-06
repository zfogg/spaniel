package storage

import (
	"github.com/google/uuid"
	"gorm.io/gorm"
	"time"
)

type AlertRule struct {
	ID              string           `json:"id"`
	Name            string           `json:"name"`
	QuerySQL        string           `json:"query_sql"`
	QueryVersion    int              `json:"query_version"`
	LegacyQueryJSON string           `json:"-" gorm:"column:query_json"`
	ConditionJSON   string           `json:"condition_json"`
	GroupByJSON     string           `json:"group_by_json"`
	PendingForNs    int64            `json:"pending_for_ns"`
	CooldownNs      int64            `json:"cooldown_ns"`
	Severity        string           `json:"severity"`
	AnnotationsJSON string           `json:"annotations_json"`
	Enabled         bool             `json:"enabled"`
	CreatedAt       int64            `json:"created_at"`
	UpdatedAt       int64            `json:"updated_at"`
	Instances       []*AlertInstance `json:"instances,omitempty" gorm:"-"`
}

func (AlertRule) TableName() string { return "alert_rules" }

type AlertInstance struct {
	RuleID          string   `json:"rule_id" gorm:"primaryKey"`
	GroupKey        string   `json:"group_key" gorm:"primaryKey"`
	LabelsJSON      string   `json:"labels_json"`
	State           string   `json:"state"`
	Value           *float64 `json:"value"`
	FirstPendingAt  *int64   `json:"first_pending_at"`
	FiredAt         *int64   `json:"fired_at"`
	ResolvedAt      *int64   `json:"resolved_at"`
	AcknowledgedAt  *int64   `json:"acknowledged_at"`
	LastEvaluatedAt int64    `json:"last_evaluated_at"`
	LastError       string   `json:"last_error"`
	LastNotifiedAt  *int64   `json:"last_notified_at"`
}

func (AlertInstance) TableName() string { return "alert_instances" }
func (d *DB) ListAlertRules() ([]*AlertRule, error) {
	var xs []*AlertRule
	if err := d.gorm.Order("updated_at DESC").Find(&xs).Error; err != nil {
		return nil, err
	}
	for _, x := range xs {
		if err := d.gorm.Where("rule_id = ?", x.ID).Order("last_evaluated_at DESC").Find(&x.Instances).Error; err != nil {
			return nil, err
		}
	}
	return xs, nil
}
func (d *DB) GetAlertRule(id string) (*AlertRule, error) {
	var x AlertRule
	if err := d.gorm.First(&x, "id = ?", id).Error; err != nil {
		return nil, err
	}
	if err := d.gorm.Where("rule_id = ?", id).Order("last_evaluated_at DESC").Find(&x.Instances).Error; err != nil {
		return nil, err
	}
	return &x, nil
}
func (d *DB) CreateAlertRule(r *AlertRule) error {
	now := time.Now().UnixNano()
	r.ID = uuid.NewString()
	r.CreatedAt = now
	r.UpdatedAt = now
	r.LegacyQueryJSON = "{}"
	return d.gorm.Create(r).Error
}
func (d *DB) UpdateAlertRule(r *AlertRule) error {
	r.UpdatedAt = time.Now().UnixNano()
	return d.gorm.Model(&AlertRule{}).Where("id = ?", r.ID).Updates(map[string]any{"name": r.Name, "query_json": "{}", "query_sql": r.QuerySQL, "query_version": r.QueryVersion, "condition_json": r.ConditionJSON, "group_by_json": r.GroupByJSON, "pending_for_ns": r.PendingForNs, "cooldown_ns": r.CooldownNs, "severity": r.Severity, "annotations_json": r.AnnotationsJSON, "enabled": r.Enabled, "updated_at": r.UpdatedAt}).Error
}
func (d *DB) UpsertAlertInstance(x *AlertInstance) error { return d.gorm.Save(x).Error }

// AcknowledgeAlert returns only the instances whose acknowledgement metadata
// changed. Callers use that list to emit one genuine acknowledgement event per
// transition; repeating the request is intentionally a no-op.
func (d *DB) AcknowledgeAlert(id string) ([]*AlertInstance, error) {
	now := time.Now().UnixNano()
	// Acknowledgement is operator metadata, not an alert lifecycle state.  A
	// firing condition must remain firing so a later evaluation can resolve it
	// correctly (and so the UI can still communicate the actual condition).
	var changed []*AlertInstance
	err := d.gorm.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("rule_id = ? AND state IN ('pending','firing') AND acknowledged_at IS NULL", id).Find(&changed).Error; err != nil {
			return err
		}
		if len(changed) == 0 {
			return nil
		}
		return tx.Model(&AlertInstance{}).Where("rule_id = ? AND state IN ('pending','firing') AND acknowledged_at IS NULL", id).Update("acknowledged_at", now).Error
	})
	if err != nil {
		return nil, err
	}
	for _, instance := range changed {
		instance.AcknowledgedAt = &now
	}
	return changed, nil
}
func (d *DB) DeleteAlertRule(id string) error {
	return d.gorm.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("rule_id = ?", id).Delete(&AlertInstance{}).Error; err != nil {
			return err
		}
		return tx.Delete(&AlertRule{}, "id = ?", id).Error
	})
}
