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
	"trace_id":     {"OpenTelemetry trace identifier.", "Trace-list links and correlating logs with spans.", "identifier"},
	"span_id":      {"OpenTelemetry span identifier.", "Span-list links and parent-child investigation.", "identifier"},
	"service_name": {"Service that emitted the record.", "Grouping or filtering by service.", "none"},
	"name":         {"Operation, log, or metric name.", "Operation breakdowns and metric selection.", "none"},
	"start_ns":     {"Start time in Unix nanoseconds.", "Time-series buckets and ordering.", "none"},
	"timestamp_ns": {"Event time in Unix nanoseconds.", "Time-series buckets and ordering.", "none"},
	"duration_ns":  {"Duration in nanoseconds.", "Latency percentiles, slow-operation tables, and heatmaps.", "none"},
	"status_code":  {"OpenTelemetry status code.", "Error-rate filters and status summaries.", "none"},
	"attributes":   {"Serialized span or log attributes as JSON.", "Filtering known semantic-convention fields with json_extract_string.", "may contain user data"},
	"resource":     {"Serialized resource attributes as JSON.", "Filtering deployment and service resource fields.", "may contain user data"},
	"body":         {"Log body.", "Log search and log-list panels.", "may contain user data"},
	"severity":     {"OpenTelemetry log severity number.", "Severity breakdowns and alert filters.", "none"},
	"value":        {"Recorded numeric metric value.", "Metric charts and scalar values.", "none"},
	"session_id":   {"Spaniel capture session identifier.", "The automatic $session_id scope; normally do not hard-code it.", "identifier"},
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
		for rows.Next() {
			var n, typ string
			if err := rows.Scan(&n, &typ); err != nil {
				rows.Close()
				return nil, err
			}
			h := columnHints[n]
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
