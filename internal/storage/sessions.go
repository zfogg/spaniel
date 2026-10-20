package storage

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/zfogg/spaniel/internal/model"
	"github.com/zfogg/spaniel/internal/storage/querygen"
)

type Session = model.Session

func (d *DB) CreateSession(label string, isBaseline bool) (*Session, error) {
	return d.createSession(label, isBaseline, false)
}

func (d *DB) CreateImportedSession(label string) (*Session, error) {
	if label == "" {
		label = fmt.Sprintf("import_%d", time.Now().UnixMilli())
	}
	return d.createSession(label, true, true)
}

func (d *DB) createSession(label string, isBaseline, isImported bool) (*Session, error) {
	now := time.Now().UnixNano()
	id := uuid.New().String()
	if label == "" {
		label = id
	}
	services, _ := json.Marshal([]string{})
	s := &Session{
		ID: id, Label: label, CreatedAt: now,
		IsBaseline: isBaseline, IsImported: isImported,
		SpanCount: 0, Services: string(services),
	}
	if err := d.namedQuery("CreateSession").Session.Create(s); err != nil {
		return nil, err
	}
	return s, nil
}

func (d *DB) SetActiveSession(id, label string) {
	d.activeSessionID = id
	d.activeSessionLabel = label
}

func (d *DB) ActiveSessionID() string { return d.activeSessionID }

func (d *DB) ActiveSessionLabel() string { return d.activeSessionLabel }

func (d *DB) ListSessions() ([]*Session, error) {
	rows, err := d.query.Session.ListWithStats()
	if err != nil {
		return nil, err
	}
	result := make([]*Session, len(rows))
	for i, r := range rows {
		result[i] = &Session{
			ID: r.ID, Label: r.Label, CreatedAt: r.CreatedAt,
			IsBaseline: r.IsBaseline, IsImported: r.IsImported,
			SpanCount: r.SpanCount, Services: r.Services, TraceCount: r.TraceCount,
			Note: r.Note, LastActivityNs: r.LastActivityNs, P95Ns: r.P95Ns,
			SizeBytes: r.SizeBytes, N1Count: r.N1Count, ErrorCount: r.ErrorCount,
		}
	}
	// Report live telemetry payload estimates. Shared indexes, free blocks and
	// deleted tuples have no session owner and must not inflate these values.
	return result, nil
}

func (d *DB) GetSession(id string) (*Session, error) {
	sessions, err := d.query.Session.GetByID(id)
	if err != nil {
		return nil, err
	}
	if len(sessions) == 0 {
		return nil, nil
	}
	return &sessions[0], nil
}

func (d *DB) SetBaseline(id string, isBaseline bool) error {
	q := d.namedQuery("SetBaseline")
	return q.Transaction(func(tx *querygen.Query) error {
		if isBaseline {
			// clear any previous baseline first
			if _, err := tx.Session.Where(tx.Session.IsBaseline.Is(true)).
				Update(tx.Session.IsBaseline, false); err != nil {
				return err
			}
		}
		_, err := tx.Session.Where(tx.Session.ID.Eq(id)).
			Update(tx.Session.IsBaseline, isBaseline)
		return err
	})
}

// SessionPatch holds the mutable user-facing fields that PATCH /api/sessions/{id} may change.
// A nil pointer means "leave unchanged"; an empty string is a valid value.
type SessionPatch struct {
	Label *string
	Note  *string
}

func (d *DB) UpdateSession(id string, p SessionPatch) error {
	updates := map[string]any{}
	if p.Label != nil {
		updates["label"] = *p.Label
	}
	if p.Note != nil {
		updates["note"] = *p.Note
	}
	if len(updates) == 0 {
		return nil
	}
	q := d.namedQuery("UpdateSession")
	_, err := q.Session.Where(q.Session.ID.Eq(id)).Updates(updates)
	return err
}

func (d *DB) DeleteSession(id string) error {
	q := d.namedQuery("DeleteSession")
	return q.Transaction(func(tx *querygen.Query) error {
		if _, err := tx.MetricSeriesCatalog.Where(tx.MetricSeriesCatalog.SessionID.Eq(id)).Delete(); err != nil {
			return err
		}
		if _, err := tx.LintWarning.Where(tx.LintWarning.SessionID.Eq(id)).Delete(); err != nil {
			return err
		}
		if _, err := tx.TraceIssue.Where(tx.TraceIssue.SessionID.Eq(id)).Delete(); err != nil {
			return err
		}
		if _, err := tx.Log.Where(tx.Log.SessionID.Eq(id)).Delete(); err != nil {
			return err
		}
		if _, err := tx.Metric.Where(tx.Metric.SessionID.Eq(id)).Delete(); err != nil {
			return err
		}
		if _, err := tx.SpanEvent.Where(tx.SpanEvent.SessionID.Eq(id)).Delete(); err != nil {
			return err
		}
		if _, err := tx.SpanLink.Where(tx.SpanLink.SessionID.Eq(id)).Delete(); err != nil {
			return err
		}
		if _, err := tx.Span.Where(tx.Span.SessionID.Eq(id)).Delete(); err != nil {
			return err
		}
		_, err := tx.Session.Where(tx.Session.ID.Eq(id)).Delete()
		return err
	})
}
