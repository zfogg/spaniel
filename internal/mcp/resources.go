package mcp

import (
	"context"
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	toongo "github.com/toon-format/toon-go"
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
		MIMEType:    "text/toon",
	}, h.readSchemaResource)

	s.AddResource(&mcpsdk.Resource{
		URI:         guideURI,
		Name:        "spaniel-guide",
		Title:       "Using Spaniel via MCP",
		Description: "How Spaniel's data model and tools fit together for debugging traces.",
		MIMEType:    "text/markdown",
	}, h.readGuideResource)

	h.registerDynamicResources(s)

	s.AddPrompt(&mcpsdk.Prompt{
		Name:        "investigate_incident",
		Description: "Run a cross-signal incident investigation using alerts, notifications, ingestion health, traces, spans, logs, and metrics.",
		Arguments: []*mcpsdk.PromptArgument{
			{Name: "focus", Description: "optional alert id, service name, symptom, or error text", Required: false},
		},
	}, promptInvestigateIncident)

	s.AddPrompt(&mcpsdk.Prompt{
		Name:        "compare_session_to_baseline",
		Description: "Compare a session to its baseline across timings, issues, route coverage, logs, and metrics.",
		Arguments: []*mcpsdk.PromptArgument{
			{Name: "baseline", Description: "baseline session id or label", Required: false},
			{Name: "compare", Description: "comparison session id or label; defaults to active", Required: false},
		},
	}, promptCompareSessionToBaseline)

	s.AddPrompt(&mcpsdk.Prompt{
		Name:        "draft_observability_pack",
		Description: "Design a cohesive dashboard and alert set from real telemetry, validate it, and optionally apply it when explicitly requested and writes are available.",
		Arguments: []*mcpsdk.PromptArgument{
			{Name: "goal", Description: "what the dashboard and alerts should help observe", Required: true},
			{Name: "service", Description: "optional service to focus on", Required: false},
		},
	}, promptDraftObservabilityPack)
}

func (h *handler) readSchemaResource(ctx context.Context, _ *mcpsdk.ReadResourceRequest) (*mcpsdk.ReadResourceResult, error) {
	cols, rows, _, err := h.store.ReadOnlyQuery(ctx,
		"SELECT table_name, column_name, data_type FROM information_schema.columns WHERE table_schema = 'main' ORDER BY table_name, ordinal_position",
		5000)
	if err != nil {
		return nil, fmt.Errorf("read schema: %w", err)
	}
	_ = cols
	type schemaColumn struct {
		TableName  string `json:"table_name"`
		ColumnName string `json:"column_name"`
		DataType   string `json:"data_type"`
	}
	items := make([]schemaColumn, 0, len(rows))
	for _, r := range rows {
		items = append(items, schemaColumn{TableName: fmt.Sprint(r[0]), ColumnName: fmt.Sprint(r[1]), DataType: fmt.Sprint(r[2])})
	}
	data, err := toongo.Marshal(struct {
		Columns []schemaColumn `json:"columns"`
	}{Columns: items})
	if err != nil {
		return nil, fmt.Errorf("encode schema as TOON: %w", err)
	}
	return &mcpsdk.ReadResourceResult{
		Contents: []*mcpsdk.ResourceContents{{URI: schemaURI, MIMEType: "text/toon", Text: string(data)}},
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

Dashboard and alert definitions can be inspected, validated, previewed, and
exported as YAML. With writes enabled, apply_* accepts typed MCP input while
import_* accepts the portable YAML document. Exported dashboard and alert YAML
includes a stable id: keep it to update that object, or remove it to create a
copy. Mutating tools are absent unless the server enables MCP writes.

Tool results are TOON text except the two YAML exports. Dynamic resources use
spaniel://dashboards/{dashboard_id}, spaniel://alerts/{alert_id},
spaniel://spans/{span_id}, spaniel://traces/{trace_id}, and scoped log/metric
templates so a client can attach a specific live object as model context.

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

func promptInvestigateIncident(_ context.Context, req *mcpsdk.GetPromptRequest) (*mcpsdk.GetPromptResult, error) {
	focus := ""
	if req != nil && req.Params != nil {
		focus = req.Params.Arguments["focus"]
	}
	focusText := "the most urgent current symptom"
	if focus != "" {
		focusText = fmt.Sprintf("%q", focus)
	}
	return userPrompt("Investigate an incident across Spaniel signals",
		fmt.Sprintf("Act as the incident investigator for %s. Build a timestamped evidence chain; do not infer causality from correlation alone.\n\n"+
			"1. Call get_server_info and get_ingestion_health first so missing telemetry is not mistaken for healthy behavior.\n"+
			"2. Call list_alerts(state=attention), list_notifications, and list_alert_history to identify the active symptom and its first transition.\n"+
			"3. Use list_traces/search, get_trace, get_span, query_logs, get_metrics, and get_metric_series to correlate the symptom across signals.\n"+
			"4. Separate confirmed facts, likely causes, and unknowns. Name the earliest causal-looking change, the blast radius, and the safest next diagnostic action.\n"+
			"5. Do not acknowledge or silence anything unless the user explicitly asked you to take that operator action.", focusText))
}

func promptCompareSessionToBaseline(_ context.Context, req *mcpsdk.GetPromptRequest) (*mcpsdk.GetPromptResult, error) {
	baseline, compare := "", ""
	if req != nil && req.Params != nil {
		baseline = req.Params.Arguments["baseline"]
		compare = req.Params.Arguments["compare"]
	}
	pickBaseline := "find the session marked as a baseline"
	if baseline != "" {
		pickBaseline = fmt.Sprintf("use baseline %q", baseline)
	}
	pickCompare := "use the active session as the comparison"
	if compare != "" {
		pickCompare = fmt.Sprintf("use comparison session %q", compare)
	}
	return userPrompt("Compare a session to a baseline",
		fmt.Sprintf("Produce a regression report grounded in Spaniel evidence.\n\n"+
			"1. Call list_sessions; %s and %s.\n"+
			"2. Call diff_sessions, then inspect the most regressed traces/spans and relevant logs and metric series.\n"+
			"3. Call get_coverage for both session scopes when possible; distinguish newly dark routes from routes that simply received no traffic.\n"+
			"4. Report improvements as well as regressions. Rank changes by user impact and absolute time, and call out sampling or ingestion-health differences that weaken the comparison.", pickBaseline, pickCompare))
}

func promptDraftObservabilityPack(_ context.Context, req *mcpsdk.GetPromptRequest) (*mcpsdk.GetPromptResult, error) {
	goal, service := "", ""
	if req != nil && req.Params != nil {
		goal = req.Params.Arguments["goal"]
		service = req.Params.Arguments["service"]
	}
	scope := "across the active session"
	if service != "" {
		scope = fmt.Sprintf("for service %q", service)
	}
	return userPrompt("Draft a dashboard and alert pack from observed telemetry",
		fmt.Sprintf("Design an observability pack %s for this goal: %q. Base it on fields and values actually present in Spaniel, not guessed schemas.\n\n"+
			"1. Inspect get_server_info, list_services, get_metrics, query_sql, and spaniel://schema. Review existing dashboards and alerts to avoid duplication.\n"+
			"2. Draft one coherent dashboard definition and the smallest useful alert set. Prefer queries that share explicit variables and return renderer-compatible shapes.\n"+
			"3. Call validate_dashboard and validate_alert for every definition. Repair validation failures before presenting anything.\n"+
			"4. By default, stop after showing the validated draft. Only call apply_dashboard/apply_alert if the user explicitly asked to save or apply it and get_server_info reports writes_enabled=true. A prompt is guidance, not permission.\n"+
			"5. Summarize why each panel and alert exists, expected signal volume, and likely false-positive modes.", scope, goal))
}
