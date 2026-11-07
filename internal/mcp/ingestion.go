package mcp

import (
	"context"
	"fmt"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zfogg/spaniel/internal/forwarder"
	"github.com/zfogg/spaniel/internal/storage"
)

func (h *handler) registerIngestionTools(s *mcpsdk.Server) {
	addTOONTool(s, &mcpsdk.Tool{Name: "get_ingestion_health", Description: "Get live ingest rates, source health, rejected and dropped telemetry, storage-full state, and forwarder status.", Annotations: &mcpsdk.ToolAnnotations{ReadOnlyHint: true}}, h.getIngestionHealth)
}

type IngestionHealthOutput struct {
	Status                    string                `json:"status" jsonschema:"healthy, degraded, or unknown"`
	Reasons                   []string              `json:"reasons"`
	SuggestedTools            []string              `json:"suggested_tools"`
	EvaluatedAtNs             int64                 `json:"evaluated_at_ns" jsonschema:"evaluation time as Unix nanoseconds"`
	LastSourceSeenNs          int64                 `json:"last_source_seen_ns" jsonschema:"most recent source activity as Unix nanoseconds; zero means unknown"`
	StorageRemainingBytes     *int64                `json:"storage_remaining_bytes,omitempty"`
	StorageUtilizationPercent *float64              `json:"storage_utilization_percent,omitempty"`
	AutoPrune                 *bool                 `json:"auto_prune,omitempty"`
	Healthy                   bool                  `json:"healthy"`
	StorageFull               bool                  `json:"storage_full"`
	MaxDBSizeBytes            int64                 `json:"max_db_size_bytes" jsonschema:"telemetry database limit in bytes; zero means unlimited"`
	DBSizeBytes               int64                 `json:"db_size_bytes"`
	Throughput                storage.Throughput    `json:"throughput"`
	Sources                   []storage.SourceStats `json:"sources"`
	DroppedSpans              int64                 `json:"dropped_spans" jsonschema:"cumulative dropped spans since process start"`
	DroppedLogs               int64                 `json:"dropped_logs" jsonschema:"cumulative dropped logs since process start"`
	DroppedMetricPoints       int64                 `json:"dropped_metric_points" jsonschema:"cumulative dropped metric points since process start"`
	LastDropAt                int64                 `json:"last_drop_at"`
	Forwarders                []forwarder.Status    `json:"forwarders"`
}

func (h *handler) getIngestionHealth(ctx context.Context, _ *mcpsdk.CallToolRequest, _ emptyInput) (*mcpsdk.CallToolResult, IngestionHealthOutput, error) {
	stats, err := h.store.WithContext(ctx).GetStats(h.store.ActiveSessionID())
	if err != nil {
		return nil, IngestionHealthOutput{}, fmt.Errorf("get storage stats: %w", err)
	}
	out := IngestionHealthOutput{Healthy: !stats.StorageFull, StorageFull: stats.StorageFull, DBSizeBytes: stats.DBSize, MaxDBSizeBytes: h.maxDBSizeBytes(), Throughput: stats.Throughput, Sources: []storage.SourceStats{}, DroppedSpans: stats.DroppedSpans, DroppedLogs: stats.DroppedLogs, DroppedMetricPoints: stats.DroppedMetricPoints, LastDropAt: stats.LastDropAt, Forwarders: []forwarder.Status{}}
	out.EvaluatedAtNs = time.Now().UnixNano()
	out.Status = "healthy"
	out.Reasons = []string{}
	out.SuggestedTools = []string{}
	if out.MaxDBSizeBytes > 0 {
		remaining := max(int64(0), out.MaxDBSizeBytes-out.DBSizeBytes)
		utilization := float64(out.DBSizeBytes) * 100 / float64(out.MaxDBSizeBytes)
		out.StorageRemainingBytes, out.StorageUtilizationPercent = &remaining, &utilization
	}
	if h.opts.AutoPrune != nil {
		value := h.opts.AutoPrune()
		out.AutoPrune = &value
	}
	if h.opts.Throughput != nil {
		out.Throughput = h.opts.Throughput.Throughput()
	}
	if h.opts.Sources != nil {
		out.Sources = h.opts.Sources.Sources()
	} else if sources, err := h.store.WithContext(ctx).GetSourceStats(h.store.ActiveSessionID()); err == nil {
		out.Sources = sources
	}
	if h.opts.Drops != nil {
		out.DroppedSpans = h.opts.Drops.DroppedSpansTotal()
		out.DroppedLogs = h.opts.Drops.DroppedLogsTotal()
		out.DroppedMetricPoints = h.opts.Drops.DroppedMetricPointsTotal()
		out.LastDropAt = h.opts.Drops.LastDropAt()
	}
	if h.opts.Forwarder != nil {
		out.Forwarders = h.opts.Forwarder.Status()
		for _, status := range out.Forwarders {
			if status.LastErr != "" {
				out.Healthy = false
				out.Reasons = append(out.Reasons, "A forwarder reports an error")
			}
		}
	}
	for _, source := range out.Sources {
		out.LastSourceSeenNs = max(out.LastSourceSeenNs, source.LastSeenNs)
		if source.RejectedPerSec > 0 {
			out.Healthy = false
			out.Reasons = append(out.Reasons, "A source is rejecting telemetry")
		}
	}
	if out.StorageFull {
		out.Reasons = append(out.Reasons, "Telemetry storage is full")
		out.SuggestedTools = append(out.SuggestedTools, "get_stats")
	}
	if !out.Healthy {
		out.Status = "degraded"
		out.SuggestedTools = append(out.SuggestedTools, "get_server_info")
	} else if len(out.Sources) == 0 {
		out.Status = "unknown"
		out.Reasons = append(out.Reasons, "No sources observed; ingestion health cannot be established")
	} else if out.LastSourceSeenNs == 0 || out.EvaluatedAtNs-out.LastSourceSeenNs > int64(5*time.Minute) {
		out.Status = "unknown"
		out.Reasons = append(out.Reasons, "No source activity observed in the past five minutes; ingestion health cannot be established")
	}
	return nil, out, nil
}
