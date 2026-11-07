package storage

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/zfogg/spaniel/internal/querydsl"
	"gorm.io/gen/field"
)

// SignalSearch is a bounded search over one signal within a session.
type SignalSearch struct {
	Signal, SessionID string
	Expr              *querydsl.Node
	Limit, Page       int
	LogFilter         *LogFilter
}
type SignalSearchResult struct {
	Rows  any
	Total int64
}

// SearchSignals uses generated DAOs and a parameterized predicate compiled from
// a validated expression tree; neither field names nor literals become raw SQL.
func (d *DB) SearchSignals(f SignalSearch) (SignalSearchResult, error) {
	if f.SessionID == "" {
		return SignalSearchResult{}, fmt.Errorf("a session is required")
	}
	if f.Limit <= 0 {
		f.Limit = 100
	}
	if f.Limit > 1000 {
		f.Limit = 1000
	}
	if f.Page <= 0 {
		f.Page = 1
	}
	if f.Page > 1000000 {
		return SignalSearchResult{}, fmt.Errorf("page too large")
	}
	expr, args, err := compileSignalExpr(f.Signal, f.Expr)
	if err != nil {
		return SignalSearchResult{}, err
	}
	if f.Signal == "traces" {
		expr = "EXISTS (SELECT 1 FROM spans AS matched WHERE matched.session_id = spans.session_id AND matched.trace_id = spans.trace_id AND (" + expr + "))"
	}
	predicate := field.NewUnsafeFieldRaw(expr, args...)
	switch f.Signal {
	case "logs":
		q := d.query.Log.Where(d.query.Log.SessionID.Eq(f.SessionID), predicate)
		if v := f.LogFilter; v != nil {
			if v.Service != "" {
				q = q.Where(d.query.Log.ServiceName.Eq(v.Service))
			}
			if v.TraceID != "" {
				q = q.Where(d.query.Log.TraceID.Eq(v.TraceID))
			}
			if v.SpanID != "" {
				q = q.Where(d.query.Log.SpanID.Eq(v.SpanID))
			}
			if v.MinSeverity > 0 {
				q = q.Where(d.query.Log.Severity.Gte(v.MinSeverity))
			}
			if v.MaxSeverity > 0 {
				q = q.Where(d.query.Log.Severity.Lte(v.MaxSeverity))
			}
			if v.FromNs > 0 {
				q = q.Where(d.query.Log.TimestampNs.Gte(v.FromNs))
			}
			if v.ToNs > 0 {
				q = q.Where(d.query.Log.TimestampNs.Lte(v.ToNs))
			}
		}
		total, err := q.Count()
		if err != nil {
			return SignalSearchResult{}, err
		}
		rows, err := q.Order(d.query.Log.TimestampNs.Desc()).Limit(f.Limit).Offset((f.Page - 1) * f.Limit).Find()
		return SignalSearchResult{Rows: rows, Total: total}, err
	case "spans", "traces":
		q := d.query.Span.Where(d.query.Span.SessionID.Eq(f.SessionID), predicate)
		if f.Signal == "traces" {
			q = q.Where(field.NewUnsafeFieldRaw("(parent_span_id = '' OR parent_span_id IS NULL)"))
		}
		total, err := q.Count()
		if err != nil {
			return SignalSearchResult{}, err
		}
		rows, err := q.Order(d.query.Span.StartNs.Desc()).Limit(f.Limit).Offset((f.Page - 1) * f.Limit).Find()
		return SignalSearchResult{Rows: rows, Total: total}, err
	case "metrics":
		q := d.query.Metric.Where(d.query.Metric.SessionID.Eq(f.SessionID), predicate)
		total, err := q.Count()
		if err != nil {
			return SignalSearchResult{}, err
		}
		rows, err := q.Order(d.query.Metric.TimestampNs.Desc()).Limit(f.Limit).Offset((f.Page - 1) * f.Limit).Find()
		return SignalSearchResult{Rows: rows, Total: total}, err
	default:
		return SignalSearchResult{}, fmt.Errorf("unknown signal %q", f.Signal)
	}
}

func compileSignalExpr(signal string, n *querydsl.Node) (string, []any, error) {
	fields := map[string]string{"service": "service_name", "service_name": "service_name", "trace_id": "trace_id", "span_id": "span_id", "name": "name", "body": "body", "severity": "severity", "status": "status_code", "status_code": "status_code", "duration_ns": "duration_ns", "timestamp_ns": "timestamp_ns", "start_ns": "start_ns", "type": "type", "value": "value", "unit": "unit"}
	allowed := map[string]map[string]bool{
		"logs":    {"service_name": true, "trace_id": true, "span_id": true, "body": true, "severity": true, "timestamp_ns": true},
		"spans":   {"service_name": true, "trace_id": true, "span_id": true, "name": true, "status_code": true, "duration_ns": true, "start_ns": true},
		"traces":  {"service_name": true, "trace_id": true, "span_id": true, "name": true, "status_code": true, "duration_ns": true, "start_ns": true},
		"metrics": {"service_name": true, "name": true, "type": true, "value": true, "unit": true, "timestamp_ns": true},
	}
	if _, ok := allowed[signal]; !ok {
		return "", nil, fmt.Errorf("unknown signal %q", signal)
	}
	var compile func(*querydsl.Node) (string, []any, error)
	compile = func(n *querydsl.Node) (string, []any, error) {
		if n == nil {
			return "TRUE", nil, nil
		}
		switch n.Op {
		case "and", "or":
			l, la, e := compile(n.Left)
			if e != nil {
				return "", nil, e
			}
			r, ra, e := compile(n.Right)
			if e != nil {
				return "", nil, e
			}
			op := " AND "
			if n.Op == "or" {
				op = " OR "
			}
			return "(" + l + op + r + ")", append(la, ra...), nil
		case "not":
			p, a, e := compile(n.Left)
			return "(NOT (" + p + "))", a, e
		case "text":
			cols := []string{"service_name"}
			switch signal {
			case "logs":
				cols = append(cols, "body")
			case "spans", "traces", "metrics":
				cols = append(cols, "name")
			}
			parts := []string{}
			args := []any{}
			for _, col := range cols {
				parts = append(parts, "contains(lower(COALESCE("+col+", '')), lower(?))")
				args = append(args, n.Value)
			}
			return "(" + strings.Join(parts, " OR ") + ")", args, nil
		}
		var col string
		var args []any
		if strings.HasPrefix(n.Field, "attributes.") || strings.HasPrefix(n.Field, "resource.") || strings.HasPrefix(n.Field, "resource_attributes.") {
			parts := strings.SplitN(n.Field, ".", 2)
			if parts[1] == "" {
				return "", nil, fmt.Errorf("attribute key is empty")
			}
			if parts[0] == "resource_attributes" {
				parts[0] = "resource"
			}
			encoded, _ := json.Marshal(parts[1])
			col = "json_extract_string(TRY_CAST(" + parts[0] + " AS JSON), ?)"
			args = append(args, "$."+string(encoded))
		} else {
			col = fields[n.Field]
			if col == "" || !allowed[signal][col] {
				return "", nil, fmt.Errorf("unknown %s field %q", signal, n.Field)
			}
		}
		if n.Op == ":" {
			return "COALESCE(contains(lower(CAST(" + col + " AS VARCHAR)), lower(?)), false)", append(args, n.Value), nil
		}
		if n.Op != "=" && n.Op != "!=" && n.Op != "<" && n.Op != ">" && n.Op != "<=" && n.Op != ">=" {
			return "", nil, fmt.Errorf("invalid operator")
		}
		if n.Op == "=" || n.Op == "!=" {
			return "COALESCE(CAST(" + col + " AS VARCHAR) " + n.Op + " ?, false)", append(args, n.Value), nil
		}
		if col == "timestamp_ns" || col == "start_ns" || col == "duration_ns" || col == "severity" || col == "status_code" {
			number, err := strconv.ParseInt(n.Value, 10, 64)
			if err != nil {
				return "", nil, fmt.Errorf("%s expects an integer", n.Field)
			}
			return col + " " + n.Op + " ?", append(args, number), nil
		}
		return "TRY_CAST(" + col + " AS DOUBLE) " + n.Op + " TRY_CAST(? AS DOUBLE)", append(args, n.Value), nil
	}
	return compile(n)
}
