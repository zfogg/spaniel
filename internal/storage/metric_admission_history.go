package storage

import (
	"github.com/zfogg/spaniel/internal/model"
	"gorm.io/gorm/clause"
)

// MetricAdmissionHistory is bounded by the admission limit for one stream.
// Attribute JSON stays internal; callers must never expose its values.
type MetricAdmissionHistory struct {
	StartedAtNs int64
	Series      []*model.MetricSeriesCatalog
}

func (d *DB) MetricAdmissionObservations() ([]*model.MetricAdmissionObservation, error) {
	return d.namedQuery("MetricAdmissionObservations").MetricAdmissionObservation.Find()
}

func (d *DB) ObserveMetricAdmissions(session, service, name string, now int64, limit int) (*MetricAdmissionHistory, error) {
	q := d.namedQuery("ObserveMetricAdmissions")
	obs := &model.MetricAdmissionObservation{SessionID: session, ServiceName: service, Name: name, StartedAtNs: now}
	if err := q.MetricAdmissionObservation.Clauses(clause.OnConflict{DoNothing: true}).Create(obs); err != nil {
		return nil, err
	}
	stored, err := q.MetricAdmissionObservation.Where(q.MetricAdmissionObservation.SessionID.Eq(session), q.MetricAdmissionObservation.ServiceName.Eq(service), q.MetricAdmissionObservation.Name.Eq(name)).Take()
	if err != nil {
		return nil, err
	}
	rows, err := q.MetricSeriesCatalog.Where(q.MetricSeriesCatalog.SessionID.Eq(session), q.MetricSeriesCatalog.ServiceName.Eq(service), q.MetricSeriesCatalog.Name.Eq(name)).Limit(limit + 1).Find()
	if err != nil {
		return nil, err
	}
	return &MetricAdmissionHistory{StartedAtNs: stored.StartedAtNs, Series: rows}, nil
}
