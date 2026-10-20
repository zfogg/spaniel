package mcp

import (
	"context"
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zfogg/spaniel/internal/forwarder"
	"github.com/zfogg/spaniel/internal/storage"
)

func (h *handler) registerIngestionTools(s *mcpsdk.Server) {
	addTOONTool(s, &mcpsdk.Tool{Name: "get_ingestion_health", Description: "Get live ingest rates, source health, rejected and dropped telemetry, storage-full state, and forwarder status.", Annotations: &mcpsdk.ToolAnnotations{ReadOnlyHint: true}}, h.getIngestionHealth)
}

type IngestionHealthOutput struct {
	Healthy             bool                  `json:"healthy"`
	StorageFull         bool                  `json:"storage_full"`
	DBSizeBytes         int64                 `json:"db_size_bytes"`
	Throughput          storage.Throughput    `json:"throughput"`
	Sources             []storage.SourceStats `json:"sources"`
	DroppedSpans        int64                 `json:"dropped_spans"`
	DroppedLogs         int64                 `json:"dropped_logs"`
	DroppedMetricPoints int64                 `json:"dropped_metric_points"`
	LastDropAt          int64                 `json:"last_drop_at"`
	Forwarders          []forwarder.Status    `json:"forwarders"`
}

func (h *handler) getIngestionHealth(ctx context.Context, _ *mcpsdk.CallToolRequest, _ emptyInput) (*mcpsdk.CallToolResult, IngestionHealthOutput, error) {
	stats, err := h.store.WithContext(ctx).GetStats(h.store.ActiveSessionID())
	if err != nil {
		return nil, IngestionHealthOutput{}, fmt.Errorf("get storage stats: %w", err)
	}
	out := IngestionHealthOutput{Healthy: !stats.StorageFull, StorageFull: stats.StorageFull, DBSizeBytes: stats.DBSize, Throughput: stats.Throughput, Sources: []storage.SourceStats{}, DroppedSpans: stats.DroppedSpans, DroppedLogs: stats.DroppedLogs, DroppedMetricPoints: stats.DroppedMetricPoints, LastDropAt: stats.LastDropAt, Forwarders: []forwarder.Status{}}
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
			}
		}
	}
	for _, source := range out.Sources {
		if source.RejectedPerSec > 0 {
			out.Healthy = false
		}
	}
	return nil, out, nil
}
