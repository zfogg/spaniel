package storage

import (
	"crypto/sha256"
	"fmt"

	"github.com/zfogg/spaniel/internal/model"
	"gorm.io/gorm/clause"
)

type Metric = model.Metric

// MetricCatalogEntry summarizes one (name, service) metric stream.
type MetricCatalogEntry = model.MetricCatalogEntry

// AppendMetric buffers a metric data point for batched insertion.
func (d *DB) AppendMetric(m *Metric) error { return d.batcher.AppendMetric(m) }

// MetricSeriesCatalog is the ingestion-facing view of durable admitted series.
type MetricSeriesCatalog struct {
	SessionID  string
	Service    string
	Name       string
	Attributes string
}

func (d *DB) RecordMetricSeries(sessionID, service, name, attrs string, timestampNs int64) (bool, error) {
	seriesKey := sha256.Sum256([]byte(name + "\x00" + service + "\x00" + attrs))
	entry := &model.MetricSeriesCatalog{
		SessionID: sessionID, ServiceName: service, Name: name,
		SeriesKey: fmt.Sprintf("%x", seriesKey[:]), SeriesAttributes: attrs,
		FirstTimestampNs: timestampNs, LastTimestampNs: timestampNs, PointCount: 1,
	}
	if err := d.namedQuery("RecordMetricSeries").MetricSeriesCatalog.Clauses(clause.OnConflict{DoNothing: true}).Create(entry); err != nil {
		return false, err
	}
	return true, nil
}

func (d *DB) MetricSeriesCatalog() ([]MetricSeriesCatalog, error) {
	rows, err := d.query.MetricSeriesCatalog.Find()
	if err != nil {
		return nil, err
	}
	out := make([]MetricSeriesCatalog, len(rows))
	for i, row := range rows {
		out[i] = MetricSeriesCatalog{SessionID: row.SessionID, Service: row.ServiceName, Name: row.Name, Attributes: row.SeriesAttributes}
	}
	return out, nil
}

// InsertMetric stores one metric data point.
func (d *DB) InsertMetric(m *Metric) error {
	return d.namedQuery("InsertMetric").Metric.Create(m)
}

// ActiveMetricSeries returns the durable source for the observable
// active-series gauge without reconstructing identities from raw points.
func (d *DB) ActiveMetricSeries() (int64, error) {
	return d.namedQuery("ActiveMetricSeries").MetricSeriesCatalog.Count()
}

// ListMetricCatalog returns one entry per (service, name) seen in the session.
// Pass "" to ignore the session filter.
func (d *DB) ListMetricCatalog(sessionID string) ([]*MetricCatalogEntry, error) {
	rows, err := d.query.Metric.ListCatalog(sessionID)
	if err != nil {
		return nil, err
	}
	out := make([]*MetricCatalogEntry, len(rows))
	for i := range rows {
		out[i] = &rows[i]
	}
	return out, nil
}

// GetMetricStreamMetadata returns the stable catalog fields for a stream,
// without applying a time window.
func (d *DB) GetMetricStreamMetadata(f MetricSeriesFilter) (*Metric, error) {
	rows, err := d.query.Metric.GetStreamMetadata(f.Name, f.Service, f.SessionID)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// MetricSeriesFilter scopes a series query.
type MetricSeriesFilter struct {
	Name      string
	Service   string
	SessionID string
	FromNs    int64 // inclusive; 0 = no lower bound
	ToNs      int64 // inclusive; 0 = no upper bound
}

// GetMetricSeries returns every data point matching the filter, ordered by
// timestamp ascending. Histogram percentiles arrive as separate rows; callers
// split them apart by attributes.percentile.
func (d *DB) GetMetricSeries(f MetricSeriesFilter) ([]*Metric, error) {
	q := d.query.Metric.Where()
	if f.Name != "" {
		q = q.Where(d.query.Metric.Name.Eq(f.Name))
	}
	if f.Service != "" {
		q = q.Where(d.query.Metric.ServiceName.Eq(f.Service))
	}
	if f.SessionID != "" {
		q = q.Where(d.query.Metric.SessionID.Eq(f.SessionID))
	}
	if f.FromNs > 0 {
		q = q.Where(d.query.Metric.TimestampNs.Gte(f.FromNs))
	}
	if f.ToNs > 0 {
		q = q.Where(d.query.Metric.TimestampNs.Lte(f.ToNs))
	}
	return q.Order(d.query.Metric.TimestampNs).Find()
}
