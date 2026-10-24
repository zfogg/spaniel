package mcp

import (
	"context"
	"fmt"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zfogg/spaniel/internal/storage"
)

// registerTopologyTools adds the service-map, services, stats, and metrics read
// tools (issue #101).
func (h *handler) registerTopologyTools(s *mcpsdk.Server) {
	readOnly := &mcpsdk.ToolAnnotations{ReadOnlyHint: true}

	addTOONTool(s, &mcpsdk.Tool{
		Name:        "get_service_map",
		Description: "Get the service dependency graph for a session, optionally limited to a span-start time window or service allow-list: nodes include span/error counts and p95 latency; edges include call counts, average duration, and errors. Defaults to the active session.",
		Annotations: readOnly,
	}, h.getServiceMap)

	addTOONTool(s, &mcpsdk.Tool{
		Name:        "list_services",
		Description: "List all service.name values observed across stored telemetry.",
		Annotations: readOnly,
	}, h.listServices)

	addTOONTool(s, &mcpsdk.Tool{
		Name:        "get_stats",
		Description: "Get counts and storage stats for a session (spans, traces, logs, sessions, DB size, and dropped-signal counters). Defaults to the active session.",
		Annotations: readOnly,
	}, h.getStats)

	addTOONTool(s, &mcpsdk.Tool{
		Name:        "get_metrics",
		Description: "List the metric catalog for a session, filterable by service, name, and type. Each entry has its type, unit, description, service, and sample count. Use get_metric_series for actual data points. Defaults to the active session.",
		Annotations: readOnly,
	}, h.getMetrics)

	addTOONTool(s, &mcpsdk.Tool{
		Name:        "get_metric_series",
		Description: "Get the time-series data points for one metric (by name), optionally narrowed by service and a [from_ns, to_ns] time window. Points include value, attributes, and exemplar links to traces. Defaults to the active session.",
		Annotations: readOnly,
	}, h.getMetricSeries)
}

// ---- get_service_map ----

type GetServiceMapInput struct {
	SessionID string    `json:"session_id,omitempty" jsonschema:"session to query; defaults to the active session"`
	Services  []string  `json:"services,omitempty" jsonschema:"optional service.name allow-list; returned edges require both endpoints"`
	FromNs    UnixNanos `json:"from_ns,omitempty" jsonschema:"inclusive span-start lower bound as a decimal unix-ns string; omit for no bound"`
	ToNs      UnixNanos `json:"to_ns,omitempty" jsonschema:"inclusive span-start upper bound as a decimal unix-ns string; omit for no bound"`
}

type ServiceNodeOut struct {
	Service    string  `json:"service"`
	SpanCount  int     `json:"span_count"`
	ErrorCount int     `json:"error_count"`
	P95Ms      float64 `json:"p95_ms"`
}

type ServiceEdgeOut struct {
	From       string  `json:"from"`
	To         string  `json:"to"`
	CallCount  int     `json:"call_count"`
	AvgMs      float64 `json:"avg_ms"`
	ErrorCount int     `json:"error_count"`
}

type ServiceMapOutput struct {
	SessionID string           `json:"session_id"`
	Nodes     []ServiceNodeOut `json:"nodes"`
	Edges     []ServiceEdgeOut `json:"edges"`
}

func (h *handler) getServiceMap(ctx context.Context, _ *mcpsdk.CallToolRequest, in GetServiceMapInput) (*mcpsdk.CallToolResult, ServiceMapOutput, error) {
	sessionID := h.resolveSession(in.SessionID)
	fromNs, err := in.FromNs.Int64("from_ns")
	if err != nil {
		return nil, ServiceMapOutput{}, err
	}
	toNs, err := in.ToNs.Int64("to_ns")
	if err != nil {
		return nil, ServiceMapOutput{}, err
	}
	data, err := h.store.WithContext(ctx).GetServiceMapInWindow(sessionID, fromNs, toNs)
	if err != nil {
		return nil, ServiceMapOutput{}, fmt.Errorf("get service map: %w", err)
	}
	out := ServiceMapOutput{SessionID: sessionID, Nodes: []ServiceNodeOut{}, Edges: []ServiceEdgeOut{}}
	allowed := map[string]bool{}
	for _, service := range in.Services {
		allowed[service] = true
	}
	if data != nil {
		for _, n := range data.Nodes {
			if len(allowed) > 0 && !allowed[n.ID] {
				continue
			}
			out.Nodes = append(out.Nodes, ServiceNodeOut{
				Service: n.ID, SpanCount: n.SpanCount, ErrorCount: n.ErrorCount, P95Ms: nsToMs(n.P95Ns),
			})
		}
		for _, e := range data.Edges {
			if len(allowed) > 0 && (!allowed[e.From] || !allowed[e.To]) {
				continue
			}
			out.Edges = append(out.Edges, ServiceEdgeOut{
				From: e.From, To: e.To, CallCount: e.CallCount, AvgMs: nsToMs(e.AvgDurationNs), ErrorCount: e.ErrorCount,
			})
		}
	}
	return nil, out, nil
}

// ---- list_services ----

type ListServicesInput struct {
	SessionID string `json:"session_id,omitempty" jsonschema:"session to query; defaults to active"`
	Signal    string `json:"signal,omitempty" jsonschema:"spans, traces, logs, or metrics; defaults to spans"`
	Search    string `json:"search,omitempty" jsonschema:"case-insensitive service.name substring"`
	Limit     int    `json:"limit,omitempty" jsonschema:"max services to return (default 100, max 500)"`
	Page      int    `json:"page,omitempty" jsonschema:"1-based page number (default 1)"`
}

type ListServicesOutput struct {
	Count    int      `json:"count"`
	Services []string `json:"services"`
}

func (h *handler) listServices(ctx context.Context, _ *mcpsdk.CallToolRequest, in ListServicesInput) (*mcpsdk.CallToolResult, ListServicesOutput, error) {
	limit, page := in.Limit, in.Page
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	if page < 1 {
		page = 1
	}
	services, err := h.store.WithContext(ctx).ListSignalServices(h.resolveSession(in.SessionID), in.Signal)
	if err != nil {
		return nil, ListServicesOutput{}, fmt.Errorf("list services: %w", err)
	}
	if services == nil {
		services = []string{}
	}
	filtered := make([]string, 0, len(services))
	needle := strings.ToLower(in.Search)
	for _, service := range services {
		if needle == "" || strings.Contains(strings.ToLower(service), needle) {
			filtered = append(filtered, service)
		}
	}
	start := min((page-1)*limit, len(filtered))
	end := min(start+limit, len(filtered))
	return nil, ListServicesOutput{Count: end - start, Services: filtered[start:end]}, nil
}

// ---- get_stats ----

type GetStatsInput struct {
	SessionID string `json:"session_id,omitempty" jsonschema:"session to query; defaults to the active session"`
}

type StatsOutput struct {
	SessionID           string `json:"session_id"`
	SpanCount           int    `json:"span_count"`
	TraceCount          int    `json:"trace_count"`
	LogCount            int    `json:"log_count"`
	SessionCount        int    `json:"session_count"`
	DBSizeBytes         int64  `json:"db_size_bytes"`
	DroppedSpans        int64  `json:"dropped_spans"`
	DroppedLogs         int64  `json:"dropped_logs"`
	DroppedMetricPoints int64  `json:"dropped_metric_points"`
}

func (h *handler) getStats(ctx context.Context, _ *mcpsdk.CallToolRequest, in GetStatsInput) (*mcpsdk.CallToolResult, StatsOutput, error) {
	sessionID := h.resolveSession(in.SessionID)
	stats, err := h.store.WithContext(ctx).GetStats(sessionID)
	if err != nil {
		return nil, StatsOutput{}, fmt.Errorf("get stats: %w", err)
	}
	return nil, StatsOutput{
		SessionID:           sessionID,
		SpanCount:           stats.SpanCount,
		TraceCount:          stats.TraceCount,
		LogCount:            stats.LogCount,
		SessionCount:        stats.SessionCount,
		DBSizeBytes:         stats.DBSize,
		DroppedSpans:        stats.DroppedSpans,
		DroppedLogs:         stats.DroppedLogs,
		DroppedMetricPoints: stats.DroppedMetricPoints,
	}, nil
}

// ---- get_metrics ----

type GetMetricsInput struct {
	SessionID string `json:"session_id,omitempty" jsonschema:"session to query; defaults to the active session"`
	Service   string `json:"service,omitempty" jsonschema:"only metrics from this service.name"`
	Name      string `json:"name,omitempty" jsonschema:"case-insensitive metric-name substring"`
	Type      string `json:"type,omitempty" jsonschema:"only this metric type: gauge, counter, histogram, or summary"`
	Limit     int    `json:"limit,omitempty" jsonschema:"max catalog entries to return (default 100, max 500)"`
	Page      int    `json:"page,omitempty" jsonschema:"1-based page number (default 1)"`
}

type MetricCatalogOut struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Unit        string `json:"unit,omitempty"`
	Description string `json:"description,omitempty"`
	Service     string `json:"service,omitempty"`
	SampleCount int    `json:"sample_count"`
}

type GetMetricsOutput struct {
	SessionID string             `json:"session_id"`
	Count     int                `json:"count"`
	Metrics   []MetricCatalogOut `json:"metrics"`
}

func (h *handler) getMetrics(ctx context.Context, _ *mcpsdk.CallToolRequest, in GetMetricsInput) (*mcpsdk.CallToolResult, GetMetricsOutput, error) {
	sessionID := h.resolveSession(in.SessionID)
	limit := in.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	page := in.Page
	if page < 1 {
		page = 1
	}
	entries, err := h.store.WithContext(ctx).ListMetricCatalog(sessionID)
	if err != nil {
		return nil, GetMetricsOutput{}, fmt.Errorf("get metrics: %w", err)
	}
	out := GetMetricsOutput{SessionID: sessionID, Metrics: []MetricCatalogOut{}}
	start := (page - 1) * limit
	matched := 0
	for _, e := range entries {
		if in.Service != "" && e.ServiceName != in.Service || in.Type != "" && e.Type != in.Type || in.Name != "" && !strings.Contains(strings.ToLower(e.Name), strings.ToLower(in.Name)) {
			continue
		}
		if matched < start {
			matched++
			continue
		}
		matched++
		out.Metrics = append(out.Metrics, MetricCatalogOut{
			Name: e.Name, Type: e.Type, Unit: e.Unit, Description: e.Description,
			Service: e.ServiceName, SampleCount: e.SampleCount,
		})
		if len(out.Metrics) >= limit {
			break
		}
	}
	out.Count = len(out.Metrics)
	return nil, out, nil
}

// ---- get_metric_series ----

type GetMetricSeriesInput struct {
	Attributes         map[string]string `json:"attributes,omitempty" jsonschema:"exact top-level attribute key/value filters; AND; scalar values compared as text; applied before pagination"`
	ResourceAttributes map[string]string `json:"resource_attributes,omitempty" jsonschema:"exact resource attribute key/value filters; AND; trace filters match any one span in the trace"`

	Name           string `json:"name" jsonschema:"metric name (from get_metrics)"`
	Service        string `json:"service,omitempty" jsonschema:"narrow to one service"`
	SessionID      string `json:"session_id,omitempty" jsonschema:"session to query; defaults to the active session"`
	FromNs         int64  `json:"from_ns,omitempty" jsonschema:"inclusive lower bound (unix ns); 0 = no bound"`
	ToNs           int64  `json:"to_ns,omitempty" jsonschema:"inclusive upper bound (unix ns); 0 = no bound"`
	Limit          int    `json:"limit,omitempty" jsonschema:"max points to return (default 500)"`
	AttributeKey   string `json:"attribute_key,omitempty" jsonschema:"only points containing this attribute key"`
	AttributeValue string `json:"attribute_value,omitempty" jsonschema:"optional exact value for attribute_key"`
}

type MetricPointOut struct {
	TimestampNs int64             `json:"timestamp_ns"`
	Value       float64           `json:"value"`
	Service     string            `json:"service,omitempty"`
	Attributes  map[string]string `json:"attributes,omitempty"`
	Exemplars   string            `json:"exemplars,omitempty"`
}

type GetMetricSeriesOutput struct {
	SessionID string           `json:"session_id"`
	Name      string           `json:"name"`
	Count     int              `json:"count"`
	Points    []MetricPointOut `json:"points"`
}

func (h *handler) getMetricSeries(ctx context.Context, _ *mcpsdk.CallToolRequest, in GetMetricSeriesInput) (*mcpsdk.CallToolResult, GetMetricSeriesOutput, error) {
	if in.Name == "" {
		return nil, GetMetricSeriesOutput{}, fmt.Errorf("name is required")
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 500
	}
	if limit > 5000 {
		limit = 5000
	}
	sessionID := h.resolveSession(in.SessionID)

	points, err := h.store.WithContext(ctx).GetMetricSeries(storage.MetricSeriesFilter{
		AttributeKey: in.AttributeKey, AttributeValue: in.AttributeValue,
		Attributes:         in.Attributes,
		ResourceAttributes: in.ResourceAttributes,
		Name:               in.Name,
		Service:            in.Service,
		SessionID:          sessionID,
		FromNs:             in.FromNs,
		ToNs:               in.ToNs,
		Limit:              limit,
	})
	if err != nil {
		return nil, GetMetricSeriesOutput{}, fmt.Errorf("get metric series: %w", err)
	}

	out := GetMetricSeriesOutput{SessionID: sessionID, Name: in.Name, Points: []MetricPointOut{}}
	for _, p := range points {
		attrs := parseAttrs(p.Attributes)
		out.Points = append(out.Points, MetricPointOut{
			TimestampNs: p.TimestampNs,
			Value:       p.Value,
			Service:     p.ServiceName,
			Attributes:  attrs,
			Exemplars:   p.Exemplars,
		})
		if len(out.Points) >= limit {
			break
		}
	}
	out.Count = len(out.Points)
	return nil, out, nil
}
