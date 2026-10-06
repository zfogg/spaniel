package storage

import (
	"github.com/google/uuid"
	"github.com/zfogg/spaniel/internal/model"
	"github.com/zfogg/spaniel/internal/storage/querygen"
	"time"
)

type AlertRule = model.AlertRule
type AlertInstance = model.AlertInstance

func (d *DB) ListAlertRules() ([]*AlertRule, error) {
	xs, err := d.query.AlertRule.Order(d.query.AlertRule.UpdatedAt.Desc()).Find()
	if err != nil {
		return nil, err
	}
	for _, x := range xs {
		instances, err := d.query.AlertInstance.Where(d.query.AlertInstance.RuleID.Eq(x.ID)).Order(d.query.AlertInstance.LastEvaluatedAt.Desc()).Find()
		if err != nil {
			return nil, err
		}
		x.Instances = instances
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
	r.ID = uuid.NewString()
	r.CreatedAt = now
	r.UpdatedAt = now
	return d.namedQuery("storage.CreateAlertRule").AlertRule.Create(r)
}
func (d *DB) UpdateAlertRule(r *AlertRule) error {
	r.UpdatedAt = time.Now().UnixNano()
	return d.namedQuery("storage.UpdateAlertRule").AlertRule.Save(r)
}
func (d *DB) UpsertAlertInstance(x *AlertInstance) error {
	return d.namedQuery("storage.UpsertAlertInstance").AlertInstance.Save(x)
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
func (d *DB) DeleteAlertRule(id string) error {
	return d.namedQuery("storage.DeleteAlertRule").Transaction(func(tx *querygen.Query) error {
		if _, err := tx.AlertInstance.Where(tx.AlertInstance.RuleID.Eq(id)).Delete(); err != nil {
			return err
		}
		_, err := tx.AlertRule.Where(tx.AlertRule.ID.Eq(id)).Delete()
		return err
	})
}
