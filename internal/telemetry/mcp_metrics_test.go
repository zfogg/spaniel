package telemetry

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestMCPMetrics(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	previous := otel.GetMeterProvider()
	otel.SetMeterProvider(provider)
	catalog := InitMetrics()
	t.Cleanup(func() { otel.SetMeterProvider(previous); InitMetrics(); _ = provider.Shutdown(context.Background()) })
	catalog.RecordMCP(context.Background(), "tools/call", "get_service_summary", "ok", 12.5)
	catalog.RecordMCP(context.Background(), "tools/call", "list_attribute_values", "error", 2)
	var data metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &data); err != nil {
		t.Fatal(err)
	}
	requests, durations := false, false
	for _, scope := range data.ScopeMetrics {
		for _, m := range scope.Metrics {
			switch m.Name {
			case "spaniel.mcp.requests":
				sum, ok := m.Data.(metricdata.Sum[int64])
				if !ok || len(sum.DataPoints) != 2 {
					t.Fatalf("request data: %+v", m.Data)
				}
				for _, p := range sum.DataPoints {
					if p.Value != 1 {
						t.Fatalf("request count: %v", p.Value)
					}
				}
				requests = true
			case "spaniel.mcp.duration":
				h, ok := m.Data.(metricdata.Histogram[float64])
				if !ok || len(h.DataPoints) != 2 {
					t.Fatalf("duration data: %+v", m.Data)
				}
				for _, p := range h.DataPoints {
					if p.Count != 1 {
						t.Fatalf("duration count: %v", p.Count)
					}
				}
				durations = true
			}
		}
	}
	if !requests || !durations {
		t.Fatalf("missing MCP metrics: requests=%v durations=%v", requests, durations)
	}
}
