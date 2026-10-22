package mcp

import (
	"context"
	"fmt"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zfogg/spaniel/internal/diff"
)

// registerSessionTools adds the session listing and diff tools (issue #102).
func (h *handler) registerSessionTools(s *mcpsdk.Server) {
	readOnly := &mcpsdk.ToolAnnotations{ReadOnlyHint: true}

	addTOONTool(s, &mcpsdk.Tool{
		Name:        "list_sessions",
		Description: "List recording sessions with span/trace/error counts, p95 latency, and active/baseline state. Filter by label, baseline/imported state, and created-time window. Use returned ids with session_id arguments or diff_sessions.",
		Annotations: readOnly,
	}, h.listSessions)

	addTOONTool(s, &mcpsdk.Tool{
		Name:        "diff_sessions",
		Description: "Compare two sessions (typically a baseline vs. a current run) and report what changed: overall duration delta, spans added/removed, DB-call delta, and per-(service, operation) timing changes. Use this to see whether a code change made things faster or slower.",
		Annotations: readOnly,
	}, h.diffSessions)
}

// ---- list_sessions ----

type SessionOut struct {
	ID         string  `json:"id"`
	Label      string  `json:"label"`
	IsActive   bool    `json:"is_active"`
	IsBaseline bool    `json:"is_baseline"`
	IsImported bool    `json:"is_imported"`
	SpanCount  int     `json:"span_count"`
	TraceCount int     `json:"trace_count"`
	ErrorCount int     `json:"error_count"`
	N1Count    int     `json:"n1_count"`
	P95Ms      float64 `json:"p95_ms"`
	CreatedAt  int64   `json:"created_at"`
	Note       string  `json:"note,omitempty"`
}

type ListSessionsInput struct {
	Search   string `json:"search,omitempty" jsonschema:"case-insensitive label or note substring"`
	Baseline *bool  `json:"baseline,omitempty" jsonschema:"only baseline (true) or non-baseline (false) sessions"`
	Imported *bool  `json:"imported,omitempty" jsonschema:"only imported (true) or local (false) sessions"`
	FromNs   int64  `json:"from_ns,omitempty" jsonschema:"inclusive created-time lower bound (unix ns); 0 = no bound"`
	ToNs     int64  `json:"to_ns,omitempty" jsonschema:"inclusive created-time upper bound (unix ns); 0 = no bound"`
	Limit    int    `json:"limit,omitempty" jsonschema:"max sessions to return (default 100, max 500)"`
	Page     int    `json:"page,omitempty" jsonschema:"1-based page number (default 1)"`
}

type ListSessionsOutput struct {
	ActiveSessionID string       `json:"active_session_id"`
	Count           int          `json:"count"`
	Sessions        []SessionOut `json:"sessions"`
}

func (h *handler) listSessions(ctx context.Context, _ *mcpsdk.CallToolRequest, in ListSessionsInput) (*mcpsdk.CallToolResult, ListSessionsOutput, error) {
	sessions, err := h.store.WithContext(ctx).ListSessions()
	if err != nil {
		return nil, ListSessionsOutput{}, fmt.Errorf("list sessions: %w", err)
	}
	active := h.store.ActiveSessionID()
	limit := in.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	page := in.Page
	if page < 1 {
		page = 1
	}
	needle := strings.ToLower(strings.TrimSpace(in.Search))
	out := ListSessionsOutput{ActiveSessionID: active, Sessions: []SessionOut{}}
	start, matched := (page-1)*limit, 0
	for _, s := range sessions {
		if needle != "" && !strings.Contains(strings.ToLower(s.Label+"\n"+s.Note), needle) || in.Baseline != nil && s.IsBaseline != *in.Baseline || in.Imported != nil && s.IsImported != *in.Imported || in.FromNs > 0 && s.CreatedAt < in.FromNs || in.ToNs > 0 && s.CreatedAt > in.ToNs {
			continue
		}
		if matched < start {
			matched++
			continue
		}
		matched++
		out.Sessions = append(out.Sessions, SessionOut{
			ID:         s.ID,
			Label:      s.Label,
			IsActive:   s.ID == active,
			IsBaseline: s.IsBaseline,
			IsImported: s.IsImported,
			SpanCount:  s.SpanCount,
			TraceCount: s.TraceCount,
			ErrorCount: s.ErrorCount,
			N1Count:    s.N1Count,
			P95Ms:      nsToMs(s.P95Ns),
			CreatedAt:  s.CreatedAt,
			Note:       s.Note,
		})
		if len(out.Sessions) >= limit {
			break
		}
	}
	out.Count = len(out.Sessions)
	return nil, out, nil
}

// ---- diff_sessions ----

type DiffSessionsInput struct {
	Baseline string `json:"baseline" jsonschema:"baseline session id (the 'before')"`
	Compare  string `json:"compare" jsonschema:"compare session id (the 'after')"`
}

type DiffSideOut struct {
	SessionID string  `json:"session_id"`
	Label     string  `json:"label"`
	TotalMs   float64 `json:"total_ms"`
	SpanCount int     `json:"span_count"`
	DBCalls   int     `json:"db_calls"`
}

type DiffSummaryOut struct {
	DurationDeltaMs  float64 `json:"duration_delta_ms"`
	DurationDeltaPct float64 `json:"duration_delta_pct"`
	SpansAdded       int     `json:"spans_added"`
	SpansRemoved     int     `json:"spans_removed"`
	DBCallDelta      int     `json:"db_call_delta"`
}

type DiffSpanOut struct {
	Service    string  `json:"service"`
	Name       string  `json:"name"`
	Status     string  `json:"status"`
	BaselineMs float64 `json:"baseline_ms"`
	CompareMs  float64 `json:"compare_ms"`
	DeltaPct   float64 `json:"delta_pct"`
}

type DiffSessionsOutput struct {
	Baseline DiffSideOut    `json:"baseline"`
	Compare  DiffSideOut    `json:"compare"`
	Summary  DiffSummaryOut `json:"summary"`
	Spans    []DiffSpanOut  `json:"spans"`
}

func (h *handler) diffSessions(ctx context.Context, _ *mcpsdk.CallToolRequest, in DiffSessionsInput) (*mcpsdk.CallToolResult, DiffSessionsOutput, error) {
	if in.Baseline == "" || in.Compare == "" {
		return nil, DiffSessionsOutput{}, fmt.Errorf("both baseline and compare session ids are required")
	}

	baseSess, err := h.store.WithContext(ctx).GetSession(in.Baseline)
	if err != nil || baseSess == nil {
		return nil, DiffSessionsOutput{}, fmt.Errorf("baseline session %q not found", in.Baseline)
	}
	cmpSess, err := h.store.WithContext(ctx).GetSession(in.Compare)
	if err != nil || cmpSess == nil {
		return nil, DiffSessionsOutput{}, fmt.Errorf("compare session %q not found", in.Compare)
	}

	baseSpans, err := h.store.WithContext(ctx).GetSpansBySession(in.Baseline)
	if err != nil {
		return nil, DiffSessionsOutput{}, fmt.Errorf("load baseline spans: %w", err)
	}
	cmpSpans, err := h.store.WithContext(ctx).GetSpansBySession(in.Compare)
	if err != nil {
		return nil, DiffSessionsOutput{}, fmt.Errorf("load compare spans: %w", err)
	}

	r := diff.Compute(baseSess, cmpSess, baseSpans, cmpSpans)

	out := DiffSessionsOutput{
		Baseline: DiffSideOut{SessionID: r.Baseline.SessionID, Label: r.Baseline.Label, TotalMs: nsToMs(r.Baseline.TotalDurationNs), SpanCount: r.Baseline.SpanCount, DBCalls: r.Baseline.DBCalls},
		Compare:  DiffSideOut{SessionID: r.Compare.SessionID, Label: r.Compare.Label, TotalMs: nsToMs(r.Compare.TotalDurationNs), SpanCount: r.Compare.SpanCount, DBCalls: r.Compare.DBCalls},
		Summary: DiffSummaryOut{
			DurationDeltaMs:  nsToMs(r.Summary.DurationDeltaNs),
			DurationDeltaPct: r.Summary.DurationDeltaPct,
			SpansAdded:       r.Summary.SpansAdded,
			SpansRemoved:     r.Summary.SpansRemoved,
			DBCallDelta:      r.Summary.DBCallDelta,
		},
		Spans: []DiffSpanOut{},
	}
	for _, s := range r.Spans {
		out.Spans = append(out.Spans, DiffSpanOut{
			Service:    s.ServiceName,
			Name:       s.Name,
			Status:     s.Status,
			BaselineMs: nsToMs(s.BaselineDurationNs),
			CompareMs:  nsToMs(s.CompareDurationNs),
			DeltaPct:   s.DeltaPct,
		})
	}
	return nil, out, nil
}
