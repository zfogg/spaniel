package storage

import (
	"strconv"

	"github.com/zfogg/spaniel/internal/model"
	"github.com/zfogg/spaniel/internal/storage/querygen"
)

type SourceStats struct {
	Service        string  `json:"service"`
	AcceptedPerSec float64 `json:"accepted_per_sec"`
	RejectedPerSec float64 `json:"rejected_per_sec"`
	ErrorRate      float64 `json:"error_rate"`
	BytesPerSec    float64 `json:"bytes_per_sec"`
	LastSeenNs     int64   `json:"last_seen_ns"`
}

type Stats struct {
	SpanCount           int   `json:"span_count"`
	TraceCount          int   `json:"trace_count"`
	LogCount            int   `json:"log_count"`
	DBSize              int64 `json:"db_size"`
	SessionCount        int   `json:"session_count"`
	OldestSessionAt     int64 `json:"oldest_session_at"`
	DroppedSpans        int64 `json:"dropped_spans"`
	DroppedLogs         int64 `json:"dropped_logs"`
	DroppedMetricPoints int64 `json:"dropped_metric_points"`
	LastDropAt          int64 `json:"last_drop_at"`
	StorageFull         bool  `json:"storage_full"`
	Throughput
}

type Throughput struct {
	SpansPerSec     float64 `json:"spans_per_sec"`
	LogsPerSec      float64 `json:"logs_per_sec"`
	MetricsPerSec   float64 `json:"metrics_per_sec"`
	PeakSpansPerSec float64 `json:"peak_spans_per_sec"`
}

func (d *DB) GetStats(sessionID string) (*Stats, error) {
	s := &Stats{StorageFull: d.Full()}

	// Every complete OpenTelemetry trace has exactly one root span. Counting
	// roots avoids an exact COUNT(DISTINCT trace_id), whose hash table grew to
	// multiple GiB on a modest on-disk store and stalled the stats endpoint.
	// This named query combines all dashboard aggregates into one round trip.
	rows, err := d.namedQuery("storage.GetStats").Span.GetStats(sessionID)
	if err != nil {
		return nil, err
	}
	if len(rows) > 0 {
		s.SpanCount = int(rows[0].SpanCount)
		s.TraceCount = int(rows[0].TraceCount)
		s.LogCount = int(rows[0].LogCount)
		s.SessionCount = int(rows[0].SessionCount)
		s.OldestSessionAt = rows[0].OldestSessionAt
	}

	// Use live blocks plus the WAL rather than the file's high-water mark. A
	// deleted session frees blocks immediately, while DuckDB may retain its file
	// allocation for future writes.
	s.DBSize = d.UsedSize()
	return s, nil
}

// GetSourceStats returns per-service ingest stats for the given session
// (or all sessions when sessionID is ""). Rates are derived from received_at.
func (d *DB) GetSourceStats(sessionID string) ([]SourceStats, error) {
	rows, err := d.query.Span.ListSourceStats(sessionID)
	if err != nil {
		return nil, err
	}

	out := make([]SourceStats, 0, len(rows))
	for _, r := range rows {
		durSec := float64(r.LastSeen-r.FirstSeen) / 1e9
		if durSec < 1 {
			durSec = 1
		}
		var errRate float64
		if r.SpanCount > 0 {
			errRate = float64(r.ErrorCount) / float64(r.SpanCount)
		}
		out = append(out, SourceStats{
			Service:        r.ServiceName,
			AcceptedPerSec: float64(r.SpanCount) / durSec,
			RejectedPerSec: 0,
			ErrorRate:      errRate,
			BytesPerSec:    float64(r.BytesTotal) / durSec,
			LastSeenNs:     r.LastSeen,
		})
	}
	return out, nil
}

// LoadDropCounters reads the persisted drop counters from the meta table.
// Returns zeros (no error) if not yet written.
func (d *DB) LoadDropCounters() (spans, logs, metrics int64, err error) {
	rows, err := d.query.Meta.Where(d.query.Meta.Key.In(
		"dropped_spans", "dropped_logs", "dropped_metric_points",
	)).Find()
	if err != nil {
		return
	}
	for _, r := range rows {
		val, parseErr := strconv.ParseInt(r.Value, 10, 64)
		if parseErr != nil {
			continue
		}
		switch r.Key {
		case "dropped_spans":
			spans = val
		case "dropped_logs":
			logs = val
		case "dropped_metric_points":
			metrics = val
		}
	}
	return
}

// SaveDropCounters writes the current drop counters to the meta table.
func (d *DB) SaveDropCounters(spans, logs, metrics int64) error {
	q := d.namedQuery("storage.SaveDropCounters")
	return q.Transaction(func(tx *querygen.Query) error {
		for _, entry := range []*model.Meta{
			{Key: "dropped_spans", Value: strconv.FormatInt(spans, 10)},
			{Key: "dropped_logs", Value: strconv.FormatInt(logs, 10)},
			{Key: "dropped_metric_points", Value: strconv.FormatInt(metrics, 10)},
		} {
			if err := tx.Meta.UpsertValue(entry.Key, entry.Value); err != nil {
				return err
			}
		}
		return nil
	})
}
