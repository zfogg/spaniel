package storage

import (
	"strings"

	"github.com/zfogg/spaniel/internal/model"
)

// TraceRow and Stats are API projections rather than persisted schema models.
// They stay in storage so callers retain the existing public result types while
// generated DAOs own only actual database tables.
type TraceRow struct {
	TraceID       string   `json:"trace_id"`
	ServiceName   string   `json:"service_name"`
	Name          string   `json:"name"`
	Attributes    string   `json:"attributes"`
	StatusCode    int      `json:"status_code"`
	StartNs       int64    `json:"start_ns"`
	EndNs         int64    `json:"end_ns"`
	DurationNs    int64    `json:"duration_ns"`
	SessionID     string   `json:"session_id"`
	SessionLabel  string   `json:"session_label"`
	HasN1         bool     `json:"has_n1"`
	SpanCount     int      `json:"span_count"`
	ServiceCount  int      `json:"service_count,omitempty"`
	IssueKinds    []string `json:"issue_kinds" gorm:"-"`
	IssueKindsRaw string   `json:"-" gorm:"column:issue_kinds_raw"`
}

type TraceFilter struct {
	Services                    []string
	SessionID                   string
	Service                     string
	Operation                   string
	IssueKind                   string
	StatusCode                  int
	HasStatus                   bool
	FromNs, ToNs, MinDurationNs int64
	Limit                       int
	Page                        int
}

func (d *DB) ListTraces(f TraceFilter) ([]*TraceRow, error) {
	if f.Limit <= 0 || f.Limit > 1000 {
		f.Limit = 100
	}
	if f.Page < 1 {
		f.Page = 1
	}
	offset := (f.Page - 1) * f.Limit
	if f.Operation != "" {
		f.Operation = "%" + f.Operation + "%"
	}

	rows, err := d.query.Span.ListTraces(f.SessionID, f.Service, f.Operation, f.HasStatus, f.StatusCode, f.FromNs, f.ToNs, f.MinDurationNs, f.IssueKind, f.Limit, offset, f.Services)
	if err != nil {
		return nil, err
	}
	result := make([]*TraceRow, len(rows))
	for i, row := range rows {
		result[i] = &TraceRow{
			TraceID: row.TraceID, ServiceName: row.ServiceName, Name: row.Name,
			Attributes: row.Attributes, StatusCode: row.StatusCode, StartNs: row.StartNs,
			EndNs: row.EndNs, DurationNs: row.DurationNs, SessionID: row.SessionID,
			SessionLabel: row.SessionLabel, HasN1: row.HasN1, SpanCount: row.SpanCount,
			IssueKindsRaw: row.IssueKindsRaw, ServiceCount: row.ServiceCount,
		}
	}
	for _, row := range result {
		row.IssueKinds = parseKinds(row.IssueKindsRaw)
	}
	return result, nil
}

// CountTraces returns the number of root spans matching a trace-list filter,
// before pagination. A trace is represented by its root span in ListTraces.
func (d *DB) CountTraces(f TraceFilter) (int, error) {
	if f.Operation != "" {
		f.Operation = "%" + f.Operation + "%"
	}
	rows, err := d.query.Span.CountTraces(f.SessionID, f.Service, f.Operation, f.HasStatus, f.StatusCode, f.FromNs, f.ToNs, f.MinDurationNs, f.IssueKind, f.Services)
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	return int(rows[0].Count), nil
}

// parseKinds splits a comma-separated list of issue kind strings into a
// deduplicated, sorted slice.
func parseKinds(raw string) []string {
	if raw == "" {
		return []string{}
	}
	seen := map[string]bool{}
	for _, k := range splitTrim(raw, ',') {
		if k != "" {
			seen[k] = true
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sortStrings(out)
	return out
}

func splitTrim(s string, sep byte) []string {
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == sep {
			part := strings.TrimSpace(s[start:i])
			out = append(out, part)
			start = i + 1
		}
	}
	return out
}

func sortStrings(ss []string) {
	for i := 1; i < len(ss); i++ {
		for j := i; j > 0 && ss[j] < ss[j-1]; j-- {
			ss[j], ss[j-1] = ss[j-1], ss[j]
		}
	}
}

// TraceOverlayFilter scopes the "traces during this window" query used by
// the metrics chart overlay. Service narrows by the metric's service name;
// SessionID by the active session when set. FromNs/ToNs are inclusive.
type TraceOverlayFilter struct {
	Service   string
	SessionID string
	FromNs    int64
	ToNs      int64
	Limit     int
}

// TraceOverlay is the lightweight row returned to the frontend for the
// metrics chart overlay + correlated-traces panel — just enough to draw a
// marker and link out to /traces/:id.
type TraceOverlay = model.TraceOverlay

// ListTracesInWindow returns root spans (traces) whose start_ns falls in
// the [FromNs, ToNs] window for use as chart overlay markers. Caps at
// f.Limit (default 50).
func (d *DB) ListTracesInWindow(f TraceOverlayFilter) ([]*TraceOverlay, error) {
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	rows, err := d.query.Span.ListTraceOverlays(f.Service, f.SessionID, f.FromNs, f.ToNs, f.Limit)
	if err != nil {
		return nil, err
	}
	out := make([]*TraceOverlay, len(rows))
	for i := range rows {
		out[i] = &rows[i]
	}
	return out, nil
}

func (d *DB) GetTrace(traceID string) ([]*Span, error) {
	return d.query.Span.Where(d.query.Span.TraceID.Eq(traceID)).
		Order(d.query.Span.StartNs).Find()
}
