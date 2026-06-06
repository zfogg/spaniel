package mcp

import (
	"context"
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zfogg/spaniel/internal/storage"
)

// registerTraceTools adds the trace/span read tools (issue #98).
func (h *handler) registerTraceTools(s *mcpsdk.Server) {
	readOnly := &mcpsdk.ToolAnnotations{ReadOnlyHint: true}

	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "list_traces",
		Description: "List recent traces (most recent first), optionally filtered by service. Each row includes the root operation, duration, span count, and any detected issue kinds (e.g. n_plus_one, error_chain). Use this to find a trace to drill into, then call get_trace. Defaults to the active session.",
		Annotations: readOnly,
	}, h.listTraces)

	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "get_trace",
		Description: "Get the full span tree for one trace as an indented waterfall, plus detected performance issues, correlated log lines, and lint warnings. This is the main debugging tool: call it with a trace_id from list_traces or search to see exactly what happened, what was slow, and what went wrong.",
		Annotations: readOnly,
	}, h.getTrace)

	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "get_span",
		Description: "Get one span by id with its full attributes, resource, events (e.g. exceptions), and links to other spans/traces. Use after get_trace when you need the complete detail of a specific span.",
		Annotations: readOnly,
	}, h.getSpan)

	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "list_slow_spans",
		Description: "List the slowest spans in a session, longest-duration first, each tagged if it is part of an n+1, an error, slow, or has a lint warning. Use to find performance hot spots across all traces. Defaults to the active session.",
		Annotations: readOnly,
	}, h.listSlowSpans)
}

// resolveSession returns the requested session id, or the active session when
// the request leaves it blank.
func (h *handler) resolveSession(sessionID string) string {
	if sessionID != "" {
		return sessionID
	}
	return h.store.ActiveSessionID()
}

// ---- list_traces ----

type ListTracesInput struct {
	SessionID string `json:"session_id,omitempty" jsonschema:"session to query; defaults to the active session"`
	Service   string `json:"service,omitempty" jsonschema:"filter to traces whose root span has this service.name"`
	Limit     int    `json:"limit,omitempty" jsonschema:"max traces to return (default 50, max 200)"`
	Page      int    `json:"page,omitempty" jsonschema:"1-based page number (default 1)"`
}

type TraceSummary struct {
	TraceID     string   `json:"trace_id"`
	Service     string   `json:"service"`
	RootName    string   `json:"root_name"`
	Status      string   `json:"status"`
	DurationMs  float64  `json:"duration_ms"`
	SpanCount   int      `json:"span_count"`
	HasN1       bool     `json:"has_n1"`
	IssueKinds  []string `json:"issue_kinds"`
	StartUnixNs int64    `json:"start_unix_ns"`
}

type ListTracesOutput struct {
	SessionID string         `json:"session_id"`
	Count     int            `json:"count"`
	Traces    []TraceSummary `json:"traces"`
}

func (h *handler) listTraces(ctx context.Context, _ *mcpsdk.CallToolRequest, in ListTracesInput) (*mcpsdk.CallToolResult, ListTracesOutput, error) {
	limit := in.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	sessionID := h.resolveSession(in.SessionID)

	rows, err := h.store.WithContext(ctx).ListTraces(storage.TraceFilter{
		SessionID: sessionID,
		Service:   in.Service,
		Limit:     limit,
		Page:      in.Page,
	})
	if err != nil {
		return nil, ListTracesOutput{}, fmt.Errorf("list traces: %w", err)
	}

	out := ListTracesOutput{SessionID: sessionID, Traces: []TraceSummary{}}
	for _, r := range rows {
		kinds := r.IssueKinds
		if kinds == nil {
			kinds = []string{}
		}
		out.Traces = append(out.Traces, TraceSummary{
			TraceID:     r.TraceID,
			Service:     r.ServiceName,
			RootName:    r.Name,
			Status:      statusString(r.StatusCode),
			DurationMs:  nsToMs(r.DurationNs),
			SpanCount:   r.SpanCount,
			HasN1:       r.HasN1,
			IssueKinds:  kinds,
			StartUnixNs: r.StartNs,
		})
	}
	out.Count = len(out.Traces)
	return nil, out, nil
}

// ---- get_trace ----

type GetTraceInput struct {
	TraceID string `json:"trace_id" jsonschema:"the trace id to fetch (from list_traces or search)"`
	MaxLogs int    `json:"max_logs,omitempty" jsonschema:"max correlated log lines to include (default 100)"`
}

type TraceSpanOut struct {
	SpanID        string            `json:"span_id"`
	ParentSpanID  string            `json:"parent_span_id"`
	Service       string            `json:"service"`
	Name          string            `json:"name"`
	Kind          string            `json:"kind"`
	Status        string            `json:"status"`
	StatusMessage string            `json:"status_message,omitempty"`
	DurationMs    float64           `json:"duration_ms"`
	StartUnixNs   int64             `json:"start_unix_ns"`
	Attributes    map[string]string `json:"attributes,omitempty"`
}

type TraceIssueOut struct {
	Kind          string  `json:"kind"`
	Count         int     `json:"count"`
	WastedMs      float64 `json:"wasted_ms"`
	ParentSpanID  string  `json:"parent_span_id,omitempty"`
	ExampleSpanID string  `json:"example_span_id,omitempty"`
}

type TraceLogOut struct {
	TimestampNs int64  `json:"timestamp_ns"`
	Severity    string `json:"severity"`
	Body        string `json:"body"`
	SpanID      string `json:"span_id,omitempty"`
	Service     string `json:"service,omitempty"`
}

type LintOut struct {
	SpanID   string `json:"span_id"`
	RuleID   string `json:"rule_id"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

type GetTraceOutput struct {
	TraceID    string          `json:"trace_id"`
	SessionID  string          `json:"session_id"`
	SpanCount  int             `json:"span_count"`
	DurationMs float64         `json:"duration_ms"`
	Waterfall  string          `json:"waterfall"`
	Spans      []TraceSpanOut  `json:"spans"`
	Issues     []TraceIssueOut `json:"issues"`
	Logs       []TraceLogOut   `json:"logs"`
	Lint       []LintOut       `json:"lint"`
}

func (h *handler) getTrace(ctx context.Context, _ *mcpsdk.CallToolRequest, in GetTraceInput) (*mcpsdk.CallToolResult, GetTraceOutput, error) {
	if in.TraceID == "" {
		return nil, GetTraceOutput{}, fmt.Errorf("trace_id is required")
	}
	maxLogs := in.MaxLogs
	if maxLogs <= 0 {
		maxLogs = 100
	}

	spans, err := h.store.WithContext(ctx).GetTrace(in.TraceID)
	if err != nil {
		return nil, GetTraceOutput{}, fmt.Errorf("get trace: %w", err)
	}
	if len(spans) == 0 {
		return nil, GetTraceOutput{}, fmt.Errorf("trace %s not found", in.TraceID)
	}

	out := GetTraceOutput{
		TraceID:   in.TraceID,
		SessionID: spans[0].SessionID,
		SpanCount: len(spans),
		Waterfall: renderWaterfall(in.TraceID, spans),
		Spans:     []TraceSpanOut{},
		Issues:    []TraceIssueOut{},
		Logs:      []TraceLogOut{},
		Lint:      []LintOut{},
	}

	var minStart, maxEnd int64 = spans[0].StartNs, spans[0].EndNs
	for _, s := range spans {
		if s.StartNs < minStart {
			minStart = s.StartNs
		}
		if s.EndNs > maxEnd {
			maxEnd = s.EndNs
		}
		out.Spans = append(out.Spans, TraceSpanOut{
			SpanID:        s.SpanID,
			ParentSpanID:  s.ParentSpanID,
			Service:       s.ServiceName,
			Name:          s.Name,
			Kind:          kindString(s.Kind),
			Status:        statusString(s.StatusCode),
			StatusMessage: s.StatusMessage,
			DurationMs:    nsToMs(s.DurationNs),
			StartUnixNs:   s.StartNs,
			Attributes:    selectKeyAttrs(parseAttrs(s.Attributes)),
		})
	}
	out.DurationMs = nsToMs(maxEnd - minStart)

	// Detected issues (best-effort; absence isn't fatal).
	if issues, err := h.store.WithContext(ctx).GetTraceIssues(in.TraceID); err == nil {
		for _, is := range issues {
			out.Issues = append(out.Issues, TraceIssueOut{
				Kind:          is.Kind,
				Count:         is.Count,
				WastedMs:      nsToMs(is.WastedNs),
				ParentSpanID:  is.ParentSpanID,
				ExampleSpanID: is.ExampleSpanID,
			})
		}
	}

	// Correlated logs.
	if logs, err := h.store.WithContext(ctx).ListLogs(storage.LogFilter{TraceID: in.TraceID, Limit: maxLogs}); err == nil {
		for _, l := range logs {
			out.Logs = append(out.Logs, TraceLogOut{
				TimestampNs: l.TimestampNs,
				Severity:    severityString(l.Severity),
				Body:        l.Body,
				SpanID:      l.SpanID,
				Service:     l.ServiceName,
			})
		}
	}

	// Lint warnings for this trace (the store exposes them per-session; filter).
	if warnings, err := h.store.WithContext(ctx).ListLintWarnings(out.SessionID); err == nil {
		for _, w := range warnings {
			if w.TraceID != in.TraceID {
				continue
			}
			out.Lint = append(out.Lint, LintOut{
				SpanID:   w.SpanID,
				RuleID:   w.RuleID,
				Severity: w.Severity,
				Message:  w.Message,
			})
		}
	}

	// The waterfall is the most useful artifact for the model; surface it as the
	// text content while the structured output carries the precise data.
	result := &mcpsdk.CallToolResult{
		Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: out.Waterfall}},
	}
	return result, out, nil
}

// ---- get_span ----

type GetSpanInput struct {
	SpanID string `json:"span_id" jsonschema:"the span id to fetch"`
}

type SpanEventOut struct {
	TimeNs     int64             `json:"time_ns"`
	Name       string            `json:"name"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

type SpanLinkOut struct {
	LinkedTraceID string            `json:"linked_trace_id"`
	LinkedSpanID  string            `json:"linked_span_id"`
	Attributes    map[string]string `json:"attributes,omitempty"`
}

type GetSpanOutput struct {
	SpanID        string            `json:"span_id"`
	TraceID       string            `json:"trace_id"`
	ParentSpanID  string            `json:"parent_span_id"`
	Service       string            `json:"service"`
	Name          string            `json:"name"`
	Kind          string            `json:"kind"`
	Status        string            `json:"status"`
	StatusMessage string            `json:"status_message,omitempty"`
	DurationMs    float64           `json:"duration_ms"`
	StartUnixNs   int64             `json:"start_unix_ns"`
	Attributes    map[string]string `json:"attributes"`
	Resource      map[string]string `json:"resource"`
	Events        []SpanEventOut    `json:"events"`
	Links         []SpanLinkOut     `json:"links"`
}

func (h *handler) getSpan(ctx context.Context, _ *mcpsdk.CallToolRequest, in GetSpanInput) (*mcpsdk.CallToolResult, GetSpanOutput, error) {
	if in.SpanID == "" {
		return nil, GetSpanOutput{}, fmt.Errorf("span_id is required")
	}
	span, err := h.store.WithContext(ctx).GetSpan(in.SpanID)
	if err != nil {
		return nil, GetSpanOutput{}, fmt.Errorf("get span: %w", err)
	}
	if span == nil {
		return nil, GetSpanOutput{}, fmt.Errorf("span %s not found", in.SpanID)
	}

	out := GetSpanOutput{
		SpanID:        span.SpanID,
		TraceID:       span.TraceID,
		ParentSpanID:  span.ParentSpanID,
		Service:       span.ServiceName,
		Name:          span.Name,
		Kind:          kindString(span.Kind),
		Status:        statusString(span.StatusCode),
		StatusMessage: span.StatusMessage,
		DurationMs:    nsToMs(span.DurationNs),
		StartUnixNs:   span.StartNs,
		Attributes:    parseAttrs(span.Attributes),
		Resource:      parseAttrs(span.Resource),
		Events:        []SpanEventOut{},
		Links:         []SpanLinkOut{},
	}

	if events, err := h.store.WithContext(ctx).ListEventsBySpan(span.SpanID); err == nil {
		for _, e := range events {
			out.Events = append(out.Events, SpanEventOut{
				TimeNs:     e.TimeNs,
				Name:       e.Name,
				Attributes: parseAttrs(e.Attributes),
			})
		}
	}
	if links, err := h.store.WithContext(ctx).ListLinksBySpan(span.SpanID); err == nil {
		for _, l := range links {
			out.Links = append(out.Links, SpanLinkOut{
				LinkedTraceID: l.LinkedTraceID,
				LinkedSpanID:  l.LinkedSpanID,
				Attributes:    parseAttrs(l.Attributes),
			})
		}
	}
	return nil, out, nil
}

// ---- list_slow_spans ----

type ListSlowSpansInput struct {
	SessionID string `json:"session_id,omitempty" jsonschema:"session to query; defaults to the active session"`
	Limit     int    `json:"limit,omitempty" jsonschema:"max spans to return (default 25, max 200)"`
}

type SlowSpanOut struct {
	SpanID      string  `json:"span_id"`
	TraceID     string  `json:"trace_id"`
	Service     string  `json:"service"`
	Name        string  `json:"name"`
	DurationMs  float64 `json:"duration_ms"`
	Status      string  `json:"status"`
	Tag         string  `json:"tag,omitempty"`
	StartUnixNs int64   `json:"start_unix_ns"`
}

type ListSlowSpansOutput struct {
	SessionID string        `json:"session_id"`
	Count     int           `json:"count"`
	Spans     []SlowSpanOut `json:"spans"`
}

func (h *handler) listSlowSpans(ctx context.Context, _ *mcpsdk.CallToolRequest, in ListSlowSpansInput) (*mcpsdk.CallToolResult, ListSlowSpansOutput, error) {
	limit := in.Limit
	if limit <= 0 {
		limit = 25
	}
	if limit > 200 {
		limit = 200
	}
	sessionID := h.resolveSession(in.SessionID)

	rows, err := h.store.WithContext(ctx).ListSpans(storage.SpanFilter{
		SessionID: sessionID,
		Sort:      "dur",
		Limit:     limit,
	})
	if err != nil {
		return nil, ListSlowSpansOutput{}, fmt.Errorf("list spans: %w", err)
	}

	out := ListSlowSpansOutput{SessionID: sessionID, Spans: []SlowSpanOut{}}
	for _, r := range rows {
		out.Spans = append(out.Spans, SlowSpanOut{
			SpanID:      r.SpanID,
			TraceID:     r.TraceID,
			Service:     r.ServiceName,
			Name:        r.Name,
			DurationMs:  nsToMs(r.DurationNs),
			Status:      statusString(r.StatusCode),
			Tag:         r.Tag,
			StartUnixNs: r.StartNs,
		})
	}
	out.Count = len(out.Spans)
	return nil, out, nil
}
