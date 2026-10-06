package telemetry

// This file is Spaniel's owned self-observability catalog. Components record
// through these methods rather than inventing instruments and dimensions at
// call sites. Attribute values are deliberately finite vocabularies.

import (
	"context"
	"sync"
	"sync/atomic"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type Metrics struct {
	receiverRequests     metric.Int64Counter
	receiverBytes        metric.Int64Counter
	receiverDuration     metric.Float64Histogram
	ingestPoints         metric.Int64Counter
	ingestDropped        metric.Int64Counter
	ingestQueueWait      metric.Float64Histogram
	ingestBatchSize      metric.Int64Histogram
	ingestRejected       metric.Int64Counter
	ingestDecodeErrors   metric.Int64Counter
	flushDuration        metric.Float64Histogram
	flushErrors          metric.Int64Counter
	storageBytesWritten  metric.Int64Counter
	storagePruneDuration metric.Float64Histogram
	storagePruneFailures metric.Int64Counter
	wsClientsConnected   metric.Int64Counter
	wsClientsDropped     metric.Int64Counter
	alertEvaluations     metric.Int64Counter
	storageRows          metric.Int64Counter
	storageWriteDuration metric.Float64Histogram
	storageQueryDuration metric.Float64Histogram
	storageFailures      metric.Int64Counter
	retentionDeleted     metric.Int64Counter
	forwardRetries       metric.Int64Counter
	forwardDrops         metric.Int64Counter
	forwardDuration      metric.Float64Histogram
	apiRequests          metric.Int64Counter
	apiDuration          metric.Float64Histogram
	wsEvents             metric.Int64Counter
	wsClients            metric.Int64ObservableGauge
	wsClientCount        atomic.Int64
	cardinalityLimited   metric.Int64Counter
	activeSeries         metric.Int64ObservableGauge
	activeSeriesValue    atomic.Int64
	forwardQueueBytes    metric.Int64ObservableGauge
	forwardQueueMu       sync.RWMutex
	forwardQueueSnapshot func() []ForwardQueueDepth
	ingestQueueDepth     atomic.Int64
	ingestQueueCapacity  atomic.Int64
	flushInFlight        atomic.Int64
	storageWriteQueue    atomic.Int64
	dbSizeLimit          atomic.Int64
	ingestQueueGauge     metric.Int64ObservableGauge
	ingestCapacityGauge  metric.Int64ObservableGauge
	flushInFlightGauge   metric.Int64ObservableGauge
	storageQueueGauge    metric.Int64ObservableGauge
	dbSizeLimitGauge     metric.Int64ObservableGauge
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
		receiverRequests: newCounter("spaniel.receiver.requests", "OTLP receiver requests", "{request}"), receiverBytes: newCounter("spaniel.receiver.bytes", "OTLP receiver payload bytes", "By"), receiverDuration: newDuration("spaniel.receiver.duration", "OTLP receiver request duration"),
		ingestPoints: newCounter("spaniel.ingest.metrics_points", "Telemetry points accepted by ingestion", "{point}"), ingestDropped: newCounter("spaniel.ingest.dropped", "Telemetry points dropped by ingestion", "{point}"), ingestRejected: newCounter("spaniel.ingest.rejected", "Ingestion requests rejected", "{request}"), ingestDecodeErrors: newCounter("spaniel.ingest.decode.errors", "OTLP decode errors", "{error}"), ingestQueueWait: newDuration("spaniel.ingest.queue_wait", "Time spent waiting for ingestion work"), ingestBatchSize: func() metric.Int64Histogram {
			v, _ := meter.Int64Histogram("spaniel.ingest.batch_size", metric.WithDescription("Points in an ingestion batch"), metric.WithUnit("{point}"))
			return v
		}(), flushDuration: newDuration("spaniel.ingest.flush.duration", "Ingestion flush duration"), flushErrors: newCounter("spaniel.ingest.flush.errors", "Ingestion flush errors", "{error}"),
		storageRows: newCounter("spaniel.storage.rows_written", "Rows written to storage", "{row}"), storageBytesWritten: newCounter("spaniel.storage.bytes_written", "Logical bytes written to storage", "By"), storageWriteDuration: newDuration("spaniel.storage.write.duration", "Storage write duration"), storageQueryDuration: newDuration("spaniel.storage.query.duration", "Storage query duration"), storageFailures: newCounter("spaniel.storage.failures", "Storage operation failures", "{failure}"), retentionDeleted: newCounter("spaniel.storage.retention.deleted", "Rows deleted by retention", "{row}"), storagePruneFailures: newCounter("spaniel.storage.prune.failures", "Storage prune failures", "{failure}"),
		forwardRetries: newCounter("spaniel.forwarder.retries", "Forwarder retry attempts", "{attempt}"), forwardDrops: newCounter("spaniel.forwarder.dropped", "Forwarder permanently dropped payloads", "{payload}"), forwardDuration: newDuration("spaniel.forwarder.export.duration", "Forwarder export duration"),
		apiRequests: newCounter("spaniel.api.requests", "API requests", "{request}"), apiDuration: newDuration("spaniel.api.duration", "API request duration"), wsEvents: newCounter("spaniel.websocket.events", "WebSocket events", "{event}"), wsClientsConnected: newCounter("spaniel.websocket.clients.connected", "WebSocket clients connected", "{client}"), wsClientsDropped: newCounter("spaniel.websocket.clients.dropped", "WebSocket clients dropped", "{client}"), alertEvaluations: newCounter("spaniel.alerts.evaluations", "Alert rule evaluations", "{evaluation}"), cardinalityLimited: newCounter("spaniel.metrics.cardinality_limited", "Metric attributes or series collapsed by cardinality policy", "{attribute}"),
	}
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
