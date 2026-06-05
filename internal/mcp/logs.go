package mcp

import (
	"context"
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zfogg/spaniel/internal/storage"
)

// registerLogTools adds the log and search read tools (issue #100).
func (h *handler) registerLogTools(s *mcpsdk.Server) {
	readOnly := &mcpsdk.ToolAnnotations{ReadOnlyHint: true}

	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "query_logs",
		Description: "Query stored log records, most recent first. Filter by trace_id or span_id to see logs correlated with a trace, by service, and by minimum severity (e.g. \"WARN\" to see warnings and above). Defaults to the active session.",
		Annotations: readOnly,
	}, h.queryLogs)

	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "search",
		Description: "Full-text search across traces, spans, logs, sessions, and services. Supports field filters like \"lint:db.system_required\" or \"n+1\" to find traces flagged by the linter/detectors. Leave session_id empty to search all sessions. Returns mixed-kind results; use the trace_id/span_id to drill in with get_trace/get_span.",
		Annotations: readOnly,
	}, h.search)
}

// ---- query_logs ----

type QueryLogsInput struct {
	SessionID   string `json:"session_id,omitempty" jsonschema:"session to query; defaults to the active session"`
	TraceID     string `json:"trace_id,omitempty" jsonschema:"only logs correlated with this trace"`
	SpanID      string `json:"span_id,omitempty" jsonschema:"only logs correlated with this span"`
	Service     string `json:"service,omitempty" jsonschema:"only logs from this service.name"`
	MinSeverity string `json:"min_severity,omitempty" jsonschema:"minimum severity: TRACE, DEBUG, INFO, WARN, ERROR, or FATAL"`
	Limit       int    `json:"limit,omitempty" jsonschema:"max log records to return (default 100, max 1000)"`
	Page        int    `json:"page,omitempty" jsonschema:"1-based page (ignored when service/min_severity is set)"`
}

type LogOut struct {
	TimestampNs int64  `json:"timestamp_ns"`
	Severity    string `json:"severity"`
	Body        string `json:"body"`
	Service     string `json:"service,omitempty"`
	TraceID     string `json:"trace_id,omitempty"`
	SpanID      string `json:"span_id,omitempty"`
}

type QueryLogsOutput struct {
	SessionID string   `json:"session_id"`
	Count     int      `json:"count"`
	Logs      []LogOut `json:"logs"`
}

func (h *handler) queryLogs(ctx context.Context, _ *mcpsdk.CallToolRequest, in QueryLogsInput) (*mcpsdk.CallToolResult, QueryLogsOutput, error) {
	limit := in.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	sessionID := h.resolveSession(in.SessionID)
	minSev := severityNum(in.MinSeverity)

	// service/min_severity are filtered in Go (the store's LogFilter doesn't
	// support them). When set, scan a wider window and ignore page so the filter
	// sees enough rows; otherwise honor the requested page.
	fetch := limit
	page := in.Page
	goFilter := in.Service != "" || minSev > 0
	if goFilter {
		fetch = 1000
		page = 1
	}

	logs, err := h.store.ListLogs(storage.LogFilter{
		SessionID: sessionID,
		TraceID:   in.TraceID,
		SpanID:    in.SpanID,
		Limit:     fetch,
		Page:      page,
	})
	if err != nil {
		return nil, QueryLogsOutput{}, fmt.Errorf("query logs: %w", err)
	}

	out := QueryLogsOutput{SessionID: sessionID, Logs: []LogOut{}}
	for _, l := range logs {
		if in.Service != "" && l.ServiceName != in.Service {
			continue
		}
		if l.Severity < minSev {
			continue
		}
		out.Logs = append(out.Logs, LogOut{
			TimestampNs: l.TimestampNs,
			Severity:    severityString(l.Severity),
			Body:        l.Body,
			Service:     l.ServiceName,
			TraceID:     l.TraceID,
			SpanID:      l.SpanID,
		})
		if len(out.Logs) >= limit {
			break
		}
	}
	out.Count = len(out.Logs)
	return nil, out, nil
}

// ---- search ----

type SearchInput struct {
	Query     string `json:"query" jsonschema:"search text; also supports lint:<rule> and n+1 filters"`
	SessionID string `json:"session_id,omitempty" jsonschema:"scope to a session; leave empty to search all sessions"`
	Limit     int    `json:"limit,omitempty" jsonschema:"max results (default 20, max 50)"`
}

type SearchHit struct {
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	Subtitle  string `json:"subtitle,omitempty"`
	TraceID   string `json:"trace_id,omitempty"`
	SpanID    string `json:"span_id,omitempty"`
	SessionID string `json:"session_id,omitempty"`
}

type SearchOutput struct {
	Query   string      `json:"query"`
	Count   int         `json:"count"`
	Results []SearchHit `json:"results"`
}

func (h *handler) search(ctx context.Context, _ *mcpsdk.CallToolRequest, in SearchInput) (*mcpsdk.CallToolResult, SearchOutput, error) {
	if in.Query == "" {
		return nil, SearchOutput{}, fmt.Errorf("query is required")
	}
	// Unlike the other tools, search deliberately does NOT default to the active
	// session — an empty session_id searches everything, which is usually what a
	// search is for.
	results, err := h.store.Search(in.Query, in.SessionID, in.Limit)
	if err != nil {
		return nil, SearchOutput{}, fmt.Errorf("search: %w", err)
	}

	out := SearchOutput{Query: in.Query, Results: []SearchHit{}}
	for _, r := range results {
		out.Results = append(out.Results, SearchHit{
			Kind:      r.Kind,
			Title:     r.Title,
			Subtitle:  r.Subtitle,
			TraceID:   r.TraceID,
			SpanID:    r.SpanID,
			SessionID: r.SessionID,
		})
	}
	out.Count = len(out.Results)
	return nil, out, nil
}
