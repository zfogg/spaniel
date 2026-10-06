package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// CatalogEntry is an editable, read-only SQL recipe, not a stored query DSL.
type CatalogEntry struct {
	Signal      string         `json:"signal"`
	Name        string         `json:"name"`
	Query       string         `json:"query"`
	DisplayType string         `json:"display_type"`
	Attributes  map[string]any `json:"attributes,omitempty"`
}

func catalogLiteral(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// Search predicates are shared by discovery and the generated SQL. contains
// treats %, _ and quotes literally, unlike LIKE. Columns are static, never input.
func catalogPredicate(signal, search, session string) string {
	fields := "service_name, name, trace_id, span_id, status_message, attributes, resource"
	switch signal {
	case "metrics":
		fields = "service_name, name, description, unit, type, attributes"
	case "logs":
		fields = "service_name, body, trace_id, span_id, cast(severity as varchar), attributes"
	}
	predicate := "TRUE"
	if session != "" {
		predicate = "session_id = " + catalogLiteral(session)
	}
	if search != "" {
		predicate += " AND contains(lower(concat_ws(' ', " + fields + ")), " + catalogLiteral(strings.ToLower(search)) + ")"
	}
	return predicate
}

func catalogRecipes(signal, predicate string) []CatalogEntry {
	from := " FROM spans WHERE " + predicate
	minute := "(start_ns // 60000000000) * 60000000000 AS timestamp_ns"
	if signal == "logs" {
		return []CatalogEntry{{signal, "Recent log records", "SELECT timestamp_ns, severity, body, service_name, trace_id, span_id FROM logs WHERE " + predicate + " ORDER BY timestamp_ns DESC LIMIT 100", "log_list", nil}}
	}
	if signal == "traces" {
		// Match any span in a trace (including child attributes), then return its
		// full duration and span count, rather than treating each span as a trace.
		return []CatalogEntry{{signal, "Recent traces", "WITH matched AS (SELECT DISTINCT session_id, trace_id FROM spans WHERE " + predicate + ") SELECT s.trace_id, arg_min(s.service_name, s.start_ns) AS service_name, arg_min(s.name, s.start_ns) AS name, min(s.start_ns) AS start_ns, max(s.end_ns) - min(s.start_ns) AS duration_ns, count(*) AS span_count FROM spans s JOIN matched m ON s.session_id = m.session_id AND s.trace_id = m.trace_id GROUP BY s.session_id, s.trace_id ORDER BY start_ns DESC LIMIT 100", "trace_list", nil}}
	}
	return []CatalogEntry{
		{signal, "Span count", "SELECT count(*) AS value" + from, "single_value", nil},
		{signal, "Request volume", "SELECT " + minute + ", count(*) AS value" + from + " AND kind = 2 GROUP BY 1 ORDER BY 1", "time_series", nil},
		{signal, "Recent spans", "SELECT trace_id, span_id, service_name, name, duration_ns, status_code" + from + " ORDER BY start_ns DESC LIMIT 100", "span_list", nil},
		{signal, "Slowest spans", "SELECT service_name, name, duration_ns / 1000000.0 AS duration_ms, status_code, trace_id, span_id" + from + " ORDER BY duration_ms DESC LIMIT 100", "table", nil},
		{signal, "Span duration distribution (10 ms buckets)", "SELECT floor(duration_ns / 10000000.0) * 10 AS x, count(*) AS value" + from + " GROUP BY 1 ORDER BY 1 LIMIT 1000", "heatmap", nil},
		{signal, "Operations by volume", "SELECT service_name || ' · ' || name AS label, count(*) AS primary_value, CASE WHEN bool_or(status_code = 2) THEN 'error' ELSE 'ok' END AS status" + from + " GROUP BY 1 ORDER BY primary_value DESC LIMIT 50", "entity_list", nil},
	}
}

// Additional operational examples complement the first eight recipes (one per
// renderer). These use collected data, not fabricated mockup measurements.
func catalogOperationalExamples(session string) []CatalogEntry {
	spans := " FROM spans WHERE " + catalogPredicate("spans", "", session)
	logs := " FROM logs WHERE " + catalogPredicate("logs", "", session)
	minute := "(start_ns // 60000000000) * 60000000000 AS timestamp_ns"
	traces := " FROM (SELECT session_id, trace_id, min(start_ns) AS start_ns, max(end_ns) - min(start_ns) AS duration_ns FROM spans WHERE " + catalogPredicate("spans", "", session) + " AND trace_id IS NOT NULL AND trace_id <> '' GROUP BY session_id, trace_id) AS traces"
	return []CatalogEntry{
		{Signal: "traces", Name: "Trace volume over time", DisplayType: "time_series", Query: "SELECT " + minute + ", count(*) AS value" + traces + " GROUP BY 1 ORDER BY 1"},
		{Signal: "traces", Name: "p95 end-to-end trace duration (ms)", DisplayType: "single_value", Query: "SELECT quantile_cont(duration_ns / 1000000.0, 0.95) AS value" + traces},
		{Signal: "spans", Name: "Successful spans (explicit OK)", DisplayType: "single_value", Query: "SELECT count(*) AS value" + spans + " AND status_code = 1"},
		{Signal: "spans", Name: "Error traces", DisplayType: "single_value", Query: "SELECT count(DISTINCT trace_id) AS value" + spans + " AND status_code = 2"},
		{Signal: "spans", Name: "Request error rate (%)", DisplayType: "single_value", Query: "SELECT coalesce(100.0 * count(*) FILTER (WHERE status_code = 2) / nullif(count(*), 0), 0) AS value" + spans + " AND kind = 2"},
		{Signal: "spans", Name: "p95 span duration (ms)", DisplayType: "time_series", Query: "SELECT " + minute + ", quantile_cont(duration_ns / 1000000.0, 0.95) AS value" + spans + " GROUP BY 1 ORDER BY 1"},
		{Signal: "spans", Name: "Errors over time by service", DisplayType: "time_series", Query: "SELECT " + minute + ", service_name AS group_value, count(*) AS value" + spans + " AND status_code = 2 GROUP BY 1,2 ORDER BY 1,2"},
		{Signal: "spans", Name: "Slow operations", DisplayType: "table", Query: "SELECT service_name, name, count(*) AS calls, avg(duration_ns / 1000000.0) AS mean_ms, max(duration_ns / 1000000.0) AS slowest_ms" + spans + " GROUP BY 1,2 ORDER BY mean_ms DESC LIMIT 50"},
		{Signal: "spans", Name: "Services by error count", DisplayType: "entity_list", Query: "SELECT service_name AS label, count(*) AS primary_value, 'error' AS status" + spans + " AND status_code = 2 GROUP BY 1 ORDER BY primary_value DESC LIMIT 50"},
		{Signal: "spans", Name: "Failed spans", DisplayType: "span_list", Query: "SELECT trace_id, span_id, service_name, name, duration_ns, status_message" + spans + " AND status_code = 2 ORDER BY start_ns DESC LIMIT 100"},
		{Signal: "logs", Name: "Warnings and errors", DisplayType: "log_list", Query: "SELECT timestamp_ns, severity, body, service_name, trace_id, span_id" + logs + " AND severity >= 13 ORDER BY timestamp_ns DESC LIMIT 100"},
		{Signal: "logs", Name: "Payment failures", DisplayType: "log_list", Query: "SELECT timestamp_ns, severity, body, service_name, trace_id, span_id" + logs + " AND severity >= 17 AND contains(lower(body), 'payment') ORDER BY timestamp_ns DESC LIMIT 100"},
		{Signal: "logs", Name: "Log volume by severity", DisplayType: "time_series", Query: "SELECT (timestamp_ns // 60000000000) * 60000000000 AS timestamp_ns, severity AS group_value, count(*) AS value" + logs + " GROUP BY 1,2 ORDER BY 1,2"},
	}
}

// QueryCatalog searches the full scoped telemetry before bounding the number
// of suggestions. Log bodies are not returned as catalog previews.
func (d *DB) QueryCatalog(ctx context.Context, signal, search, session string) (result []CatalogEntry, err error) {
	// Suggested titles must satisfy the panel-create API's 160-character limit.
	// SQL always retains the complete search string and dimensions.
	defer func() {
		for i := range result {
			if title := []rune(result[i].Name); len(title) > 160 {
				result[i].Name = string(title[:159]) + "…"
			}
		}
	}()
	items := []CatalogEntry{}
	for _, source := range []string{"spans", "traces", "logs"} {
		if signal != "" && signal != source {
			continue
		}
		for _, entry := range catalogRecipes(source, catalogPredicate(source, "", session)) {
			if search == "" || strings.Contains(strings.ToLower(entry.Name+" "+entry.DisplayType+" "+entry.Query), strings.ToLower(search)) {
				items = append(items, entry)
			}
		}
		if search == "" {
			continue
		}
		predicate := catalogPredicate(source, search, session)
		table := "spans"
		if source == "logs" {
			table = "logs"
		}
		var exists bool
		// Bypass the instrumented ORM: recording this SQL as a span would make
		// subsequent searches match their own earlier search text.
		if err := d.SQL().QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM "+table+" WHERE "+predicate+")").Scan(&exists); err != nil {
			return nil, fmt.Errorf("search %s: %w", source, err)
		}
		if exists {
			for _, entry := range catalogRecipes(source, predicate) {
				entry.Name += " matching “" + search + "”"
				items = append(items, entry)
			}
		}
	}
	for _, entry := range catalogOperationalExamples(session) {
		if (signal == "" || signal == entry.Signal) && (search == "" || strings.Contains(strings.ToLower(entry.Name+" "+entry.DisplayType+" "+entry.Query), strings.ToLower(search))) {
			items = append(items, entry)
		}
	}
	// Start with a varied gallery, not dozens of percentile/dimension variants.
	// Searching still discovers metrics, and signal=metrics explicitly browses them.
	if (search == "" && signal == "") || (signal != "" && signal != "metrics") {
		return items, nil
	}
	// Keep attribute sets separate: histogram percentiles, hosts and other
	// dimensions must never be averaged into one indistinguishable stream.
	type metricStream struct{ Name, ServiceName, Type, Unit, Attributes string }
	var streams []metricStream
	predicate := catalogPredicate("metrics", search, session)
	rows, err := d.SQL().QueryContext(ctx, "SELECT coalesce(name, ''), coalesce(service_name, ''), coalesce(type, ''), coalesce(unit, ''), coalesce(attributes, '{}') AS attributes FROM metrics WHERE "+predicate+" GROUP BY 1,2,3,4,5 ORDER BY max(timestamp_ns) DESC, 1,2,3,4,5 LIMIT 40")
	if err != nil {
		return nil, fmt.Errorf("search metrics: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var stream metricStream
		if err := rows.Scan(&stream.Name, &stream.ServiceName, &stream.Type, &stream.Unit, &stream.Attributes); err != nil {
			return nil, fmt.Errorf("read metric stream: %w", err)
		}
		streams = append(streams, stream)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search metrics: %w", err)
	}
	for _, stream := range streams {
		where := catalogPredicate("metrics", "", session) + " AND name = " + catalogLiteral(stream.Name) + " AND service_name = " + catalogLiteral(stream.ServiceName) + " AND type = " + catalogLiteral(stream.Type) + " AND unit = " + catalogLiteral(stream.Unit) + " AND coalesce(attributes, '{}') = " + catalogLiteral(stream.Attributes)
		label := stream.ServiceName + " · " + stream.Name + " (" + stream.Type + ", " + stream.Unit + ")"
		var dimensions map[string]any
		if json.Unmarshal([]byte(stream.Attributes), &dimensions) == nil {
			if percentile, ok := dimensions["percentile"]; ok {
				label += " · " + fmt.Sprint(percentile)
			}
		}
		from := " FROM metrics WHERE " + where
		// Temporality is not stored, so never invent a counter rate by subtracting
		// samples. Expose reported counter values explicitly instead.
		measure, suffix := "avg(value)", "mean over time"
		if stream.Type == "counter" {
			measure, suffix = "arg_max(value, timestamp_ns)", "reported counter over time (not a rate)"
		}
		if stream.Type == "histogram" {
			measure, suffix = "arg_max(value, timestamp_ns)", "reported percentile over time"
		}
		items = append(items,
			CatalogEntry{"metrics", label + " · latest reported value", "SELECT value, " + catalogLiteral(stream.Unit) + " AS unit" + from + " ORDER BY timestamp_ns DESC LIMIT 1", "single_value", dimensions},
			CatalogEntry{"metrics", label + " · " + suffix, "SELECT (timestamp_ns // 60000000000) * 60000000000 AS timestamp_ns, " + measure + " AS value" + from + " GROUP BY 1 ORDER BY 1", "time_series", dimensions})
	}
	return items, nil
}
