package mcp

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zfogg/spaniel/internal/coverage"
)

type GetServiceSummaryInput struct {
	Service   string    `json:"service" jsonschema:"exact service.name to summarize"`
	SessionID string    `json:"session_id,omitempty" jsonschema:"session to query; defaults to active"`
	FromNs    UnixNanos `json:"from_ns,omitempty" jsonschema:"inclusive span-start or signal timestamp lower bound as decimal unix ns"`
	ToNs      UnixNanos `json:"to_ns,omitempty" jsonschema:"inclusive upper bound as decimal unix ns"`
	Limit     int       `json:"limit,omitempty" jsonschema:"maximum operations and finding groups each; default 10, max 50"`
}

type ServiceOperationOut struct {
	Name       string  `json:"name"`
	Kind       string  `json:"kind"`
	Count      int64   `json:"count"`
	ErrorCount int64   `json:"error_count"`
	P95Ms      float64 `json:"p95_ms"`
}
type ServiceFindingOut struct {
	Source         string  `json:"source"`
	RuleID         string  `json:"rule_id"`
	Count          int64   `json:"count"`
	TraceCount     int64   `json:"trace_count"`
	WastedMs       float64 `json:"wasted_ms"`
	ExampleTraceID string  `json:"example_trace_id"`
}
type ServiceSummaryOutput struct {
	SessionID        string                     `json:"session_id"`
	Service          string                     `json:"service"`
	FromNs           string                     `json:"from_ns"`
	ToNs             string                     `json:"to_ns"`
	ObservedAtNs     string                     `json:"observed_at_ns"`
	SpanCount        int64                      `json:"span_count"`
	TraceCount       int64                      `json:"trace_count"`
	ErrorSpanCount   int64                      `json:"error_span_count"`
	SpanErrorRate    float64                    `json:"span_error_rate"`
	P50Ms            float64                    `json:"p50_ms"`
	P95Ms            float64                    `json:"p95_ms"`
	P99Ms            float64                    `json:"p99_ms"`
	LastSpanNs       string                     `json:"last_span_ns"`
	LogCount         int64                      `json:"log_count"`
	ErrorLogCount    int64                      `json:"error_log_count"`
	LastLogNs        string                     `json:"last_log_ns"`
	MetricPointCount int64                      `json:"metric_point_count"`
	LastMetricNs     string                     `json:"last_metric_ns"`
	Operations       []ServiceOperationOut      `json:"operations"`
	Findings         []ServiceFindingOut        `json:"findings"`
	Dependencies     []ServiceEdgeOut           `json:"dependencies"`
	SessionCoverage  []coverage.ServiceCoverage `json:"session_coverage"`
	Notes            []string                   `json:"notes"`
}

func (h *handler) getServiceSummary(ctx context.Context, _ *mcpsdk.CallToolRequest, in GetServiceSummaryInput) (*mcpsdk.CallToolResult, ServiceSummaryOutput, error) {
	out := ServiceSummaryOutput{}
	if in.Service == "" {
		return nil, out, fmt.Errorf("service is required")
	}
	from, to, err := parseTimeWindow(in.FromNs, in.ToNs)
	if err != nil {
		return nil, out, err
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}
	sessionID := h.resolveSession(in.SessionID)
	db := h.store.WithContext(ctx)
	data, err := db.GetServiceSummary(sessionID, in.Service, from, to, limit)
	if err != nil {
		return nil, out, err
	}
	overview := data.Overview
	out = ServiceSummaryOutput{SessionID: sessionID, Service: in.Service, FromNs: strconv.FormatInt(from, 10), ToNs: strconv.FormatInt(to, 10), ObservedAtNs: strconv.FormatInt(time.Now().UnixNano(), 10), SpanCount: overview.SpanCount, TraceCount: overview.TraceCount, ErrorSpanCount: overview.ErrorCount, P50Ms: nsToMs(overview.P50Ns), P95Ms: nsToMs(overview.P95Ns), P99Ms: nsToMs(overview.P99Ns), LastSpanNs: strconv.FormatInt(overview.LastSpanNs, 10), LastLogNs: "0", LastMetricNs: "0", Operations: []ServiceOperationOut{}, Findings: []ServiceFindingOut{}, Dependencies: []ServiceEdgeOut{}, SessionCoverage: []coverage.ServiceCoverage{}, Notes: []string{"Latency percentiles and error rate describe all service spans, not request throughput. Findings belong to their parent/example span (detectors) or offending span (lint). Coverage covers the entire session, independent of the time window. Operation and finding lists are bounded by limit; counts cover all matching data."}}
	if overview.SpanCount > 0 {
		out.SpanErrorRate = float64(overview.ErrorCount) / float64(overview.SpanCount)
	}
	for _, op := range data.Operations {
		out.Operations = append(out.Operations, ServiceOperationOut{op.Name, kindString(op.Kind), op.Count, op.ErrorCount, nsToMs(op.P95Ns)})
	}
	for _, f := range data.Findings {
		out.Findings = append(out.Findings, ServiceFindingOut{f.Source, f.RuleID, f.Count, f.TraceCount, nsToMs(f.WastedNs), f.ExampleTraceID})
	}
	for _, s := range data.Signals {
		if s.Signal == "logs" {
			out.LogCount, out.ErrorLogCount, out.LastLogNs = s.Count, s.ErrorCount, strconv.FormatInt(s.LastNs, 10)
		} else {
			out.MetricPointCount, out.LastMetricNs = s.Count, strconv.FormatInt(s.LastNs, 10)
		}
	}
	graph, err := db.GetServiceMapInWindow(sessionID, from, to)
	if err != nil {
		return nil, out, err
	}
	for _, e := range graph.Edges {
		if e.From == in.Service || e.To == in.Service {
			out.Dependencies = append(out.Dependencies, ServiceEdgeOut{From: e.From, To: e.To, CallCount: e.CallCount, AvgMs: nsToMs(e.AvgDurationNs), ErrorCount: e.ErrorCount})
		}
	}
	sort.Slice(out.Dependencies, func(i, j int) bool {
		a, b := out.Dependencies[i], out.Dependencies[j]
		if a.CallCount != b.CallCount {
			return a.CallCount > b.CallCount
		}
		if a.From != b.From {
			return a.From < b.From
		}
		return a.To < b.To
	})
	if len(out.Dependencies) > limit {
		out.Dependencies = out.Dependencies[:limit]
		out.Notes = append(out.Notes, "Dependency list truncated to limit; get_service_map returns the complete graph.")
	}
	_, report, err := h.getCoverage(ctx, nil, GetCoverageInput{SessionID: sessionID})
	if err != nil {
		return nil, out, err
	}
	for _, s := range report.Report.Services {
		if s.Name == in.Service {
			if len(s.DarkRoutes) > limit {
				s.DarkRoutes = s.DarkRoutes[:limit]
				out.Notes = append(out.Notes, "Dark routes truncated to limit; get_coverage returns the complete list.")
			}
			if len(s.ObservedRoutes) > limit {
				s.ObservedRoutes = s.ObservedRoutes[:limit]
			}
			out.SessionCoverage = append(out.SessionCoverage, s)
		}
	}
	return nil, out, nil
}

func parseTimeWindow(from, to UnixNanos) (int64, int64, error) {
	f, err := from.Int64("from_ns")
	if err != nil {
		return 0, 0, err
	}
	t, err := to.Int64("to_ns")
	if err != nil {
		return 0, 0, err
	}
	if f < 0 || t < 0 || (t > 0 && f > t) {
		return 0, 0, fmt.Errorf("invalid time window: require nonnegative bounds and from_ns <= to_ns")
	}
	return f, t, nil
}
