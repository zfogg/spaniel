package mcp

import (
	"testing"

	"github.com/zfogg/spaniel/internal/storage"
)

// seedMetrics inserts a small two-metric catalog into sessID.
func seedMetrics(t *testing.T, store *storage.DB, sessID string) {
	t.Helper()
	for _, m := range []*storage.Metric{
		{Name: "http.req.count", Description: "reqs", Unit: "req", Type: "counter", TimestampNs: 100, Value: 5, Attributes: `{"route":"/api/users"}`, ServiceName: "api", SessionID: sessID},
		{Name: "http.req.count", Description: "reqs", Unit: "req", Type: "counter", TimestampNs: 200, Value: 6, Attributes: `{"route":"/api/users"}`, ServiceName: "api", SessionID: sessID},
		{Name: "pg.pool.in_use", Description: "conns", Unit: "conn", Type: "gauge", TimestampNs: 100, Value: 8, Attributes: "{}", ServiceName: "postgres", SessionID: sessID},
	} {
		if err := store.InsertMetric(m); err != nil {
			t.Fatalf("insert metric: %v", err)
		}
	}
}

func TestListServices(t *testing.T) {
	cs, store := connect(t, Options{Version: "t"})
	seedTrace(t, store, store.ActiveSessionID())

	out := callStructured[ListServicesOutput](t, cs, "list_services", nil)
	if out.Count != 2 {
		t.Fatalf("expected 2 services, got %d (%v)", out.Count, out.Services)
	}
	set := map[string]bool{}
	for _, s := range out.Services {
		set[s] = true
	}
	if !set["api"] || !set["postgres"] {
		t.Errorf("expected api and postgres, got %v", out.Services)
	}
}

func TestGetServiceMap(t *testing.T) {
	cs, store := connect(t, Options{Version: "t"})
	seedTrace(t, store, store.ActiveSessionID())

	out := callStructured[ServiceMapOutput](t, cs, "get_service_map", nil)
	nodes := map[string]ServiceNodeOut{}
	for _, n := range out.Nodes {
		nodes[n.Service] = n
	}
	if _, ok := nodes["api"]; !ok {
		t.Errorf("service map missing api node: %+v", out.Nodes)
	}
	if _, ok := nodes["postgres"]; !ok {
		t.Errorf("service map missing postgres node: %+v", out.Nodes)
	}
	// api (server) calls postgres (client child) → one edge.
	foundEdge := false
	for _, e := range out.Edges {
		if e.From == "api" && e.To == "postgres" {
			foundEdge = true
			if e.CallCount < 1 {
				t.Errorf("api→postgres call_count = %d, want >=1", e.CallCount)
			}
		}
	}
	if !foundEdge {
		t.Errorf("expected api→postgres edge, got %+v", out.Edges)
	}
}

func TestGetStats(t *testing.T) {
	cs, store := connect(t, Options{Version: "t"})
	seedTrace(t, store, store.ActiveSessionID())

	out := callStructured[StatsOutput](t, cs, "get_stats", nil)
	if out.SpanCount != 2 {
		t.Errorf("span_count = %d, want 2", out.SpanCount)
	}
	if out.TraceCount != 1 {
		t.Errorf("trace_count = %d, want 1", out.TraceCount)
	}
	if out.LogCount != 1 {
		t.Errorf("log_count = %d, want 1", out.LogCount)
	}
}

func TestGetMetrics(t *testing.T) {
	cs, store := connect(t, Options{Version: "t"})
	seedMetrics(t, store, store.ActiveSessionID())

	out := callStructured[GetMetricsOutput](t, cs, "get_metrics", nil)
	if out.Count != 2 {
		t.Fatalf("expected 2 catalog entries, got %d (%+v)", out.Count, out.Metrics)
	}
	byName := map[string]MetricCatalogOut{}
	for _, m := range out.Metrics {
		byName[m.Name] = m
	}
	if byName["http.req.count"].Type != "counter" {
		t.Errorf("http.req.count type = %q, want counter", byName["http.req.count"].Type)
	}
	if byName["pg.pool.in_use"].Type != "gauge" {
		t.Errorf("pg.pool.in_use type = %q, want gauge", byName["pg.pool.in_use"].Type)
	}
}

func TestGetMetricSeries(t *testing.T) {
	cs, store := connect(t, Options{Version: "t"})
	seedMetrics(t, store, store.ActiveSessionID())

	out := callStructured[GetMetricSeriesOutput](t, cs, "get_metric_series", map[string]any{"name": "http.req.count"})
	if out.Count != 2 {
		t.Fatalf("expected 2 points, got %d (%+v)", out.Count, out.Points)
	}
	for _, p := range out.Points {
		if p.Service != "api" {
			t.Errorf("point service = %q, want api", p.Service)
		}
		if p.Attributes["route"] != "/api/users" {
			t.Errorf("point route attr = %q, want /api/users", p.Attributes["route"])
		}
	}
}

func TestGetMetricSeries_RequiresName(t *testing.T) {
	cs, _ := connect(t, Options{Version: "t"})
	if !callToolIsError(t, cs, "get_metric_series", map[string]any{}) {
		t.Error("expected tool error when name is empty")
	}
}

func TestTopologyToolsRegistered(t *testing.T) {
	cs, _ := connect(t, Options{Version: "t"})
	names := toolNames(t, cs)
	for _, want := range []string{"get_service_map", "list_services", "get_stats", "get_metrics", "get_metric_series"} {
		if !names[want] {
			t.Errorf("tool %q not registered; have %v", want, names)
		}
	}
}
