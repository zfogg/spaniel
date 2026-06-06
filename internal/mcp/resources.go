package mcp

import (
	"context"
	"fmt"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	schemaURI = "spaniel://schema"
	guideURI  = "spaniel://guide"
)

// registerResourcesAndPrompts adds MCP resources (schema, guide) and prompt
// templates for common debugging workflows (issue #104).
func (h *handler) registerResourcesAndPrompts(s *mcpsdk.Server) {
	s.AddResource(&mcpsdk.Resource{
		URI:         schemaURI,
		Name:        "spaniel-db-schema",
		Title:       "Spaniel DuckDB schema",
		Description: "Live table/column listing for the telemetry database, for writing query_sql queries.",
		MIMEType:    "text/plain",
	}, h.readSchemaResource)

	s.AddResource(&mcpsdk.Resource{
		URI:         guideURI,
		Name:        "spaniel-guide",
		Title:       "Using Spaniel via MCP",
		Description: "How Spaniel's data model and tools fit together for debugging traces.",
		MIMEType:    "text/markdown",
	}, h.readGuideResource)

	s.AddPrompt(&mcpsdk.Prompt{
		Name:        "debug_latest_trace",
		Description: "Investigate the most recent trace (optionally for a service): pull it, summarize the waterfall, and explain any detected issues, errors, or slow spans.",
		Arguments: []*mcpsdk.PromptArgument{
			{Name: "service", Description: "limit to traces from this service.name", Required: false},
		},
	}, promptDebugLatestTrace)

	s.AddPrompt(&mcpsdk.Prompt{
		Name:        "diff_against_baseline",
		Description: "Compare the active session against a baseline session and report regressions.",
		Arguments: []*mcpsdk.PromptArgument{
			{Name: "baseline", Description: "baseline session id or label", Required: false},
		},
	}, promptDiffAgainstBaseline)

	s.AddPrompt(&mcpsdk.Prompt{
		Name:        "find_bottlenecks",
		Description: "Find the biggest performance problems in the active session and propose fixes.",
	}, promptFindBottlenecks)
}

func (h *handler) readSchemaResource(ctx context.Context, _ *mcpsdk.ReadResourceRequest) (*mcpsdk.ReadResourceResult, error) {
	cols, rows, _, err := h.store.ReadOnlyQuery(ctx,
		"SELECT table_name, column_name, data_type FROM information_schema.columns WHERE table_schema = 'main' ORDER BY table_name, ordinal_position",
		5000)
	if err != nil {
		return nil, fmt.Errorf("read schema: %w", err)
	}
	_ = cols
	var b strings.Builder
	b.WriteString("Spaniel DuckDB schema (table: columns)\n\n")
	curTable := ""
	for _, r := range rows {
		table := fmt.Sprint(r[0])
		col := fmt.Sprint(r[1])
		typ := fmt.Sprint(r[2])
		if table != curTable {
			if curTable != "" {
				b.WriteString("\n")
			}
			fmt.Fprintf(&b, "%s:\n", table)
			curTable = table
		}
		fmt.Fprintf(&b, "  %s %s\n", col, typ)
	}
	return &mcpsdk.ReadResourceResult{
		Contents: []*mcpsdk.ResourceContents{{URI: schemaURI, MIMEType: "text/plain", Text: b.String()}},
	}, nil
}

const spanielGuide = `# Using Spaniel via MCP

Spaniel stores local OpenTelemetry data in DuckDB and exposes it through these tools.

## Data model
- **spans** — units of work in a trace (trace_id, span_id, parent_span_id, service, name, duration, status, attributes JSON).
- **logs** — log records, correlated to spans via trace_id/span_id.
- **metrics** — metric data points with exemplar links to traces.
- **sessions** — recording buckets; one is "active" (new telemetry lands there). Mark one as a baseline to diff against.
- **trace_issues** — detected problems: n_plus_one, error_chain, chatty_http, slow DB, etc.
- **lint_warnings** — OpenTelemetry semantic-convention violations.

## Typical workflow
1. list_traces (or search) to find a trace of interest.
2. get_trace to see the full waterfall plus its issues, correlated logs, and lint.
3. get_span for full attributes/events on a specific span.
4. list_issues / list_slow_spans to find what to fix.
5. After a code change, diff_sessions(baseline, compare) to confirm it helped.

## Escape hatch
query_sql runs read-only SQL directly against the tables above — use it for
custom aggregations the typed tools don't cover. Read the spaniel://schema
resource for column names.
`

func (h *handler) readGuideResource(_ context.Context, _ *mcpsdk.ReadResourceRequest) (*mcpsdk.ReadResourceResult, error) {
	return &mcpsdk.ReadResourceResult{
		Contents: []*mcpsdk.ResourceContents{{URI: guideURI, MIMEType: "text/markdown", Text: spanielGuide}},
	}, nil
}

// userPrompt builds a single-user-message prompt result.
func userPrompt(desc, text string) (*mcpsdk.GetPromptResult, error) {
	return &mcpsdk.GetPromptResult{
		Description: desc,
		Messages: []*mcpsdk.PromptMessage{{
			Role:    "user",
			Content: &mcpsdk.TextContent{Text: text},
		}},
	}, nil
}

func promptDebugLatestTrace(_ context.Context, req *mcpsdk.GetPromptRequest) (*mcpsdk.GetPromptResult, error) {
	service := ""
	if req != nil && req.Params != nil {
		service = req.Params.Arguments["service"]
	}
	scope := "the active session"
	if service != "" {
		scope = fmt.Sprintf("service %q", service)
	}
	return userPrompt("Investigate the latest trace",
		fmt.Sprintf("Use the Spaniel MCP tools to debug the most recent trace in %s.\n\n"+
			"1. Call list_traces%s to find the newest trace (prefer one with errors or detected issues).\n"+
			"2. Call get_trace on it and read the waterfall, issues, correlated logs, and lint warnings.\n"+
			"3. If a span looks suspicious, call get_span for its full attributes and events.\n"+
			"4. Summarize what the trace did, what was slow or failing, and the likely root cause.",
			scope, serviceArg(service)))
}

func promptDiffAgainstBaseline(_ context.Context, req *mcpsdk.GetPromptRequest) (*mcpsdk.GetPromptResult, error) {
	baseline := ""
	if req != nil && req.Params != nil {
		baseline = req.Params.Arguments["baseline"]
	}
	pick := "find the session marked as a baseline (see is_baseline in list_sessions)"
	if baseline != "" {
		pick = fmt.Sprintf("use baseline %q", baseline)
	}
	return userPrompt("Diff the active session against a baseline",
		fmt.Sprintf("Compare the current run against a baseline using Spaniel.\n\n"+
			"1. Call list_sessions to find the active session and %s.\n"+
			"2. Call diff_sessions with baseline and compare set to those session ids.\n"+
			"3. Report the duration delta, any spans added/removed, the DB-call delta, and the per-operation timing changes that regressed (status \"changed\" with a positive delta_pct).",
			pick))
}

func promptFindBottlenecks(_ context.Context, _ *mcpsdk.GetPromptRequest) (*mcpsdk.GetPromptResult, error) {
	return userPrompt("Find performance bottlenecks",
		"Find the biggest performance problems in the active Spaniel session.\n\n"+
			"1. Call list_issues to see detected problems (n_plus_one, error_chain, chatty_http, slow DB) ordered by time wasted.\n"+
			"2. Call list_slow_spans to see the slowest individual operations.\n"+
			"3. For the worst offenders, call get_trace to understand the context.\n"+
			"4. Propose concrete fixes, most impactful first.")
}

// serviceArg renders the optional service filter for list_traces in the prompt text.
func serviceArg(service string) string {
	if service == "" {
		return ""
	}
	return fmt.Sprintf(" with service=%q", service)
}
