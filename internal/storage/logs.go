package storage

import "github.com/zfogg/spaniel/internal/model"

type Log = model.Log

// AppendLog buffers a log record for batched insertion.
func (d *DB) AppendLog(l *Log) error { return d.batcher.AppendLog(l) }

func (d *DB) InsertLog(l *Log) error {
	return d.namedQuery("InsertLog").Log.Create(l)
}

type LogFilter struct {
	Attributes  map[string]string
	Services    []string
	SessionID   string
	Service     string
	TraceID     string
	SpanID      string
	MinSeverity int
	MaxSeverity int
	FromNs      int64
	ToNs        int64
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
	if len(f.Attributes) > 0 {
		rows, err := d.query.Log.ListAttributeFiltered(f.SessionID, f.Service, f.TraceID, f.SpanID, f.MinSeverity, f.MaxSeverity, f.FromNs, f.ToNs, f.Services, encodeAttributeFilters(f.Attributes), f.Limit, offset)
		out := make([]*Log, len(rows))
		for i := range rows {
			out[i] = &rows[i]
		}
		return out, err
	}

	q := d.query.Log.Where()
	if f.SessionID != "" {
		q = q.Where(d.query.Log.SessionID.Eq(f.SessionID))
	}
	if len(f.Services) > 0 {
		q = q.Where(d.query.Log.ServiceName.In(f.Services...))
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
	if f.FromNs > 0 {
		q = q.Where(d.query.Log.TimestampNs.Gte(f.FromNs))
	}
	if f.ToNs > 0 {
		q = q.Where(d.query.Log.TimestampNs.Lte(f.ToNs))
	}
	return q.Order(d.query.Log.TimestampNs.Desc()).Limit(f.Limit).Offset(offset).Find()
}

func (d *DB) CountLogs(f LogFilter) (int, error) {
	if len(f.Attributes) > 0 {
		rows, err := d.query.Log.CountAttributeFiltered(f.SessionID, f.Service, f.TraceID, f.SpanID, f.MinSeverity, f.MaxSeverity, f.FromNs, f.ToNs, f.Services, encodeAttributeFilters(f.Attributes))
		if err != nil {
			return 0, err
		}
		if len(rows) == 0 {
			return 0, nil
		}
		return int(rows[0].Count), nil
	}
	q := d.query.Log.Where()
	if f.SessionID != "" {
		q = q.Where(d.query.Log.SessionID.Eq(f.SessionID))
	}
	if len(f.Services) > 0 {
		q = q.Where(d.query.Log.ServiceName.In(f.Services...))
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
	if f.FromNs > 0 {
		q = q.Where(d.query.Log.TimestampNs.Gte(f.FromNs))
	}
	if f.ToNs > 0 {
		q = q.Where(d.query.Log.TimestampNs.Lte(f.ToNs))
	}
	count, err := q.Count()
	return int(count), err
}
