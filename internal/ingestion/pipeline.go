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

				p.scheduleN1Check(s.TraceID)
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

func (p *Pipeline) IngestMetrics(_ context.Context, _ pmetric.Metrics) error {
	// metrics storage is a future milestone; accept and discard for now
	return nil
}

func (p *Pipeline) scheduleN1Check(traceID string) {
	p.debounceMu.Lock()
	defer p.debounceMu.Unlock()
	if t, ok := p.debounce[traceID]; ok {
		t.Reset(500 * time.Millisecond)
		return
	}
	p.debounce[traceID] = time.AfterFunc(500*time.Millisecond, func() {
		p.runN1Check(traceID)
		p.debounceMu.Lock()
		delete(p.debounce, traceID)
		p.debounceMu.Unlock()
	})
}

func (p *Pipeline) runN1Check(traceID string) {
	spans, err := p.store.GetTrace(traceID)
	if err != nil || len(spans) == 0 {
		return
	}
	// Count DB spans grouped by statement to detect N+1
	stmtCount := make(map[string]int)
	stmtSpan := make(map[string]*storage.Span)
	for _, s := range spans {
		if !findSubstr(s.Attributes, `"db.statement"`) {
			continue
		}
		stmt := extractAttrString(s.Attributes, "db.statement")
		stmtCount[stmt]++
		stmtSpan[stmt] = s
	}
	sessionID := p.store.ActiveSessionID()
	for stmt, count := range stmtCount {
		if count >= 5 {
			s := stmtSpan[stmt]
			_ = p.store.InsertLintWarning(&storage.LintWarning{
				SpanID:    s.SpanID,
				TraceID:   traceID,
				SessionID: sessionID,
				RuleID:    "n_plus_one",
				Message:   "Potential N+1: same DB statement executed " + itoa(count) + " times in one trace",
				Severity:  "warning",
				CreatedAt: time.Now().UnixNano(),
			})
			break
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
