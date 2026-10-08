package storage

import "github.com/zfogg/spaniel/internal/model"

type Log = model.Log

// AppendLog buffers a log record for batched insertion.
func (d *DB) AppendLog(l *Log) error { return d.batcher.AppendLog(l) }

func (d *DB) InsertLog(l *Log) error {
	return d.namedQuery("InsertLog").Log.Create(l)
}

type LogFilter struct {
	SessionID   string
	Service     string
	TraceID     string
	SpanID      string
	MinSeverity int
	MaxSeverity int
	Limit       int
	Page        int
}

func (d *DB) ListLogs(f LogFilter) ([]*Log, error) {
	if f.Limit <= 0 || f.Limit > 1000 {
		f.Limit = 500
	}
	if f.Page < 1 {
		f.Page = 1
	}
	offset := (f.Page - 1) * f.Limit

	q := d.query.Log.Where()
	if f.SessionID != "" {
		q = q.Where(d.query.Log.SessionID.Eq(f.SessionID))
	}
	if f.Service != "" {
		q = q.Where(d.query.Log.ServiceName.Eq(f.Service))
	}
	if f.TraceID != "" {
		q = q.Where(d.query.Log.TraceID.Eq(f.TraceID))
	}
	if f.SpanID != "" {
		q = q.Where(d.query.Log.SpanID.Eq(f.SpanID))
	}
	if f.MinSeverity > 0 {
		q = q.Where(d.query.Log.Severity.Gte(f.MinSeverity))
	}
	if f.MaxSeverity > 0 {
		q = q.Where(d.query.Log.Severity.Lte(f.MaxSeverity))
	}
	return q.Order(d.query.Log.TimestampNs.Desc()).Limit(f.Limit).Offset(offset).Find()
}

func (d *DB) CountLogs(f LogFilter) (int, error) {
	q := d.query.Log.Where()
	if f.SessionID != "" {
		q = q.Where(d.query.Log.SessionID.Eq(f.SessionID))
	}
	if f.Service != "" {
		q = q.Where(d.query.Log.ServiceName.Eq(f.Service))
	}
	if f.TraceID != "" {
		q = q.Where(d.query.Log.TraceID.Eq(f.TraceID))
	}
	if f.SpanID != "" {
		q = q.Where(d.query.Log.SpanID.Eq(f.SpanID))
	}
	if f.MinSeverity > 0 {
		q = q.Where(d.query.Log.Severity.Gte(f.MinSeverity))
	}
	if f.MaxSeverity > 0 {
		q = q.Where(d.query.Log.Severity.Lte(f.MaxSeverity))
	}
	count, err := q.Count()
	return int(count), err
}
