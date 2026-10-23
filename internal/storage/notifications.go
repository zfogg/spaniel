package storage

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/zfogg/spaniel/internal/model"
	"gorm.io/gorm"
)

type NotificationRecord = model.NotificationRecord

type NotificationFilter struct {
	Source, Severity, Search string
	Unread, Acknowledged     *bool
	FromNs, ToNs             int64
}

func (d *DB) RecordNotification(n *NotificationRecord) error {
	d.notificationMu.Lock()
	defer d.notificationMu.Unlock()
	if n.ID == "" {
		n.ID = uuid.NewString()
	}
	if n.CreatedAt == 0 {
		n.CreatedAt = time.Now().UnixNano()
	}
	if n.DedupeKey != "" {
		existing, err := d.query.NotificationRecord.Where(d.query.NotificationRecord.DedupeKey.Eq(n.DedupeKey)).Order(d.query.NotificationRecord.CreatedAt.Desc()).First()
		if err == nil {
			// Findings have stable identities. Late spans may update their
			// evidence for hours; they must not recreate an acknowledged inbox
			// item every minute. Alert lifecycle deliveries retain their cooldown.
			if n.Source == "issue" || n.Source == "lint" || n.CreatedAt-existing.CreatedAt < int64(time.Minute) {
				return nil
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
	}
	return d.namedQuery("RecordNotification").NotificationRecord.Create(n)
}

func (d *DB) ListNotifications(source string, page, limit int) ([]*NotificationRecord, int64, error) {
	return d.ListNotificationsFiltered(NotificationFilter{Source: source}, page, limit)
}

// ListNotificationsFiltered applies inbox predicates before paging.
func (d *DB) ListNotificationsFiltered(f NotificationFilter, page, limit int) ([]*NotificationRecord, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 30
	}
	q := d.query.NotificationRecord.Where()
	if f.Source != "" {
		q = q.Where(d.query.NotificationRecord.Source.Eq(f.Source))
	}
	if f.Severity != "" {
		q = q.Where(d.query.NotificationRecord.Severity.Eq(f.Severity))
	}
	if f.Unread != nil {
		if *f.Unread {
			q = q.Where(d.query.NotificationRecord.ReadAt.IsNull())
		} else {
			q = q.Where(d.query.NotificationRecord.ReadAt.IsNotNull())
		}
	}
	if f.Acknowledged != nil {
		if *f.Acknowledged {
			q = q.Where(d.query.NotificationRecord.AcknowledgedAt.IsNotNull())
		} else {
			q = q.Where(d.query.NotificationRecord.AcknowledgedAt.IsNull())
		}
	}
	if f.FromNs > 0 {
		q = q.Where(d.query.NotificationRecord.CreatedAt.Gte(f.FromNs))
	}
	if f.ToNs > 0 {
		q = q.Where(d.query.NotificationRecord.CreatedAt.Lte(f.ToNs))
	}
	if f.Search != "" {
		pattern := "%" + f.Search + "%"
		q = q.Where(d.query.NotificationRecord.Title.Like(pattern)).Or(d.query.NotificationRecord.Body.Like(pattern))
	}
	total, err := q.Count()
	if err != nil {
		return nil, 0, err
	}
	items, err := q.Order(d.query.NotificationRecord.CreatedAt.Desc(), d.query.NotificationRecord.ID.Desc()).Offset((page - 1) * limit).Limit(limit).Find()
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
