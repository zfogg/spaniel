package telemetry

// This file is Spaniel's owned self-observability catalog. Components record
// through these methods rather than inventing instruments and dimensions at
// call sites. Attribute values are deliberately finite vocabularies.

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type Metrics struct {
	receiverRequests          metric.Int64Counter
	receiverBytes             metric.Int64Counter
	receiverDuration          metric.Float64Histogram
	ingestPoints              metric.Int64Counter
	ingestDropped             metric.Int64Counter
	ingestQueueWait           metric.Float64Histogram
	ingestBatchSize           metric.Int64Histogram
	ingestRejected            metric.Int64Counter
	ingestDecodeErrors        metric.Int64Counter
	flushDuration             metric.Float64Histogram
	flushErrors               metric.Int64Counter
	storageBytesWritten       metric.Int64Counter
	storagePruneDuration      metric.Float64Histogram
	storagePruneFailures      metric.Int64Counter
	wsClientsConnected        metric.Int64Counter
	wsClientsDropped          metric.Int64Counter
	alertEvaluations          metric.Int64Counter
	ingestRequests            metric.Int64Counter
	ingestRequestDuration     metric.Float64Histogram
	ingestPayloadBytes        metric.Int64Histogram
	ingestRateLimited         metric.Int64Counter
	ingestSampledOut          metric.Int64Counter
	ingestSeriesLimited       metric.Int64Counter
	ingestStorageFull         metric.Int64Counter
	storageFlushRows          metric.Int64Histogram
	storageFlushErrors        metric.Int64Counter
	storageAppendDuration     metric.Float64Histogram
	storageCheckpointDuration metric.Float64Histogram
	wsMessagesSent            metric.Int64Counter
	wsMessageBytes            metric.Int64Histogram
	alertNotifications        metric.Int64Counter
	lintWarnings              metric.Int64Counter
	detectorIssues            metric.Int64Counter
	storageRetentionRows      metric.Int64Counter
	storageRows               metric.Int64Counter
	storageWriteDuration      metric.Float64Histogram
	storageQueryDuration      metric.Float64Histogram
	storageFailures           metric.Int64Counter
	retentionDeleted          metric.Int64Counter
	forwardRetries            metric.Int64Counter
	forwardDrops              metric.Int64Counter
	forwardDuration           metric.Float64Histogram
	apiRequests               metric.Int64Counter
	apiDuration               metric.Float64Histogram
	wsEvents                  metric.Int64Counter
	wsClients                 metric.Int64ObservableGauge
	wsClientCount             atomic.Int64
	cardinalityLimited        metric.Int64Counter
	activeSeries              metric.Int64ObservableGauge
	activeSeriesValue         atomic.Int64
	forwardQueueBytes         metric.Int64ObservableGauge
	forwardQueueMu            sync.RWMutex
	forwardQueueSnapshot      func() []ForwardQueueDepth
	ingestQueueDepth          atomic.Int64
	ingestQueueCapacity       atomic.Int64
	flushInFlight             atomic.Int64
	storageWriteQueue         atomic.Int64
	dbSizeLimit               atomic.Int64
	serverStartedAt           time.Time
	storageFull               atomic.Int64
	maintenanceInFlight       atomic.Int64
	dbSizeCurrent             atomic.Int64
	ingestQueueGauge          metric.Int64ObservableGauge
	ingestCapacityGauge       metric.Int64ObservableGauge
	flushInFlightGauge        metric.Int64ObservableGauge
	storageQueueGauge         metric.Int64ObservableGauge
	dbSizeLimitGauge          metric.Int64ObservableGauge
	ingestActiveGauge         metric.Int64ObservableGauge
	storageFullGauge          metric.Int64ObservableGauge
	maintenanceGauge          metric.Int64ObservableGauge
	dbSizeCurrentGauge        metric.Int64ObservableGauge
	seriesCountGauge          metric.Int64ObservableGauge
	serverUptimeGauge         metric.Float64ObservableGauge
}

// ForwardQueueDepth is intentionally identified by a stable hash, never an
// upstream URL which could contain credentials or arbitrary cardinality.
type ForwardQueueDepth struct {
	UpstreamID string
	Bytes      int64
}

var (
	metricsMu      sync.Mutex
	metricsCatalog *Metrics
)

// InitMetrics must run after the real meter provider is installed. It is safe
// to call again after a test/provider reset.
func InitMetrics() *Metrics {
	metricsMu.Lock()
	defer metricsMu.Unlock()
	meter := otel.Meter("spaniel")
	newCounter := func(name, desc, unit string) metric.Int64Counter {
		v, _ := meter.Int64Counter(name, metric.WithDescription(desc), metric.WithUnit(unit))
		return v
	}
	newDuration := func(name, desc string) metric.Float64Histogram {
		v, _ := meter.Float64Histogram(name, metric.WithDescription(desc), metric.WithUnit("ms"))
		return v
	}
	m := &Metrics{
		serverStartedAt:  time.Now(),
		receiverRequests: newCounter("spaniel.receiver.requests", "OTLP receiver requests", "{request}"), receiverBytes: newCounter("spaniel.receiver.bytes", "OTLP receiver payload bytes", "By"), receiverDuration: newDuration("spaniel.receiver.duration", "OTLP receiver request duration"),
		ingestPoints: newCounter("spaniel.ingest.metrics_points", "Telemetry points accepted by ingestion", "{point}"), ingestDropped: newCounter("spaniel.ingest.dropped", "Telemetry points dropped by ingestion", "{point}"), ingestRejected: newCounter("spaniel.ingest.rejected", "Ingestion requests rejected", "{request}"), ingestDecodeErrors: newCounter("spaniel.ingest.decode.errors", "OTLP decode errors", "{error}"), ingestQueueWait: newDuration("spaniel.ingest.queue_wait", "Time spent waiting for ingestion work"), ingestBatchSize: func() metric.Int64Histogram {
			v, _ := meter.Int64Histogram("spaniel.ingest.batch_size", metric.WithDescription("Points in an ingestion batch"), metric.WithUnit("{point}"))
			return v
		}(), flushDuration: newDuration("spaniel.ingest.flush.duration", "Ingestion flush duration"), flushErrors: newCounter("spaniel.ingest.flush.errors", "Ingestion flush errors", "{error}"),
		storageRows: newCounter("spaniel.storage.rows_written", "Rows written to storage", "{row}"), storageBytesWritten: newCounter("spaniel.storage.bytes_written", "Logical bytes written to storage", "By"), storageWriteDuration: newDuration("spaniel.storage.write.duration", "Storage write duration"), storageQueryDuration: newDuration("spaniel.storage.query.duration", "Storage query duration"), storageFailures: newCounter("spaniel.storage.failures", "Storage operation failures", "{failure}"), retentionDeleted: newCounter("spaniel.storage.retention.deleted", "Rows deleted by retention", "{row}"), storagePruneFailures: newCounter("spaniel.storage.prune.failures", "Storage prune failures", "{failure}"),
		forwardRetries: newCounter("spaniel.forwarder.retries", "Forwarder retry attempts", "{attempt}"), forwardDrops: newCounter("spaniel.forwarder.dropped", "Forwarder permanently dropped payloads", "{payload}"), forwardDuration: newDuration("spaniel.forwarder.export.duration", "Forwarder export duration"),
		apiRequests: newCounter("spaniel.api.requests", "API requests", "{request}"), apiDuration: newDuration("spaniel.api.duration", "API request duration"), wsEvents: newCounter("spaniel.websocket.events", "WebSocket events", "{event}"), wsClientsConnected: newCounter("spaniel.websocket.clients.connected", "WebSocket clients connected", "{client}"), wsClientsDropped: newCounter("spaniel.websocket.clients.dropped", "WebSocket clients dropped", "{client}"), alertEvaluations: newCounter("spaniel.alerts.evaluations", "Alert rule evaluations", "{evaluation}"), cardinalityLimited: newCounter("spaniel.metrics.cardinality_limited", "Metric attributes or series collapsed by cardinality policy", "{attribute}"),
		ingestRequests: newCounter("spaniel.ingest.requests", "Ingestion requests", "{request}"), ingestRateLimited: newCounter("spaniel.ingest.rate_limited", "Signals rejected by rate limiting", "{signal}"), ingestSampledOut: newCounter("spaniel.ingest.sampled_out", "Signals excluded by sampling", "{signal}"), ingestSeriesLimited: newCounter("spaniel.ingest.series_limited", "Metric series limited by cardinality policy", "{series}"), ingestStorageFull: newCounter("spaniel.ingest.storage_full_rejections", "Ingestion requests rejected because storage is full", "{request}"), storageFlushErrors: newCounter("spaniel.storage.flush.errors", "Storage flush errors", "{error}"), wsMessagesSent: newCounter("spaniel.websocket.messages.sent", "WebSocket messages sent", "{message}"), alertNotifications: newCounter("spaniel.alerts.notifications", "Alert notifications", "{notification}"), storageRetentionRows: newCounter("spaniel.storage.retention.deleted_rows", "Rows deleted by storage retention", "{row}"), lintWarnings: newCounter("spaniel.lint.warnings", "Lint warnings fired during span analysis", "{warning}"), detectorIssues: newCounter("spaniel.detector.issues_found", "Trace issues detected by post-ingestion detectors", "{issue}"),
	}
	m.ingestRequestDuration = newDuration("spaniel.ingest.request.duration", "Ingestion request duration")
	m.ingestPayloadBytes, _ = meter.Int64Histogram("spaniel.ingest.payload.bytes", metric.WithDescription("Ingestion request payload bytes"), metric.WithUnit("By"))
	m.storageFlushRows, _ = meter.Int64Histogram("spaniel.storage.flush.rows", metric.WithDescription("Rows per storage flush"), metric.WithUnit("{row}"))
	m.storageAppendDuration = newDuration("spaniel.storage.append.duration", "Storage append duration")
	m.storageCheckpointDuration = newDuration("spaniel.storage.checkpoint.duration", "Storage checkpoint duration")
	m.wsMessageBytes, _ = meter.Int64Histogram("spaniel.websocket.message.bytes", metric.WithDescription("WebSocket message bytes"), metric.WithUnit("By"))
	m.storagePruneDuration = newDuration("spaniel.storage.prune.duration", "Storage prune duration")
	m.ingestQueueGauge, _ = meter.Int64ObservableGauge("spaniel.ingest.queue.depth", metric.WithDescription("Pending ingestion work"), metric.WithUnit("{request}"), metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
		o.Observe(m.ingestQueueDepth.Load())
		return nil
	}))
	m.ingestCapacityGauge, _ = meter.Int64ObservableGauge("spaniel.ingest.queue.capacity", metric.WithDescription("Maximum concurrent ingestion work"), metric.WithUnit("{request}"), metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
		o.Observe(m.ingestQueueCapacity.Load())
		return nil
	}))
	m.flushInFlightGauge, _ = meter.Int64ObservableGauge("spaniel.ingest.flush.in_flight", metric.WithDescription("Ingestion flushes in progress"), metric.WithUnit("{flush}"), metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error { o.Observe(m.flushInFlight.Load()); return nil }))
	m.storageQueueGauge, _ = meter.Int64ObservableGauge("spaniel.storage.write_queue.depth", metric.WithDescription("Rows buffered for storage writes"), metric.WithUnit("{row}"), metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
		o.Observe(m.storageWriteQueue.Load())
		return nil
	}))
	m.dbSizeLimitGauge, _ = meter.Int64ObservableGauge("spaniel.storage.db_size.limit", metric.WithDescription("Configured database size limit"), metric.WithUnit("By"), metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error { o.Observe(m.dbSizeLimit.Load()); return nil }))
	m.ingestActiveGauge, _ = meter.Int64ObservableGauge("spaniel.ingest.active_requests", metric.WithDescription("Ingestion requests in progress or waiting"), metric.WithUnit("{request}"), metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
		o.Observe(m.ingestQueueDepth.Load())
		return nil
	}))
	m.storageFullGauge, _ = meter.Int64ObservableGauge("spaniel.storage.full", metric.WithDescription("Whether storage is rejecting ingestion"), metric.WithUnit("{state}"), metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error { o.Observe(m.storageFull.Load()); return nil }))
	m.maintenanceGauge, _ = meter.Int64ObservableGauge("spaniel.storage.maintenance.in_flight", metric.WithDescription("Storage maintenance operations in progress"), metric.WithUnit("{operation}"), metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
		o.Observe(m.maintenanceInFlight.Load())
		return nil
	}))
	m.dbSizeCurrentGauge, _ = meter.Int64ObservableGauge("spaniel.storage.db_size.current", metric.WithDescription("Current DuckDB file size"), metric.WithUnit("By"), metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error { o.Observe(m.dbSizeCurrent.Load()); return nil }))
	m.seriesCountGauge, _ = meter.Int64ObservableGauge("spaniel.metrics.series.count", metric.WithDescription("Indexed metric series count"), metric.WithUnit("{series}"), metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
		o.Observe(m.activeSeriesValue.Load())
		return nil
	}))
	m.serverUptimeGauge, _ = meter.Float64ObservableGauge("spaniel.server.uptime", metric.WithDescription("Spaniel process uptime"), metric.WithUnit("s"), metric.WithFloat64Callback(func(_ context.Context, o metric.Float64Observer) error {
		o.Observe(time.Since(m.serverStartedAt).Seconds())
		return nil
	}))
	// The value is updated on database open and on each admitted identity. It
	// does not need a polling goroutine and reflects the durable catalog.
	m.activeSeries, _ = meter.Int64ObservableGauge("spaniel.metrics.active_series",
		metric.WithDescription("Active indexed metric series in durable storage"),
		metric.WithUnit("{series}"),
		metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
			o.Observe(m.activeSeriesValue.Load())
			return nil
		}),
	)
	m.wsClients, _ = meter.Int64ObservableGauge("spaniel.websocket.clients",
		metric.WithDescription("Currently connected WebSocket clients"), metric.WithUnit("{client}"),
		metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error { o.Observe(m.wsClientCount.Load()); return nil }),
	)
	m.forwardQueueBytes, _ = meter.Int64ObservableGauge("spaniel.forwarder.queue.bytes",
		metric.WithDescription("Pending bytes in forwarding spool queues"), metric.WithUnit("By"),
		metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
			m.forwardQueueMu.RLock()
			snapshot := m.forwardQueueSnapshot
			m.forwardQueueMu.RUnlock()
			if snapshot != nil {
				for _, depth := range snapshot() {
					o.Observe(depth.Bytes, metric.WithAttributes(attribute.String("upstream_id", depth.UpstreamID)))
				}
			}
			return nil
		}),
	)
	metricsCatalog = m
	return m
}

func Catalog() *Metrics {
	metricsMu.Lock()
	current := metricsCatalog
	metricsMu.Unlock()
	if current != nil {
		return current
	}
	return InitMetrics()
}
func resultAttrs(signal, result string) []attribute.KeyValue {
	return []attribute.KeyValue{attribute.String("signal", signal), attribute.String("result", result)}
}
func (m *Metrics) RecordReceive(ctx context.Context, signal, result string, bytes int64, durationMs float64) {
	a := resultAttrs(signal, result)
	m.receiverRequests.Add(ctx, 1, metric.WithAttributes(a...))
	m.receiverBytes.Add(ctx, bytes, metric.WithAttributes(a...))
	m.receiverDuration.Record(ctx, durationMs, metric.WithAttributes(a...))
	b := append(a, attribute.String("protocol", "http"))
	m.ingestRequests.Add(ctx, 1, metric.WithAttributes(b...))
	m.ingestRequestDuration.Record(ctx, durationMs, metric.WithAttributes(b...))
	m.ingestPayloadBytes.Record(ctx, bytes, metric.WithAttributes(attribute.String("signal", signal), attribute.String("protocol", "http")))
}
func (m *Metrics) RecordIngest(ctx context.Context, signal, result string, points int64) {
	a := resultAttrs(signal, result)
	if result == "accepted" {
		m.ingestPoints.Add(ctx, points, metric.WithAttributes(a...))
	} else {
		m.ingestDropped.Add(ctx, points, metric.WithAttributes(a...))
	}
	m.ingestBatchSize.Record(ctx, points, metric.WithAttributes(a...))
}
func (m *Metrics) RecordIngestQueueWait(ctx context.Context, durationMs float64) {
	m.ingestQueueWait.Record(ctx, durationMs)
}
func (m *Metrics) RecordFlush(ctx context.Context, result string, durationMs float64) {
	a := metric.WithAttributes(attribute.String("result", result))
	m.flushDuration.Record(ctx, durationMs, a)
	if result != "ok" {
		m.flushErrors.Add(ctx, 1, a)
	}
}
func (m *Metrics) RecordIngestRejected(ctx context.Context, signal, reason string) {
	m.ingestRejected.Add(ctx, 1, metric.WithAttributes(attribute.String("signal", signal), attribute.String("reason", reason)))
}
func (m *Metrics) RecordIngestDecodeError(ctx context.Context, signal, protocol string) {
	m.ingestDecodeErrors.Add(ctx, 1, metric.WithAttributes(attribute.String("signal", signal), attribute.String("protocol", protocol)))
}
func (m *Metrics) RecordStorageBytesWritten(ctx context.Context, bytes int64) {
	if bytes > 0 {
		m.storageBytesWritten.Add(ctx, bytes)
	}
}
func (m *Metrics) RecordStoragePrune(ctx context.Context, result string, durationMs float64) {
	m.storagePruneDuration.Record(ctx, durationMs, metric.WithAttributes(attribute.String("result", result)))
	if result != "ok" {
		m.storagePruneFailures.Add(ctx, 1, metric.WithAttributes(attribute.String("result", result)))
	}
}
func (m *Metrics) RecordAlertEvaluation(ctx context.Context, result string) {
	m.alertEvaluations.Add(ctx, 1, metric.WithAttributes(attribute.String("result", result)))
}
func (m *Metrics) RecordIngestRateLimited(ctx context.Context, signal string, count int64) {
	if count > 0 {
		m.ingestRateLimited.Add(ctx, count, metric.WithAttributes(attribute.String("signal", signal)))
	}
}
func (m *Metrics) RecordIngestSampledOut(ctx context.Context, signal string, count int64) {
	if count > 0 {
		m.ingestSampledOut.Add(ctx, count, metric.WithAttributes(attribute.String("signal", signal)))
	}
}
func (m *Metrics) RecordIngestSeriesLimited(ctx context.Context, signal string) {
	m.ingestSeriesLimited.Add(ctx, 1, metric.WithAttributes(attribute.String("signal", signal)))
}
func (m *Metrics) RecordIngestStorageFull(ctx context.Context, signal string) {
	m.ingestStorageFull.Add(ctx, 1, metric.WithAttributes(attribute.String("signal", signal)))
}
func (m *Metrics) RecordStorageFlush(ctx context.Context, table, result string, rows int64) {
	m.storageFlushRows.Record(ctx, rows, metric.WithAttributes(attribute.String("table", table)))
	if result != "ok" {
		m.storageFlushErrors.Add(ctx, 1, metric.WithAttributes(attribute.String("table", table)))
	}
}
func (m *Metrics) RecordStorageAppend(ctx context.Context, table string, durationMs float64) {
	m.storageAppendDuration.Record(ctx, durationMs, metric.WithAttributes(attribute.String("table", table)))
}
func (m *Metrics) RecordStorageCheckpoint(ctx context.Context, durationMs float64) {
	m.storageCheckpointDuration.Record(ctx, durationMs)
}
func (m *Metrics) RecordWebSocketMessageSent(ctx context.Context, event string, bytes int64) {
	m.wsMessagesSent.Add(ctx, 1, metric.WithAttributes(attribute.String("event", event)))
	m.wsMessageBytes.Record(ctx, bytes, metric.WithAttributes(attribute.String("direction", "sent")))
}
func (m *Metrics) RecordWebSocketMessageReceived(ctx context.Context, bytes int64) {
	m.wsMessageBytes.Record(ctx, bytes, metric.WithAttributes(attribute.String("direction", "received")))
}
func (m *Metrics) RecordAlertNotification(ctx context.Context, transition, result string) {
	m.alertNotifications.Add(ctx, 1, metric.WithAttributes(attribute.String("transition", transition), attribute.String("result", result)))
}
func (m *Metrics) RecordLintWarning(ctx context.Context, rule, severity string) {
	m.lintWarnings.Add(ctx, 1, metric.WithAttributes(attribute.String("rule_id", rule), attribute.String("severity", severity)))
}
func (m *Metrics) RecordDetectorIssue(ctx context.Context, kind string) {
	m.detectorIssues.Add(ctx, 1, metric.WithAttributes(attribute.String("kind", kind)))
}
func (m *Metrics) RecordStorage(ctx context.Context, operation, result string, rows int64, durationMs float64) {
	a := metric.WithAttributes(attribute.String("operation", operation), attribute.String("result", result))
	if operation == "query" {
		m.storageQueryDuration.Record(ctx, durationMs, a)
	} else {
		m.storageWriteDuration.Record(ctx, durationMs, a)
	}
	if rows > 0 {
		m.storageRows.Add(ctx, rows, a)
	}
	if result != "ok" {
		m.storageFailures.Add(ctx, 1, a)
	}
}
func (m *Metrics) RecordAPI(ctx context.Context, route, status string, durationMs float64) {
	a := metric.WithAttributes(attribute.String("route", route), attribute.String("status_class", status))
	m.apiRequests.Add(ctx, 1, a)
	m.apiDuration.Record(ctx, durationMs, a)
}
func (m *Metrics) RecordCardinalityLimited(ctx context.Context, reason string, count int64) {
	m.cardinalityLimited.Add(ctx, count, metric.WithAttributes(attribute.String("reason", reason)))
}

// RecordRetention records rows deleted by a completed retention pass. The
// reason vocabulary mirrors the configured retention policy.
func (m *Metrics) RecordRetention(ctx context.Context, reason string, rows int64) {
	if rows > 0 {
		m.retentionDeleted.Add(ctx, rows, metric.WithAttributes(attribute.String("reason", reason)))
		m.storageRetentionRows.Add(ctx, rows, metric.WithAttributes(attribute.String("reason", reason)))
	}
}

// SetActiveSeries is fed from storage's durable metric_series_catalog, not a
// request-local map. It is intentionally a snapshot gauge rather than a sum.
func (m *Metrics) SetActiveSeries(count int64)     { m.activeSeriesValue.Store(count) }
func (m *Metrics) SetWebSocketClients(count int64) { m.wsClientCount.Store(count) }
func (m *Metrics) SetIngestQueue(depth, capacity int64) {
	m.ingestQueueDepth.Store(depth)
	m.ingestQueueCapacity.Store(capacity)
}
func (m *Metrics) SetFlushInFlight(count int64)          { m.flushInFlight.Store(count) }
func (m *Metrics) AddFlushInFlight(delta int64)          { m.flushInFlight.Add(delta) }
func (m *Metrics) SetStorageWriteQueueDepth(count int64) { m.storageWriteQueue.Store(count) }
func (m *Metrics) SetStorageDBSizeLimit(bytes int64)     { m.dbSizeLimit.Store(bytes) }
func (m *Metrics) SetStorageFull(full bool) {
	if full {
		m.storageFull.Store(1)
	} else {
		m.storageFull.Store(0)
	}
}
func (m *Metrics) SetStorageMaintenanceInFlight(count int64) { m.maintenanceInFlight.Store(count) }
func (m *Metrics) SetStorageDBSizeCurrent(bytes int64)       { m.dbSizeCurrent.Store(bytes) }
func (m *Metrics) RegisterForwardQueueSnapshot(snapshot func() []ForwardQueueDepth) {
	m.forwardQueueMu.Lock()
	m.forwardQueueSnapshot = snapshot
	m.forwardQueueMu.Unlock()
}
func (m *Metrics) RecordForward(ctx context.Context, result string, durationMs float64) {
	a := metric.WithAttributes(attribute.String("result", result))
	m.forwardDuration.Record(ctx, durationMs, a)
	if result == "retry" {
		m.forwardRetries.Add(ctx, 1, a)
	}
	if result == "dropped" || result == "enqueue_error" {
		m.forwardDrops.Add(ctx, 1, a)
	}
}

func (m *Metrics) RecordForwardDrops(ctx context.Context, count int64) {
	if count > 0 {
		m.forwardDrops.Add(ctx, count, metric.WithAttributes(attribute.String("result", "spool_capacity")))
	}
}
func (m *Metrics) RecordWebSocket(ctx context.Context, event, result string, count int64) {
	m.wsEvents.Add(ctx, count, metric.WithAttributes(attribute.String("event", event), attribute.String("result", result)))
}
func (m *Metrics) RecordWebSocketClientConnected(ctx context.Context) {
	m.wsClientsConnected.Add(ctx, 1)
}
func (m *Metrics) RecordWebSocketClientDropped(ctx context.Context, reason string) {
	m.wsClientsDropped.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", reason)))
}
