// Package querydsl implements Spaniel's safe, deliberately constrained query DSL.
package querydsl

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const MaxGroups = 100

// MagicVariables is the single canonical registry for context supplied query
// variables. Callers should use this instead of carrying their own lists.
var MagicVariables = map[string]bool{"service_name": true, "environment": true, "window": true, "operation_name": true, "status": true, "region": true, "deployment_version": true, "attribute": true, "selected_trace_id": true, "selected_span_id": true, "selected_log_id": true}

func IsMagicVariable(name string) bool { return MagicVariables[strings.TrimPrefix(name, "$")] }

type Query struct {
	Function string   `json:"function"`
	Signal   string   `json:"signal"`
	Target   string   `json:"target"`
	GroupBy  string   `json:"group_by,omitempty"`
	Filters  []Filter `json:"filters,omitempty"`
}
type Filter struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value string `json:"value"`
}
type Compiled struct {
	SQL     string
	Args    []any
	Columns []string
}

var base = regexp.MustCompile(`(?i)^\s*(?:(count|rate|last|sum|avg|p50|p95|p99|heatmap)\s*\(\s*([^)]*)\s*\)|(logs|traces|spans))\s*(?:by\s+([a-zA-Z0-9_.]+))?\s*(?:where\s+(.+))?\s*$`)
var filt = regexp.MustCompile(`(?i)^\s*([a-zA-Z0-9_.]+)\s*(=|>=|<=|>|<|contains)\s*(?:"([^"]*)"|(\$[a-zA-Z0-9_]+)|([a-zA-Z0-9_.-]+))\s*$`)

func Parse(text string) (Query, error) { return ParseWithVariables(text, nil) }

// ParseWithVariables accepts dashboard-defined variable names in addition to
// fixed magic variables. Values are still compiled as SQL parameters.
func ParseWithVariables(text string, custom map[string]bool) (Query, error) {
	m := base.FindStringSubmatch(text)
	if m == nil {
		return Query{}, fmt.Errorf("use a supported telemetry query")
	}
	q := Query{Function: strings.ToLower(m[1]), Target: strings.TrimSpace(m[2]), GroupBy: m[4], Signal: strings.ToLower(m[3])}
	if q.Signal == "" {
		q.Signal = "spans"
		if strings.EqualFold(q.Target, "traces") {
			q.Signal = "traces"
		}
		if strings.Contains(strings.ToLower(q.Target), "log") {
			q.Signal = "logs"
		}
		if strings.Contains(strings.ToLower(q.Target), "metric") {
			q.Signal = "metrics"
		}
	}
	if q.Function == "" {
		q.Function = "records"
	}
	if !field(q.GroupBy) && q.GroupBy != "" {
		return q, fmt.Errorf("grouping is not allowed")
	}
	if m[5] != "" {
		for _, part := range regexp.MustCompile(`(?i)\s+and\s+`).Split(m[5], -1) {
			x := filt.FindStringSubmatch(part)
			if x == nil || !field(x[1]) {
				return q, fmt.Errorf("invalid filter")
			}
			v := x[3]
			if v == "" {
				v = x[4]
			}
			if v == "" {
				v = x[5]
			}
			if strings.HasPrefix(v, "$") && !IsMagicVariable(v) && !custom[strings.TrimPrefix(v, "$")] {
				return q, fmt.Errorf("unknown variable %s", v)
			}
			q.Filters = append(q.Filters, Filter{x[1], strings.ToLower(x[2]), v})
		}
	}
	return q, nil
}
func (q Query) JSON() string { b, _ := json.Marshal(q); return string(b) }
func field(x string) bool {
	if x == "" {
		return true
	}
	switch x {
	case "service", "service_name", "name", "duration", "status", "severity", "body", "trace_id", "span_id", "value":
		return true
	}
	return regexp.MustCompile(`^(attributes|resource)\.[A-Za-z][A-Za-z0-9_.-]{0,80}$`).MatchString(x)
}
func Compile(q Query, vars map[string]string, session string) (Compiled, error) {
	table, timeCol := "spans", "start_ns"
	if q.Signal == "logs" {
		table, timeCol = "logs", "timestamp_ns"
	}
	if q.Signal == "metrics" {
		table, timeCol = "metrics", "timestamp_ns"
	}
	where := []string{" WHERE 1=1"}
	args := []any{}
	if session != "" {
		where = append(where, " AND session_id = ?")
		args = append(args, session)
	}
	// $window controls the actual telemetry interval independently of filters.
	// It is intentionally parsed as a duration, never interpolated into SQL.
	if window := vars["window"]; window != "" {
		d, err := time.ParseDuration(window)
		if err != nil || d <= 0 {
			return Compiled{}, fmt.Errorf("variable $window requires a positive duration such as 30m")
		}
		where = append(where, " AND "+timeCol+" >= ?")
		args = append(args, time.Now().Add(-d).UnixNano())
	}
	for _, f := range q.Filters {
		v := f.Value
		if strings.HasPrefix(v, "$") {
			var ok bool
			v, ok = vars[strings.TrimPrefix(v, "$")]
			if !ok || v == "" {
				return Compiled{}, fmt.Errorf("variable %s has no value", f.Value)
			}
		}
		e := expr(f.Field, table)
		if f.Op == "contains" {
			where = append(where, " AND lower("+e+") LIKE lower(?)")
			args = append(args, "%"+v+"%")
		} else {
			where = append(where, " AND "+e+" "+f.Op+" ?")
			args = append(args, v)
		}
	}
	if q.Signal == "traces" {
		where = append(where, " AND (parent_span_id = '' OR parent_span_id IS NULL)")
	}
	if q.Function == "records" {
		cols := "trace_id, span_id, service_name, name, start_ns, duration_ns, status_code"
		if table == "logs" {
			cols = "timestamp_ns, trace_id, span_id, severity, body, service_name"
		}
		return Compiled{"SELECT " + cols + " FROM " + table + strings.Join(where, "") + " ORDER BY " + timeCol + " DESC LIMIT 100", args, strings.Split(cols, ", ")}, nil
	}
	v := expr(q.Target, table)
	a := map[string]string{"count": "count(*)", "rate": "count(*) / greatest((max(" + timeCol + ") - min(" + timeCol + ")) / 1000000000.0, 1)", "last": "max(" + v + ")", "sum": "sum(" + v + ")", "avg": "avg(" + v + ")", "p50": "quantile_cont(" + v + ", 0.5)", "p95": "quantile_cont(" + v + ", 0.95)", "p99": "quantile_cont(" + v + ", 0.99)", "heatmap": "count(*)"}[q.Function]
	if a == "" {
		return Compiled{}, fmt.Errorf("unsupported aggregate")
	}
	sel, group, cols := a+" AS value", "", []string{"value"}
	if q.Function == "heatmap" {
		// Log-scaled numeric buckets work for latency and metric magnitudes
		// without relying on a backend-specific histogram extension.
		bucket := "floor(log10(greatest(cast(" + v + " as double), 1)))"
		sel = bucket + " AS group_value, " + a + " AS value"
		group = " GROUP BY " + bucket
		cols = []string{"group_value", "value"}
	} else if q.GroupBy != "" {
		g := expr(q.GroupBy, table)
		sel = g + " AS group_value, " + sel
		group = " GROUP BY " + g
		cols = []string{"group_value", "value"}
	}
	return Compiled{"SELECT " + sel + " FROM " + table + strings.Join(where, "") + group + " ORDER BY value DESC LIMIT " + strconv.Itoa(MaxGroups), args, cols}, nil
}
func expr(f, table string) string {
	switch f {
	case "service", "service_name":
		return "service_name"
	case "name":
		return "name"
	case "duration":
		return "duration_ns"
	case "status":
		return "status_code"
	case "severity":
		return "severity"
	case "body":
		return "body"
	case "trace_id", "span_id", "value":
		return f
	}
	if strings.HasPrefix(f, "resource.") {
		return "json_extract_string(resource, '$." + strings.TrimPrefix(f, "resource.") + "')"
	}
	if strings.HasPrefix(f, "attributes.") {
		return "json_extract_string(attributes, '$." + strings.TrimPrefix(f, "attributes.") + "')"
	}
	if table == "metrics" {
		return "value"
	}
	return "duration_ns"
}
