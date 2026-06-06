package mcp

import (
	"context"
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerDiagnosticTools adds the issue/lint read tools (issue #99).
func (h *handler) registerDiagnosticTools(s *mcpsdk.Server) {
	readOnly := &mcpsdk.ToolAnnotations{ReadOnlyHint: true}

	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "list_issues",
		Description: "List performance issues Spaniel detected (n_plus_one, error_chain, chatty_http, slow DB, etc.), ordered by time wasted. Pass trace_id to scope to one trace, otherwise lists the session's issues (defaults to the active session). Each issue includes how many times it occurred, the time wasted, and an example span to jump to.",
		Annotations: readOnly,
	}, h.listIssues)

	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "list_lint_warnings",
		Description: "List OpenTelemetry semantic-convention lint warnings (e.g. missing db.system, missing service.name) plus detector findings projected as lint entries. Defaults to the active session; pass trace_id to scope to one trace. Use this to find instrumentation gaps.",
		Annotations: readOnly,
	}, h.listLintWarnings)
}

// ---- list_issues ----

type ListIssuesInput struct {
	SessionID string `json:"session_id,omitempty" jsonschema:"session to query when trace_id is not set; defaults to the active session"`
	TraceID   string `json:"trace_id,omitempty" jsonschema:"scope to a single trace; takes precedence over session_id"`
}

type IssueOut struct {
	Kind          string  `json:"kind"`
	TraceID       string  `json:"trace_id"`
	Count         int     `json:"count"`
	WastedMs      float64 `json:"wasted_ms"`
	Fingerprint   string  `json:"fingerprint,omitempty"`
	ParentSpanID  string  `json:"parent_span_id,omitempty"`
	ExampleSpanID string  `json:"example_span_id,omitempty"`
}

type ListIssuesOutput struct {
	SessionID string     `json:"session_id,omitempty"`
	TraceID   string     `json:"trace_id,omitempty"`
	Count     int        `json:"count"`
	Issues    []IssueOut `json:"issues"`
}

func (h *handler) listIssues(ctx context.Context, _ *mcpsdk.CallToolRequest, in ListIssuesInput) (*mcpsdk.CallToolResult, ListIssuesOutput, error) {
	out := ListIssuesOutput{Issues: []IssueOut{}}

	// Trace-scoped takes precedence over session-scoped.
	if in.TraceID != "" {
		out.TraceID = in.TraceID
		rows, err := h.store.WithContext(ctx).GetTraceIssues(in.TraceID)
		if err != nil {
			return nil, ListIssuesOutput{}, fmt.Errorf("get trace issues: %w", err)
		}
		for _, is := range rows {
			out.Issues = append(out.Issues, IssueOut{
				Kind: is.Kind, TraceID: is.TraceID, Count: is.Count, WastedMs: nsToMs(is.WastedNs),
				Fingerprint: is.Fingerprint, ParentSpanID: is.ParentSpanID, ExampleSpanID: is.ExampleSpanID,
			})
		}
		out.Count = len(out.Issues)
		return nil, out, nil
	}

	sessionID := h.resolveSession(in.SessionID)
	out.SessionID = sessionID
	rows, err2 := h.store.WithContext(ctx).ListTraceIssuesBySession(sessionID)
	if err2 != nil {
		return nil, ListIssuesOutput{}, fmt.Errorf("list session issues: %w", err2)
	}
	for _, is := range rows {
		out.Issues = append(out.Issues, IssueOut{
			Kind: is.Kind, TraceID: is.TraceID, Count: is.Count, WastedMs: nsToMs(is.WastedNs),
			Fingerprint: is.Fingerprint, ParentSpanID: is.ParentSpanID, ExampleSpanID: is.ExampleSpanID,
		})
	}
	out.Count = len(out.Issues)
	return nil, out, nil
}

// ---- list_lint_warnings ----

type ListLintInput struct {
	SessionID string `json:"session_id,omitempty" jsonschema:"session to query; defaults to the active session"`
	TraceID   string `json:"trace_id,omitempty" jsonschema:"filter to a single trace"`
	Limit     int    `json:"limit,omitempty" jsonschema:"max warnings to return (default 200, max 500)"`
}

type LintWarningOut struct {
	SpanID   string `json:"span_id"`
	TraceID  string `json:"trace_id"`
	RuleID   string `json:"rule_id"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

type ListLintOutput struct {
	SessionID string           `json:"session_id"`
	Count     int              `json:"count"`
	Warnings  []LintWarningOut `json:"warnings"`
}

func (h *handler) listLintWarnings(ctx context.Context, _ *mcpsdk.CallToolRequest, in ListLintInput) (*mcpsdk.CallToolResult, ListLintOutput, error) {
	limit := in.Limit
	if limit <= 0 {
		limit = 200
	}
	if limit > 500 {
		limit = 500
	}
	sessionID := h.resolveSession(in.SessionID)

	warnings, err := h.store.WithContext(ctx).ListLintWarnings(sessionID)
	if err != nil {
		return nil, ListLintOutput{}, fmt.Errorf("list lint warnings: %w", err)
	}

	out := ListLintOutput{SessionID: sessionID, Warnings: []LintWarningOut{}}
	for _, w := range warnings {
		if in.TraceID != "" && w.TraceID != in.TraceID {
			continue
		}
		out.Warnings = append(out.Warnings, LintWarningOut{
			SpanID: w.SpanID, TraceID: w.TraceID, RuleID: w.RuleID,
			Severity: w.Severity, Message: w.Message,
		})
		if len(out.Warnings) >= limit {
			break
		}
	}
	out.Count = len(out.Warnings)
	return nil, out, nil
}
