package ingestion

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/ws"
)

type Pipeline struct {
	store   *storage.DB
	hub     *ws.Hub
	tp      *throughputCounter
	sampler *Sampler

	debounce   map[string]*time.Timer
	debounceMu sync.Mutex

	tracer        trace.Tracer
	ingestCounter metric.Int64Counter
}

func NewPipeline(store *storage.DB, hub *ws.Hub) *Pipeline {
	return NewPipelineWithSampler(store, hub, NewSampler(0, KeepErrors|KeepNPlusOne|KeepSlow))
}

func NewPipelineWithSampler(store *storage.DB, hub *ws.Hub, s *Sampler) *Pipeline {
	meter := otel.Meter("spaniel/ingestion")
	counter, _ := meter.Int64Counter("spaniel.ingest.signals",
		metric.WithDescription("Total signals (spans, logs, metric points) ingested"),
		metric.WithUnit("{signal}"),
	)
	return &Pipeline{
		store:         store,
		hub:           hub,
		tp:            newThroughputCounter(),
		sampler:       s,
		debounce:      make(map[string]*time.Timer),
		tracer:        otel.Tracer("spaniel/ingestion"),
		ingestCounter: counter,
	}
}

// Throughput returns the current rolling per-second ingest rates.
func (p *Pipeline) Throughput() storage.Throughput {
	return p.tp.Throughput()
}

// DropCounters returns the sampler's drop counters for surfacing in /api/stats.
func (p *Pipeline) DropCounters() *Counters { return &p.sampler.Counters }

func (p *Pipeline) IngestTraces(ctx context.Context, traces ptrace.Traces) error {
	ctx, span := p.tracer.Start(ctx, "IngestTraces",
		trace.WithAttributes(attribute.Int("otel.span_count", traces.SpanCount())))
	defer span.End()

	sessionID := p.store.ActiveSessionID()

	sessionLabel := p.store.ActiveSessionLabel()

	spansSeen := 0
	defer func() { p.tp.addSpans(spansSeen) }()

	for i := 0; i < traces.ResourceSpans().Len(); i++ {
		rs := traces.ResourceSpans().At(i)
		svcName := serviceNameFromAttrs(rs.Resource().Attributes())
		resourceJSON := mapToJSON(rs.Resource().Attributes())

		for j := 0; j < rs.ScopeSpans().Len(); j++ {
			ss := rs.ScopeSpans().At(j)
			for k := 0; k < ss.Spans().Len(); k++ {
				span := ss.Spans().At(k)

				s := &storage.Span{
					TraceID:       span.TraceID().String(),
					SpanID:        span.SpanID().String(),
					ParentSpanID:  span.ParentSpanID().String(),
					ServiceName:   svcName,
					Name:          span.Name(),
					Kind:          int(span.Kind()),
					StartNs:       int64(span.StartTimestamp()),
					EndNs:         int64(span.EndTimestamp()),
					StatusCode:    int(span.Status().Code()),
					StatusMessage: span.Status().Message(),
					Attributes:    mapToJSON(span.Attributes()),
					Resource:      resourceJSON,
					SessionID:     sessionID,
					SessionLabel:  sessionLabel,
					ReceivedAt:    time.Now().UnixNano(),
					Sampled:       true,
				}

				if !p.sampler.DecideSpan(s) {
					p.sampler.Counters.bumpSpans(1)
					continue
				}

				if err := p.store.InsertSpan(s); err != nil {
					return err
				}
				spansSeen++

				// Persist span events. Exception events keep their canonical
				// exception.* attributes (mapToJSON preserves the whole map),
				// so the frontend can render a stack trace from them.
				if span.Events().Len() > 0 {
					events := make([]*storage.SpanEvent, 0, span.Events().Len())
					for ei := 0; ei < span.Events().Len(); ei++ {
						ev := span.Events().At(ei)
						events = append(events, &storage.SpanEvent{
							SpanID:     s.SpanID,
							TraceID:    s.TraceID,
							SessionID:  sessionID,
							TimeNs:     int64(ev.Timestamp()),
							Name:       ev.Name(),
							Attributes: mapToJSON(ev.Attributes()),
						})
					}
					if err := p.store.InsertSpanEvents(events); err != nil {
						return err
					}
				}

				// Persist span links — references to other spans/traces this
				// span was caused by (fan-out batches, async retries, etc.).
				if span.Links().Len() > 0 {
					links := make([]*storage.SpanLink, 0, span.Links().Len())
					for li := 0; li < span.Links().Len(); li++ {
						ln := span.Links().At(li)
						links = append(links, &storage.SpanLink{
							SpanID:        s.SpanID,
							TraceID:       s.TraceID,
							SessionID:     sessionID,
							LinkedTraceID: ln.TraceID().String(),
							LinkedSpanID:  ln.SpanID().String(),
							TraceState:    ln.TraceState().AsRaw(),
							Attributes:    mapToJSON(ln.Attributes()),
						})
					}
					if err := p.store.InsertSpanLinks(links); err != nil {
						return err
					}
				}

				go lintSpan(s, sessionID, p.store)

				p.hub.Broadcast(ws.NewSpanEvent(&ws.SpanPayload{
					TraceID:     s.TraceID,
					SpanID:      s.SpanID,
					ServiceName: s.ServiceName,
					Name:        s.Name,
					DurationNs:  s.EndNs - s.StartNs,
					StatusCode:  s.StatusCode,
				}))

				p.scheduleDetectors(s.TraceID)
			}
		}
	}
	p.ingestCounter.Add(ctx, int64(spansSeen), metric.WithAttributes(attribute.String("signal", "traces")))
	return nil
}

func (p *Pipeline) IngestLogs(ctx context.Context, logs plog.Logs) error {
	ctx, span := p.tracer.Start(ctx, "IngestLogs",
		trace.WithAttributes(attribute.Int("otel.log_record_count", logs.LogRecordCount())))
	defer span.End()

	sessionID := p.store.ActiveSessionID()

	logsSeen := 0
	defer func() { p.tp.addLogs(logsSeen) }()

	for i := 0; i < logs.ResourceLogs().Len(); i++ {
		rl := logs.ResourceLogs().At(i)
		svcName := serviceNameFromAttrs(rl.Resource().Attributes())

		for j := 0; j < rl.ScopeLogs().Len(); j++ {
			sl := rl.ScopeLogs().At(j)
			for k := 0; k < sl.LogRecords().Len(); k++ {
				lr := sl.LogRecords().At(k)

				if !p.sampler.DecideLog() {
					p.sampler.Counters.bumpLogs(1)
					continue
				}

				l := &storage.Log{
					TimestampNs: int64(lr.Timestamp()),
					TraceID:     lr.TraceID().String(),
					SpanID:      lr.SpanID().String(),
					Severity:    int(lr.SeverityNumber()),
					Body:        lr.Body().AsString(),
					Attributes:  mapToJSON(lr.Attributes()),
					ServiceName: svcName,
					SessionID:   sessionID,
					ReceivedAt:  time.Now().UnixNano(),
				}

				if err := p.store.InsertLog(l); err != nil {
					return err
				}
				logsSeen++
				p.hub.Broadcast(ws.NewLogEvent(&ws.LogPayload{
					TraceID:     l.TraceID,
					SpanID:      l.SpanID,
					Severity:    l.Severity,
					Body:        l.Body,
					ServiceName: l.ServiceName,
					SessionID:   l.SessionID,
				}))
			}
		}
	}
	p.ingestCounter.Add(ctx, int64(logsSeen), metric.WithAttributes(attribute.String("signal", "logs")))
	return nil
}

func (p *Pipeline) IngestMetrics(ctx context.Context, md pmetric.Metrics) error {
	ctx, span := p.tracer.Start(ctx, "IngestMetrics",
		trace.WithAttributes(attribute.Int("otel.metric_point_count", md.DataPointCount())))
	defer span.End()
	err := p.ingestMetricsTree(md, p.store.ActiveSessionID())
	p.ingestCounter.Add(ctx, int64(md.DataPointCount()), metric.WithAttributes(attribute.String("signal", "metrics")))
	return err
}

func (p *Pipeline) scheduleDetectors(traceID string) {
	p.debounceMu.Lock()
	defer p.debounceMu.Unlock()
	if t, ok := p.debounce[traceID]; ok {
		t.Reset(500 * time.Millisecond)
		return
	}
	p.debounce[traceID] = time.AfterFunc(500*time.Millisecond, func() {
		runDetectors(traceID, p.store, p.hub)
		p.debounceMu.Lock()
		delete(p.debounce, traceID)
		p.debounceMu.Unlock()
	})
}

func serviceNameFromAttrs(attrs pcommon.Map) string {
	if v, ok := attrs.Get("service.name"); ok {
		return v.AsString()
	}
	return "unknown_service"
}

func mapToJSON(m pcommon.Map) string {
	raw := make(map[string]any, m.Len())
	m.Range(func(k string, v pcommon.Value) bool {
		raw[k] = v.AsRaw()
		return true
	})
	b, _ := json.Marshal(raw)
	return string(b)
}

func extractAttrString(attrsJSON, key string) string {
	var m map[string]any
	if err := json.Unmarshal([]byte(attrsJSON), &m); err != nil {
		return ""
	}
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := [20]byte{}
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[pos:])
}
