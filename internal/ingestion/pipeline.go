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

	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/ws"
)

type Pipeline struct {
	store *storage.DB
	hub   *ws.Hub

	debounce   map[string]*time.Timer
	debounceMu sync.Mutex
}

func NewPipeline(store *storage.DB, hub *ws.Hub) *Pipeline {
	return &Pipeline{
		store:    store,
		hub:      hub,
		debounce: make(map[string]*time.Timer),
	}
}

func (p *Pipeline) IngestTraces(ctx context.Context, traces ptrace.Traces) error {
	sessionID := p.store.ActiveSessionID()

	sessionLabel := p.store.ActiveSessionLabel()

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
				}

				if err := p.store.InsertSpan(s); err != nil {
					return err
				}

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

				p.hub.Broadcast(&ws.SpanEvent{
					Type:        "span",
					TraceID:     s.TraceID,
					SpanID:      s.SpanID,
					ServiceName: s.ServiceName,
					Name:        s.Name,
					DurationNs:  s.EndNs - s.StartNs,
					StatusCode:  s.StatusCode,
				})

				p.scheduleDetectors(s.TraceID)
			}
		}
	}
	return nil
}

func (p *Pipeline) IngestLogs(ctx context.Context, logs plog.Logs) error {
	sessionID := p.store.ActiveSessionID()

	for i := 0; i < logs.ResourceLogs().Len(); i++ {
		rl := logs.ResourceLogs().At(i)
		svcName := serviceNameFromAttrs(rl.Resource().Attributes())

		for j := 0; j < rl.ScopeLogs().Len(); j++ {
			sl := rl.ScopeLogs().At(j)
			for k := 0; k < sl.LogRecords().Len(); k++ {
				lr := sl.LogRecords().At(k)

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
			}
		}
	}
	return nil
}

func (p *Pipeline) IngestMetrics(_ context.Context, md pmetric.Metrics) error {
	return p.ingestMetricsTree(md, p.store.ActiveSessionID())
}

func (p *Pipeline) scheduleDetectors(traceID string) {
	p.debounceMu.Lock()
	defer p.debounceMu.Unlock()
	if t, ok := p.debounce[traceID]; ok {
		t.Reset(500 * time.Millisecond)
		return
	}
	p.debounce[traceID] = time.AfterFunc(500*time.Millisecond, func() {
		runDetectors(traceID, p.store)
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
