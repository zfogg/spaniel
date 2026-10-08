package storage

import (
	"github.com/zfogg/spaniel/internal/model"
	"github.com/zfogg/spaniel/internal/storage/querygen"
)

type Span = model.Span

type SpanEvent = model.SpanEvent

type SpanLink = model.SpanLink

// spanCols is the read projection for spans: JSON columns are cast to VARCHAR
// so they scan into the string fields on Span.
const spanCols = `trace_id, span_id, parent_span_id, service_name, name, kind,
	start_ns, end_ns, duration_ns, status_code, status_message,
	attributes::VARCHAR AS attributes, resource::VARCHAR AS resource,
	session_id, session_label, received_at`

// AppendSpan buffers a span for batched insertion via the columnar Appender.
// Rows become durable on the next flush; ingest callers flush at the end of
// each request, so reads-after-ingest within a request observe the row.
func (d *DB) AppendSpan(s *Span) error { return d.batcher.AppendSpan(s) }

func (d *DB) InsertSpan(s *Span) error {
	if s.DurationNs == 0 {
		s.DurationNs = s.EndNs - s.StartNs
	}
	return d.namedQuery("storage.InsertSpan").Span.Create(s)
}

// InsertSpanEvents bulk-inserts the events attached to a span. Empty input
// is a no-op; on error any rows inserted before the failure are not rolled
// back (events are best-effort, not transactional, like spans).
func (d *DB) InsertSpanEvents(events []*SpanEvent) error {
	if len(events) == 0 {
		return nil
	}
	return d.namedQuery("storage.InsertSpanEvents").SpanEvent.Create(events...)
}

// ListEventsBySpan returns the events attached to a single span, in time order.
func (d *DB) ListEventsBySpan(spanID string) ([]*SpanEvent, error) {
	rows, err := d.query.SpanEvent.ListBySpan(spanID)
	if err != nil {
		return nil, err
	}
	out := make([]*SpanEvent, len(rows))
	for i := range rows {
		out[i] = &rows[i]
	}
	return out, nil
}

// InsertSpanLinks bulk-inserts links emitted by a span. Best-effort: not
// transactional, mirroring InsertSpanEvents.
func (d *DB) InsertSpanLinks(links []*SpanLink) error {
	if len(links) == 0 {
		return nil
	}
	return d.namedQuery("storage.InsertSpanLinks").SpanLink.Create(links...)
}

// ListLinksBySpan returns the outbound links emitted by a single span.
func (d *DB) ListLinksBySpan(spanID string) ([]*SpanLink, error) {
	q := d.namedQuery("storage.ListLinksBySpan")
	rows, err := q.SpanLink.ListBySpan(spanID)
	return spanLinkPointers(rows, err)
}

// ListIncomingLinks returns every link in the store whose target is the
// given trace ID — the "who links into this trace?" reverse lookup.
func (d *DB) ListIncomingLinks(linkedTraceID string) ([]*SpanLink, error) {
	q := d.namedQuery("storage.ListIncomingLinks")
	rows, err := q.SpanLink.ListIncomingByTrace(linkedTraceID)
	return spanLinkPointers(rows, err)
}

// ListLinksByTrace returns all span_links whose trace_id matches — used to
// bulk-attach links to spans when serving GET /api/traces/:id so the
// waterfall can show the link badge without a per-span round-trip.
func (d *DB) ListLinksByTrace(traceID string) ([]*SpanLink, error) {
	q := d.namedQuery("storage.ListLinksByTrace")
	rows, err := q.SpanLink.ListByTrace(traceID)
	return spanLinkPointers(rows, err)
}

func spanLinkPointers(rows []model.SpanLink, err error) ([]*SpanLink, error) {
	if err != nil {
		return nil, err
	}
	out := make([]*SpanLink, len(rows))
	for i := range rows {
		out[i] = &rows[i]
	}
	return out, nil
}

func (d *DB) GetSpan(spanID string) (*Span, error) {
	spans, err := d.query.Span.Where(d.query.Span.SpanID.Eq(spanID)).Limit(1).Find()
	if err != nil {
		return nil, err
	}
	if len(spans) == 0 {
		return nil, nil
	}
	return spans[0], nil
}

type SpanFilter struct {
	SessionID string
	Sort      string // "time" | "dur" | "name"
	Limit     int
	Page      int
	Service   string
	Name      string
	Kind      int
	HasKind   bool
}

type SpanRow = model.SpanRow

// ListSpans returns a flat list of spans with computed tag (n+1/slow/lint/error).
// Tags are derived from trace_issues (n+1), status_code (error), duration (slow),
// and lint_warnings (lint). Priority: n+1 > error > slow > lint.
func (d *DB) ListSpans(f SpanFilter) ([]*SpanRow, error) {
	if f.Limit <= 0 || f.Limit > 1000 {
		f.Limit = 100
	}
	if f.Page < 1 {
		f.Page = 1
	}
	rows, err := d.query.Span.ListTagged(f.SessionID, f.Service, f.Name, f.HasKind, f.Kind, f.Sort, f.Limit, (f.Page-1)*f.Limit)
	if err != nil {
		return nil, err
	}
	result := make([]*SpanRow, len(rows))
	for i := range rows {
		result[i] = &rows[i]
	}
	return result, nil
}

// CountSpans returns the full number of spans matching a list filter, before
// pagination. Keeping this separate from ListSpans makes the API metadata
// truthful without making callers load every row.
func (d *DB) CountSpans(f SpanFilter) (int, error) {
	q := d.query.Span.Where()
	if f.SessionID != "" {
		q = q.Where(d.query.Span.SessionID.Eq(f.SessionID))
	}
	if f.Service != "" {
		q = q.Where(d.query.Span.ServiceName.Eq(f.Service))
	}
	if f.Name != "" {
		q = q.Where(d.query.Span.Name.Eq(f.Name))
	}
	if f.HasKind {
		q = q.Where(d.query.Span.Kind.Eq(f.Kind))
	}
	count, err := q.Count()
	if err != nil {
		return 0, err
	}
	return int(count), nil
}

// SpanGroup is an operation-level aggregate. A group deliberately includes
// service and kind: the same name in different services or roles is not the
// same operation.
type SpanGroup = model.SpanGroup

func (d *DB) ListSpanGroups(f SpanFilter) ([]*SpanGroup, error) {
	if f.Limit <= 0 || f.Limit > 1000 {
		f.Limit = 100
	}
	if f.Page < 1 {
		f.Page = 1
	}
	rows, err := d.query.Span.ListGroups(f.SessionID, f.Limit, (f.Page-1)*f.Limit)
	if err != nil {
		return nil, err
	}
	result := make([]*SpanGroup, len(rows))
	for i := range rows {
		result[i] = &rows[i]
	}
	return result, nil
}

func (d *DB) CountSpanGroups(f SpanFilter) (int, error) {
	rows, err := d.query.Span.CountGroups(f.SessionID)
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	return int(rows[0].Count), nil
}

// DeleteSpansNamed removes every span with name and the rows that directly
// reference those spans. It is intended for narrow telemetry repair actions;
// it deliberately leaves all other spans in the affected traces intact.
func (d *DB) DeleteSpansNamed(name string) (int, error) {
	q := d.namedQuery("storage.DeleteSpansNamed")
	spans, err := q.Span.Where(q.Span.Name.Eq(name)).Find()
	if err != nil {
		return 0, err
	}
	if len(spans) == 0 {
		return 0, nil
	}
	ids := make([]string, 0, len(spans))
	for _, span := range spans {
		ids = append(ids, span.SpanID)
	}
	err = q.Transaction(func(tx *querygen.Query) error {
		if _, err := tx.SpanEvent.Where(tx.SpanEvent.SpanID.In(ids...)).Delete(); err != nil {
			return err
		}
		if _, err := tx.SpanLink.Where(tx.SpanLink.SpanID.In(ids...)).Or(tx.SpanLink.LinkedSpanID.In(ids...)).Delete(); err != nil {
			return err
		}
		if _, err := tx.LintWarning.Where(tx.LintWarning.SpanID.In(ids...)).Delete(); err != nil {
			return err
		}
		_, err := tx.Span.Where(tx.Span.SpanID.In(ids...)).Delete()
		return err
	})
	if err != nil {
		return 0, err
	}
	return len(ids), nil
}

// GetSpansBySession returns all spans for a session, ordered by start time.
func (d *DB) GetSpansBySession(sessionID string) ([]*Span, error) {
	return d.query.Span.Where(d.query.Span.SessionID.Eq(sessionID)).
		Order(d.query.Span.StartNs).Find()
}
