package mcp

import (
	"context"
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zfogg/spaniel/internal/storage"
)

// registerTopologyTools adds the service-map, services, stats, and metrics read
// tools (issue #101).
func (h *handler) registerTopologyTools(s *mcpsdk.Server) {
	readOnly := &mcpsdk.ToolAnnotations{ReadOnlyHint: true}

	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "get_service_map",
		Description: "Get the service dependency graph for a session: nodes (services with span/error counts and p95 latency) and edges (caller→callee with call counts, average duration, and errors). Use to understand how services call each other and where errors concentrate. Defaults to the active session.",
		Annotations: readOnly,
	}, h.getServiceMap)

	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "list_services",
		Description: "List all service.name values observed across stored telemetry.",
		Annotations: readOnly,
	}, h.listServices)

	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "get_stats",
		Description: "Get counts and storage stats for a session (spans, traces, logs, sessions, DB size, and dropped-signal counters). Defaults to the active session.",
		Annotations: readOnly,
	}, h.getStats)

	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "get_metrics",
		Description: "List the metric catalog for a session: each metric name with its type (gauge/counter/histogram), unit, description, service, and sample count. Use get_metric_series to fetch the actual data points. Defaults to the active session.",
		Annotations: readOnly,
	}, h.getMetrics)

	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "get_metric_series",
		Description: "Get the time-series data points for one metric (by name), optionally narrowed by service and a [from_ns, to_ns] time window. Points include value, attributes, and exemplar links to traces. Defaults to the active session.",
		Annotations: readOnly,
	}, h.getMetricSeries)
}

// ---- get_service_map ----

type GetServiceMapInput struct {
	SessionID string `json:"session_id,omitempty" jsonschema:"session to query; defaults to the active session"`
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
	data, err := h.store.GetServiceMap(sessionID)
	if err != nil {
		return nil, ServiceMapOutput{}, fmt.Errorf("get service map: %w", err)
	}
	out := ServiceMapOutput{SessionID: sessionID, Nodes: []ServiceNodeOut{}, Edges: []ServiceEdgeOut{}}
	if data != nil {
		for _, n := range data.Nodes {
			out.Nodes = append(out.Nodes, ServiceNodeOut{
				Service: n.ID, SpanCount: n.SpanCount, ErrorCount: n.ErrorCount, P95Ms: nsToMs(n.P95Ns),
			})
		}
		for _, e := range data.Edges {
			out.Edges = append(out.Edges, ServiceEdgeOut{
				From: e.From, To: e.To, CallCount: e.CallCount, AvgMs: nsToMs(e.AvgDurationNs), ErrorCount: e.ErrorCount,
			})
		}
	}
	return nil, out, nil
}

// ---- list_services ----

type ListServicesOutput struct {
	Count    int      `json:"count"`
	Services []string `json:"services"`
}

func (h *handler) listServices(ctx context.Context, _ *mcpsdk.CallToolRequest, _ emptyInput) (*mcpsdk.CallToolResult, ListServicesOutput, error) {
	services, err := h.store.ListServices()
	if err != nil {
		return nil, ListServicesOutput{}, fmt.Errorf("list services: %w", err)
	}
	if services == nil {
		services = []string{}
	}
	return nil, ListServicesOutput{Count: len(services), Services: services}, nil
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
	stats, err := h.store.GetStats(sessionID)
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
	entries, err := h.store.ListMetricCatalog(sessionID)
	if err != nil {
		return nil, GetMetricsOutput{}, fmt.Errorf("get metrics: %w", err)
	}
	out := GetMetricsOutput{SessionID: sessionID, Metrics: []MetricCatalogOut{}}
	for _, e := range entries {
		out.Metrics = append(out.Metrics, MetricCatalogOut{
			Name: e.Name, Type: e.Type, Unit: e.Unit, Description: e.Description,
			Service: e.ServiceName, SampleCount: e.SampleCount,
		})
	}
	out.Count = len(out.Metrics)
	return nil, out, nil
}

// ---- get_metric_series ----

type GetMetricSeriesInput struct {
	Name      string `json:"name" jsonschema:"metric name (from get_metrics)"`
	Service   string `json:"service,omitempty" jsonschema:"narrow to one service"`
	SessionID string `json:"session_id,omitempty" jsonschema:"session to query; defaults to the active session"`
	FromNs    int64  `json:"from_ns,omitempty" jsonschema:"inclusive lower bound (unix ns); 0 = no bound"`
	ToNs      int64  `json:"to_ns,omitempty" jsonschema:"inclusive upper bound (unix ns); 0 = no bound"`
	Limit     int    `json:"limit,omitempty" jsonschema:"max points to return (default 500)"`
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
	sessionID := h.resolveSession(in.SessionID)

	points, err := h.store.GetMetricSeries(storage.MetricSeriesFilter{
		Name:      in.Name,
		Service:   in.Service,
		SessionID: sessionID,
		FromNs:    in.FromNs,
		ToNs:      in.ToNs,
	})
	if err != nil {
		return nil, GetMetricSeriesOutput{}, fmt.Errorf("get metric series: %w", err)
	}

	out := GetMetricSeriesOutput{SessionID: sessionID, Name: in.Name, Points: []MetricPointOut{}}
	for _, p := range points {
		out.Points = append(out.Points, MetricPointOut{
			TimestampNs: p.TimestampNs,
			Value:       p.Value,
			Service:     p.ServiceName,
			Attributes:  parseAttrs(p.Attributes),
			Exemplars:   p.Exemplars,
		})
		if len(out.Points) >= limit {
			break
		}
	}
	out.Count = len(out.Points)
	return nil, out, nil
}
