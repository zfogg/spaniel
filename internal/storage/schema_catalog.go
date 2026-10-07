package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// SchemaCatalog is the reviewed public contract for safe, queryable telemetry
// views. It deliberately excludes physical DuckDB tables.
type SchemaCatalog struct {
	Version     string       `json:"version"`
	Fingerprint string       `json:"fingerprint"`
	Views       []SchemaView `json:"views"`
	Parameters  []string     `json:"parameters"`
}
type SchemaView struct {
	Name    string         `json:"name"`
	Purpose string         `json:"purpose"`
	Columns []SchemaColumn `json:"columns"`
	Samples []SchemaSample `json:"samples"`
}
type SchemaColumn struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	UseItFor    string `json:"use_it_for"`
	Sensitivity string `json:"sensitivity"`
}
type SchemaSample struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	DisplayType string `json:"display_type"`
	SQL         string `json:"sql"`
	Explanation string `json:"explanation"`
}

var schemaViewMetadata = map[string]struct{ Purpose string }{
	"telemetry_spans":         {"One row per recorded span; use for operation latency and errors."},
	"telemetry_traces":        {"One row per trace, derived from its spans."},
	"telemetry_logs":          {"One row per structured log record."},
	"telemetry_metrics":       {"One row per metric point."},
	"telemetry_span_events":   {"Timestamped OpenTelemetry events attached to spans."},
	"telemetry_span_links":    {"Cross-trace and asynchronous span relationships."},
	"telemetry_metric_series": {"Bounded directory of observed metric streams."},
	"telemetry_findings":      {"Normalized Spaniel lint and trace findings."},
	"telemetry_sessions":      {"Capture sessions for advanced comparisons."},
}

var columnHints = map[string]struct{ description, use, sensitivity string }{
	"trace_id":                {"OpenTelemetry trace identifier.", "Trace-list links and correlating logs with spans.", "identifier"},
	"span_id":                 {"OpenTelemetry span identifier.", "Span-list links and parent-child investigation.", "identifier"},
	"service_name":            {"Service that emitted the record.", "Grouping or filtering by service.", "none"},
	"name":                    {"Operation, log, or metric name.", "Operation breakdowns and metric selection.", "none"},
	"start_ns":                {"Start time in Unix nanoseconds.", "Time-series buckets and ordering.", "none"},
	"timestamp_ns":            {"Event time in Unix nanoseconds.", "Time-series buckets and ordering.", "none"},
	"duration_ns":             {"Duration in nanoseconds.", "Latency percentiles, slow-operation tables, and heatmaps.", "none"},
	"status_code":             {"OpenTelemetry status code.", "Error-rate filters and status summaries.", "none"},
	"attributes":              {"Serialized span or log attributes as JSON.", "Filtering known semantic-convention fields with json_extract_string.", "may contain user data"},
	"resource":                {"Serialized resource attributes as JSON.", "Filtering deployment and service resource fields.", "may contain user data"},
	"body":                    {"Log body.", "Log search and log-list panels.", "may contain user data"},
	"severity":                {"OpenTelemetry log severity number.", "Severity breakdowns and alert filters.", "none"},
	"value":                   {"Recorded numeric metric value.", "Metric charts and scalar values.", "none"},
	"session_id":              {"Spaniel capture session identifier.", "The automatic $session_id scope; normally do not hard-code it.", "identifier"},
	"session_label":           {"Human-readable label of the capture session.", "Grouping or labeling results by capture session.", "none"},
	"received_at":             {"Time Spaniel received the signal, in Unix nanoseconds.", "Ingestion-rate analysis and arrival-time ordering.", "none"},
	"sampled":                 {"Whether the span was retained by the sampling policy.", "Separating sampled telemetry from unsampled records.", "none"},
	"parent_span_id":          {"Identifier of the parent span, if one exists.", "Reconstructing a trace tree or finding root spans.", "identifier"},
	"kind":                    {"Span kind or normalized finding classification.", "Filtering signal categories and analysis results.", "none"},
	"end_ns":                  {"End time in Unix nanoseconds.", "Calculating time ranges and ordering completed spans.", "none"},
	"status_message":          {"OpenTelemetry status detail supplied with the span.", "Investigating failed or cancelled operations.", "may contain user data"},
	"span_count":              {"Number of spans in the trace or capture session.", "Sizing traces and comparing capture volume.", "none"},
	"description":             {"Metric instrument description supplied by the emitter.", "Explaining unfamiliar metric names in a query result.", "may contain user data"},
	"unit":                    {"Unit reported for the metric value.", "Formatting values and selecting comparable metric streams.", "none"},
	"type":                    {"OpenTelemetry metric instrument type.", "Distinguishing gauges, counters, histograms, and summaries.", "none"},
	"aggregation_temporality": {"Whether metric points are cumulative or delta values.", "Interpreting counter and histogram changes correctly.", "none"},
	"is_monotonic":            {"Whether a sum is expected to increase monotonically.", "Choosing rate calculations for sum metrics.", "none"},
	"start_timestamp_ns":      {"Start of the metric aggregation interval, in Unix nanoseconds.", "Computing delta intervals and aggregation windows.", "none"},
	"flags":                   {"OTLP metric point flags.", "Filtering points with exporter-defined flags.", "none"},
	"histogram_count":         {"Number of observations in a histogram point.", "Computing histogram averages and validating distributions.", "none"},
	"histogram_sum":           {"Sum of observations in a histogram point.", "Computing histogram averages with histogram_count.", "none"},
	"histogram_min":           {"Smallest observation in a histogram point, when reported.", "Inspecting observed latency or value floors.", "none"},
	"histogram_max":           {"Largest observation in a histogram point, when reported.", "Inspecting observed latency or value ceilings.", "none"},
	"explicit_bounds":         {"Serialized explicit histogram bucket boundaries.", "Interpreting bucket_counts for explicit histograms.", "none"},
	"bucket_counts":           {"Serialized counts for explicit histogram buckets.", "Building percentile or distribution analyses.", "none"},
	"exp_scale":               {"Scale used by an exponential histogram.", "Interpreting exponential histogram bucket indexes.", "none"},
	"exp_zero_count":          {"Number of zero-valued exponential histogram observations.", "Accounting for the zero bucket in distributions.", "none"},
	"exp_zero_threshold":      {"Width of the exponential histogram zero bucket.", "Interpreting near-zero observations.", "none"},
	"exp_positive_offset":     {"Starting index for positive exponential histogram buckets.", "Decoding exp_positive_counts.", "none"},
	"exp_positive_counts":     {"Serialized counts for positive exponential histogram buckets.", "Analyzing positive exponential histogram distributions.", "none"},
	"exp_negative_offset":     {"Starting index for negative exponential histogram buckets.", "Decoding exp_negative_counts.", "none"},
	"exp_negative_counts":     {"Serialized counts for negative exponential histogram buckets.", "Analyzing negative exponential histogram distributions.", "none"},
	"summary_count":           {"Number of observations summarized by a summary point.", "Weighting summary values and validating summary streams.", "none"},
	"summary_sum":             {"Sum of observations summarized by a summary point.", "Computing averages with summary_count.", "none"},
	"summary_quantiles":       {"Serialized quantile values from a summary point.", "Reading application-reported quantile estimates.", "none"},
	"series_attributes":       {"Canonical serialized attributes that identify a metric series.", "Filtering or labeling a bounded metric stream.", "may contain user data"},
	"series_key":              {"Stable hash-like key for the bounded metric series.", "Joining metric points to telemetry_metric_series.", "identifier"},
	"first_timestamp_ns":      {"Earliest metric point time observed for the series, in Unix nanoseconds.", "Finding when a metric stream first appeared.", "none"},
	"last_timestamp_ns":       {"Most recent metric point time observed for the series, in Unix nanoseconds.", "Finding stale or recently active metric streams.", "none"},
	"point_count":             {"Number of metric points admitted for the series.", "Comparing stream volume and cardinality.", "none"},
	"scope_name":              {"Instrumentation scope name that emitted the metric.", "Separating library and application instrumentation.", "none"},
	"scope_version":           {"Instrumentation scope version that emitted the metric.", "Comparing telemetry across library versions.", "none"},
	"scope_schema_url":        {"Schema URL declared by the instrumentation scope.", "Understanding the semantic-convention schema in use.", "none"},
	"scope_attributes":        {"Serialized instrumentation scope attributes as JSON.", "Filtering library-specific instrumentation metadata.", "may contain user data"},
	"exemplars":               {"Serialized OTLP metric exemplars.", "Linking metric observations to trace context when available.", "may contain user data"},
	"time_ns":                 {"Event time in Unix nanoseconds.", "Ordering span events and building event timelines.", "none"},
	"linked_trace_id":         {"Trace identifier referenced by a span link.", "Following cross-trace and asynchronous relationships.", "identifier"},
	"linked_span_id":          {"Span identifier referenced by a span link.", "Following cross-trace and asynchronous relationships.", "identifier"},
	"trace_state":             {"W3C trace-state carried by the span link.", "Diagnosing propagated trace routing state.", "may contain user data"},
	"source":                  {"Spaniel subsystem that produced the finding.", "Filtering lint warnings versus trace-analysis findings.", "none"},
	"message":                 {"Human-readable finding message.", "Explaining the issue shown in a finding row.", "may contain user data"},
	"count":                   {"Number of occurrences represented by the finding.", "Prioritizing recurring trace or lint issues.", "none"},
	"wasted_ns":               {"Estimated wasted duration in nanoseconds.", "Ranking findings by potential latency impact.", "none"},
	"created_at":              {"Record creation time in Unix nanoseconds.", "Ordering findings and capture-session metadata.", "none"},
	"id":                      {"Capture session identifier.", "Selecting a baseline or comparison session.", "identifier"},
	"label":                   {"Human-readable capture session label.", "Presenting or selecting sessions in comparisons.", "none"},
	"is_baseline":             {"Whether this session is marked as a comparison baseline.", "Selecting the default baseline for session diffs.", "none"},
	"is_imported":             {"Whether the session originated from an import.", "Distinguishing imported captures from live collection.", "none"},
	"services":                {"Serialized list of services observed in the capture session.", "Understanding session coverage before comparing it.", "none"},
	"note":                    {"Operator note attached to the capture session.", "Adding context to a session comparison.", "may contain user data"},
	"last_activity_ns":        {"Most recent session activity time in Unix nanoseconds.", "Finding active sessions and ordering recent captures.", "none"},
}

func samplesFor(view string) []SchemaSample {
	switch view {
	case "telemetry_spans":
		return []SchemaSample{{"span-count-over-time", "Span count over time", "time_series", "SELECT (start_ns // 60000000000) * 60000000000 AS timestamp_ns, count(*) AS value FROM telemetry_spans WHERE session_id = $session_id GROUP BY 1 ORDER BY 1", "A one-minute request volume series scoped to the active session."}, {"slow-spans", "Slow spans", "table", "SELECT service_name, name, duration_ns / 1000000.0 AS duration_ms, trace_id, span_id FROM telemetry_spans WHERE session_id = $session_id ORDER BY duration_ns DESC LIMIT 30", "The slowest spans with links back to their trace."}}
	case "telemetry_traces":
		return []SchemaSample{{"errored-traces", "Recent traces", "trace_list", "SELECT trace_id, service_name, name, start_ns, duration_ns, span_count FROM telemetry_traces WHERE session_id = $session_id ORDER BY start_ns DESC LIMIT 30", "A trace-list starting point."}}
	case "telemetry_logs":
		return []SchemaSample{{"log-volume", "Log volume by severity", "time_series", "SELECT (timestamp_ns // 60000000000) * 60000000000 AS timestamp_ns, severity AS group_value, count(*) AS value FROM telemetry_logs WHERE session_id = $session_id GROUP BY 1, 2 ORDER BY 1, 2", "A grouped log volume chart."}}
	case "telemetry_metrics":
		return []SchemaSample{{"metric-values", "Metric values over time", "time_series", "SELECT timestamp_ns, avg(value) AS value FROM telemetry_metrics WHERE session_id = $session_id GROUP BY 1 ORDER BY 1", "A generic metric starting point; add a metric-name filter before saving."}}
	}
	return nil
}

// GenerateSchemaCatalog introspects only the public allowlist and fails when
// reviewed metadata no longer matches the migrated database.
func GenerateSchemaCatalog(path string) (*SchemaCatalog, error) {
	db, err := Open(path)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	catalog := &SchemaCatalog{Version: "1", Parameters: []string{"$session_id (resolved to the active session by Spaniel)"}}
	for _, name := range []string{"telemetry_spans", "telemetry_traces", "telemetry_logs", "telemetry_metrics", "telemetry_span_events", "telemetry_span_links", "telemetry_metric_series", "telemetry_findings", "telemetry_sessions"} {
		var exists int64
		if err := db.gorm.Raw("SELECT COUNT(*) FROM information_schema.views WHERE table_schema = 'main' AND table_name = ?", name).Scan(&exists).Error; err != nil || exists != 1 {
			return nil, fmt.Errorf("public view %s missing: %w", name, err)
		}
		rows, err := db.gorm.Raw("SELECT column_name, data_type FROM information_schema.columns WHERE table_schema = 'main' AND table_name = ? ORDER BY ordinal_position", name).Rows()
		if err != nil {
			return nil, err
		}
		view := SchemaView{Name: name, Purpose: schemaViewMetadata[name].Purpose, Samples: samplesFor(name)}
		if view.Samples == nil {
			view.Samples = []SchemaSample{}
		}
		for rows.Next() {
			var n, typ string
			if err := rows.Scan(&n, &typ); err != nil {
				rows.Close()
				return nil, err
			}
			h := columnHints[n]
			if h.description == "" || h.use == "" {
				rows.Close()
				return nil, fmt.Errorf("public view %s column %s is missing reviewed metadata", name, n)
			}
			view.Columns = append(view.Columns, SchemaColumn{n, typ, h.description, h.use, h.sensitivity})
		}
		rows.Close()
		if len(view.Columns) == 0 {
			return nil, fmt.Errorf("public view %s has no columns", name)
		}
		catalog.Views = append(catalog.Views, view)
	}
	parts := []string{catalog.Version}
	for _, v := range catalog.Views {
		for _, c := range v.Columns {
			parts = append(parts, v.Name+":"+c.Name+":"+c.Type)
		}
	}
	sort.Strings(parts)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	catalog.Fingerprint = hex.EncodeToString(sum[:])
	return catalog, nil
}
