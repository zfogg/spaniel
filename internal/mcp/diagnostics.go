package mcp

import (
	"context"
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerDiagnosticTools adds the issue/lint read tools (issue #99).
func (h *handler) registerDiagnosticTools(s *mcpsdk.Server) {
	readOnly := &mcpsdk.ToolAnnotations{ReadOnlyHint: true}

	addTOONTool(s, &mcpsdk.Tool{
		Name:        "list_lint_warnings",
		Description: "List OpenTelemetry semantic-convention lint warnings (e.g. missing db.system.name, missing service.name) plus detector findings projected as lint entries. Filter by trace, rule, severity, and created-time window; defaults to the active session. Use this to find instrumentation gaps.",
		Annotations: readOnly,
	}, h.listLintWarnings)
}

// ---- list_lint_warnings ----

type ListLintInput struct {
	SessionID string `json:"session_id,omitempty" jsonschema:"session to query; defaults to the active session"`
	TraceID   string `json:"trace_id,omitempty" jsonschema:"filter to a single trace"`
	RuleID    string `json:"rule_id,omitempty" jsonschema:"only this lint or detector rule id"`
	Severity  string `json:"severity,omitempty" jsonschema:"only this severity, e.g. warn or error"`
	FromNs    int64  `json:"from_ns,omitempty" jsonschema:"inclusive finding-created lower bound (unix ns); 0 = no bound"`
	ToNs      int64  `json:"to_ns,omitempty" jsonschema:"inclusive finding-created upper bound (unix ns); 0 = no bound"`
	Limit     int    `json:"limit,omitempty" jsonschema:"max warnings to return (default 200, max 500)"`
	Page      int    `json:"page,omitempty" jsonschema:"1-based page number (default 1)"`
}

type LintWarningOut struct {
	SpanID    string `json:"span_id"`
	TraceID   string `json:"trace_id"`
	RuleID    string `json:"rule_id"`
	Severity  string `json:"severity"`
	Message   string `json:"message"`
	CreatedAt int64  `json:"created_at"`
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
	page := in.Page
	if page < 1 {
		page = 1
	}
	sessionID := h.resolveSession(in.SessionID)

	warnings, err := h.store.WithContext(ctx).ListLintWarnings(sessionID)
	if err != nil {
		return nil, ListLintOutput{}, fmt.Errorf("list lint warnings: %w", err)
	}

	out := ListLintOutput{SessionID: sessionID, Warnings: []LintWarningOut{}}
	matched := 0
	start := (page - 1) * limit
	for _, w := range warnings {
		if in.TraceID != "" && w.TraceID != in.TraceID {
			continue
		}
		if in.RuleID != "" && w.RuleID != in.RuleID || in.Severity != "" && w.Severity != in.Severity || in.FromNs > 0 && w.CreatedAt < in.FromNs || in.ToNs > 0 && w.CreatedAt > in.ToNs {
			continue
		}
		if matched < start {
			matched++
			continue
		}
		matched++
		out.Warnings = append(out.Warnings, LintWarningOut{
			SpanID: w.SpanID, TraceID: w.TraceID, RuleID: w.RuleID,
			Severity: w.Severity, Message: w.Message, CreatedAt: w.CreatedAt,
		})
		if len(out.Warnings) >= limit {
			break
		}
	}
	out.Count = len(out.Warnings)
	return nil, out, nil
}
