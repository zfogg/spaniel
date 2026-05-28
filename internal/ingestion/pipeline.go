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

	debounce     map[string]*time.Timer
	debounceMu   sync.Mutex
	traceBuffers sync.Map

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

				var events []*storage.SpanEvent
				if span.Events().Len() > 0 {
					events = make([]*storage.SpanEvent, 0, span.Events().Len())
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
				}

				var links []*storage.SpanLink
				if span.Links().Len() > 0 {
					links = make([]*storage.SpanLink, 0, span.Links().Len())
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
				}

				if p.sampler.NeedsBuffer() {
					p.bufferSpan(s.TraceID, s, events, links)
					p.scheduleDetectors(s.TraceID)
					continue
				}

				// Fast path: per-span decision.
				if !p.sampler.DecideSpan(s) {
					p.sampler.Counters.bumpSpans(1)
					continue
				}

				if err := p.store.InsertSpan(s); err != nil {
					return err
				}
				spansSeen++
				if len(events) > 0 {
					if err := p.store.InsertSpanEvents(events); err != nil {
						return err
					}
				}
				if len(links) > 0 {
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
		if p.sampler.NeedsBuffer() {
			p.flushBuffer(traceID)
		} else {
			runDetectors(traceID, p.store, p.hub)
		}
		p.debounceMu.Lock()
		delete(p.debounce, traceID)
		p.debounceMu.Unlock()
	})
}

const maxBufferSpansPerTrace = 10_000

type pendingSpan struct {
	span   *storage.Span
	events []*storage.SpanEvent
	links  []*storage.SpanLink
}

type traceBuffer struct {
	mu       sync.Mutex
	entries  []*pendingSpan
	services map[string]struct{}
}

func (p *Pipeline) bufferSpan(traceID string, s *storage.Span, events []*storage.SpanEvent, links []*storage.SpanLink) {
	val, _ := p.traceBuffers.LoadOrStore(traceID, &traceBuffer{services: map[string]struct{}{}})
	buf := val.(*traceBuffer)
	buf.mu.Lock()
	defer buf.mu.Unlock()
	if len(buf.entries) >= maxBufferSpansPerTrace {
		return // fail open: buffer cap reached, drop this span from buffer
	}
	buf.entries = append(buf.entries, &pendingSpan{span: s, events: events, links: links})
	buf.services[s.ServiceName] = struct{}{}
}

func (p *Pipeline) flushBuffer(traceID string) {
	val, ok := p.traceBuffers.LoadAndDelete(traceID)
	if !ok {
		return
	}
	buf := val.(*traceBuffer)
	buf.mu.Lock()
	entries := buf.entries
	services := buf.services
	buf.mu.Unlock()

	if len(entries) == 0 {
		return
	}

	spans := make([]*storage.Span, len(entries))
	for i, e := range entries {
		spans[i] = e.span
	}

	sessionID := p.store.ActiveSessionID()
	now := time.Now().UnixNano()
	issues := detectN1Spans(traceID, sessionID, spans, now)

	serviceP95 := map[string]int64{}
	if p.sampler.alwaysKeep&KeepSlow != 0 {
		for svc := range services {
			if p95, err := p.store.GetServiceP95(svc); err == nil {
				serviceP95[svc] = p95
			}
		}
	}

	if !p.sampler.DecideTrace(spans, len(issues) > 0, serviceP95) {
		p.sampler.Counters.bumpSpans(int64(len(entries)))
		return
	}

	// Accept: store all spans.
	for _, e := range entries {
		if err := p.store.InsertSpan(e.span); err != nil {
			continue
		}
		p.tp.addSpans(1)
		if len(e.events) > 0 {
			_ = p.store.InsertSpanEvents(e.events)
		}
		if len(e.links) > 0 {
			_ = p.store.InsertSpanLinks(e.links)
		}
		go lintSpan(e.span, e.span.SessionID, p.store)
		p.hub.Broadcast(ws.NewSpanEvent(&ws.SpanPayload{
			TraceID:     e.span.TraceID,
			SpanID:      e.span.SpanID,
			ServiceName: e.span.ServiceName,
			Name:        e.span.Name,
			DurationNs:  e.span.EndNs - e.span.StartNs,
			StatusCode:  e.span.StatusCode,
		}))
	}

	// Store N+1 issues and broadcast.
	for _, issue := range issues {
		if err := p.store.UpsertTraceIssue(issue); err == nil {
			p.hub.Broadcast(ws.NewIssueEvent(&ws.IssuePayload{
				TraceID:     issue.TraceID,
				Kind:        issue.Kind,
				Fingerprint: issue.Fingerprint,
				Count:       issue.Count,
				WastedNs:    issue.WastedNs,
			}))
		}
	}
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
