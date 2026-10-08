package storage

import (
	"time"

	"github.com/google/uuid"
	"github.com/zfogg/spaniel/internal/model"
)

type NotificationRecord = model.NotificationRecord

func (d *DB) RecordNotification(n *NotificationRecord) error {
	if n.ID == "" {
		n.ID = uuid.NewString()
	}
	if n.CreatedAt == 0 {
		n.CreatedAt = time.Now().UnixNano()
	}
	if n.DedupeKey != "" {
		existing, err := d.query.NotificationRecord.Where(d.query.NotificationRecord.DedupeKey.Eq(n.DedupeKey)).Order(d.query.NotificationRecord.CreatedAt.Desc()).First()
		if err == nil && n.CreatedAt-existing.CreatedAt < int64(time.Minute) {
			return nil
		}
	}
	return d.namedQuery("storage.RecordNotification").NotificationRecord.Create(n)
}

func (d *DB) ListNotifications(source string, page, limit int) ([]*NotificationRecord, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 30
	}
	q := d.query.NotificationRecord.Where()
	if source != "" {
		q = q.Where(d.query.NotificationRecord.Source.Eq(source))
	}
	total, err := q.Count()
	if err != nil {
		return nil, 0, err
	}
	items, err := q.Order(d.query.NotificationRecord.CreatedAt.Desc()).Offset((page - 1) * limit).Limit(limit).Find()
	return items, total, err
}

func (d *DB) MarkNotificationRead(id string, acknowledged bool) error {
	now := time.Now().UnixNano()
	updates := map[string]any{"read_at": now}
	if acknowledged {
		updates["acknowledged_at"] = now
	}
	_, err := d.query.NotificationRecord.Where(d.query.NotificationRecord.ID.Eq(id)).Updates(updates)
	return err
}
