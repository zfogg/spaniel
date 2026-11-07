package storage

import (
	"errors"
	"math"
	"time"

	"github.com/zfogg/spaniel/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	dashboardSampleInterval = time.Minute
	dashboardSampleMaxAge   = 24 * time.Hour
	dashboardSampleMaxRows  = 20000
)

type DashboardSamplePoint struct {
	TimestampMs int64   `json:"timestamp_ms"`
	Value       float64 `json:"value"`
}

// DashboardSampleHistory records at most one point per minute per series and
// returns at most one day's samples. Storage pressure never blocks panel reads.
func (d *DB) DashboardSampleHistory(seriesKey, panelID, sessionID string, value float64, now time.Time) ([]DashboardSamplePoint, error) {
	d.dashboardSampleMu.Lock()
	defer d.dashboardSampleMu.Unlock()
	q := d.namedQuery("DashboardSampleHistory").DashboardPanelSample
	cutoff := now.Add(-dashboardSampleMaxAge).UnixNano()
	bucket := now.Truncate(dashboardSampleInterval).UnixNano()
	if !d.Full() && !math.IsNaN(value) && !math.IsInf(value, 0) {
		_, err := q.Where(q.SeriesKey.Eq(seriesKey), q.BucketNs.Eq(bucket)).First()
		if errors.Is(err, gorm.ErrRecordNotFound) {
			sample := &model.DashboardPanelSample{SeriesKey: seriesKey, PanelID: panelID, SessionID: sessionID, BucketNs: bucket, Value: value}
			if err := q.Clauses(clause.OnConflict{DoNothing: true}).Create(sample); err != nil {
				return nil, err
			}
			d.changed("DashboardSample", "dashboard-panel")
			// This auxiliary history is bounded even for many variable combinations.
			expired, err := q.Where(q.BucketNs.Lt(cutoff)).Count()
			if err != nil {
				return nil, err
			}
			if expired > 0 {
				if _, err := q.Where(q.BucketNs.Lt(cutoff)).Delete(); err != nil {
					return nil, err
				}
			}
			count, err := q.Count()
			if err != nil {
				return nil, err
			}
			if count > dashboardSampleMaxRows {
				oldest, err := q.Order(q.BucketNs, q.SeriesKey).Limit(int(count - dashboardSampleMaxRows)).Find()
				if err != nil {
					return nil, err
				}
				for _, row := range oldest {
					if _, err := q.Where(q.SeriesKey.Eq(row.SeriesKey), q.BucketNs.Eq(row.BucketNs)).Delete(); err != nil {
						return nil, err
					}
				}
			}
		} else if err != nil {
			return nil, err
		}
	}
	rows, err := q.Where(q.SeriesKey.Eq(seriesKey), q.BucketNs.Gte(cutoff)).Order(q.BucketNs).Limit(int(dashboardSampleMaxAge/dashboardSampleInterval) + 1).Find()
	if err != nil {
		return nil, err
	}
	points := make([]DashboardSamplePoint, 0, len(rows))
	for _, row := range rows {
		points = append(points, DashboardSamplePoint{TimestampMs: row.BucketNs / int64(time.Millisecond), Value: row.Value})
	}
	return points, nil
}
