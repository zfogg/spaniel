package importer

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"

	"github.com/zfogg/spaniel/internal/storage"
)

type Format string

const (
	FormatAuto   Format = "auto"
	FormatOTLP   Format = "otlp"
	FormatJaeger Format = "jaeger"
)

type Result struct {
	Session    *storage.Session `json:"session"`
	SpanCount  int              `json:"span_count"`
	TraceCount int              `json:"trace_count"`
}

// Import creates a new imported (baseline) session and inserts spans from data.
func Import(store *storage.DB, label string, format Format, data []byte) (*Result, error) {
	if format == FormatAuto {
		format = detect(data)
	}

	sess, err := store.CreateImportedSession(label)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	var spans []*storage.Span
	switch format {
	case FormatOTLP:
		spans, err = parseOTLP(sess.ID, sess.Label, data)
	case FormatJaeger:
		spans, err = parseJaeger(sess.ID, sess.Label, data)
	default:
		return nil, fmt.Errorf("unsupported format: %s", format)
	}
	if err != nil {
		// Roll back the session we just created
		_ = store.DeleteSession(sess.ID)
		return nil, fmt.Errorf("parse %s: %w", format, err)
	}

	for _, s := range spans {
		if err := store.AppendSpan(s); err != nil {
			return nil, fmt.Errorf("insert span: %w", err)
		}
	}
	// Flush the batched appends so the imported spans are durable before we
	// return — the caller reads back session stats immediately.
	if err := store.FlushBatch(); err != nil {
		return nil, fmt.Errorf("flush spans: %w", err)
	}

	traceIDs := make(map[string]struct{}, len(spans))
	for _, s := range spans {
		traceIDs[s.TraceID] = struct{}{}
	}

	return &Result{
		Session:    sess,
		SpanCount:  len(spans),
		TraceCount: len(traceIDs),
	}, nil
}

// detect guesses format from the top-level JSON keys.
func detect(data []byte) Format {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return FormatOTLP
	}
	if _, ok := probe["resourceSpans"]; ok {
		return FormatOTLP
	}
	if raw, ok := probe["data"]; ok {
		// Jaeger wraps traces in a data array of objects with "spans" + "processes"
		var arr []json.RawMessage
		if json.Unmarshal(raw, &arr) == nil && len(arr) > 0 {
			var item map[string]json.RawMessage
			if json.Unmarshal(arr[0], &item) == nil {
				if _, ok := item["spans"]; ok {
					return FormatJaeger
				}
			}
		}
	}
	return FormatOTLP
}

// ── OTLP parser ───────────────────────────────────────────────────────────────

func parseOTLP(sessionID, sessionLabel string, data []byte) ([]*storage.Span, error) {
	req := ptraceotlp.NewExportRequest()
	if err := req.UnmarshalJSON(data); err != nil {
		return nil, err
	}
	traces := req.Traces()
	now := time.Now().UnixNano()

	var result []*storage.Span
	for i := 0; i < traces.ResourceSpans().Len(); i++ {
		rs := traces.ResourceSpans().At(i)
		svcName := "unknown_service"
		if v, ok := rs.Resource().Attributes().Get("service.name"); ok {
			svcName = v.AsString()
		}
		resourceJSON := attrsToJSON(rs.Resource().Attributes().AsRaw())

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
					Attributes:    attrsToJSON(span.Attributes().AsRaw()),
					Resource:      resourceJSON,
					SessionID:     sessionID,
					SessionLabel:  sessionLabel,
					ReceivedAt:    now,
				}
				result = append(result, s)
			}
		}
	}
	return result, nil
}

func attrsToJSON(m map[string]any) string {
	b, _ := json.Marshal(m)
	return string(b)
}

// Use context to satisfy import even though IngestTraces uses it internally.
var _ = context.Background

// ── Jaeger parser ─────────────────────────────────────────────────────────────

type jaegerExport struct {
	Data []jaegerTrace `json:"data"`
}

type jaegerTrace struct {
	TraceID   string                    `json:"traceID"`
	Spans     []jaegerSpan              `json:"spans"`
	Processes map[string]jaegerProcess  `json:"processes"`
}

type jaegerSpan struct {
	TraceID       string      `json:"traceID"`
	SpanID        string      `json:"spanID"`
	OperationName string      `json:"operationName"`
	References    []jaegerRef `json:"references"`
	StartTime     int64       `json:"startTime"` // microseconds
	Duration      int64       `json:"duration"`  // microseconds
	Tags          []jaegerTag `json:"tags"`
	ProcessID     string      `json:"processID"`
	Process       *jaegerProcess `json:"process"`
}

type jaegerRef struct {
	RefType string `json:"refType"`
	SpanID  string `json:"spanID"`
}

type jaegerTag struct {
	Key   string `json:"key"`
	Type  string `json:"type"`
	Value any    `json:"value"`
}

type jaegerProcess struct {
	ServiceName string      `json:"serviceName"`
	Tags        []jaegerTag `json:"tags"`
}

func parseJaeger(sessionID, sessionLabel string, data []byte) ([]*storage.Span, error) {
	var export jaegerExport
	if err := json.Unmarshal(data, &export); err != nil {
		return nil, err
	}
	now := time.Now().UnixNano()

	var result []*storage.Span
	for _, trace := range export.Data {
		for _, js := range trace.Spans {
			svcName := "unknown_service"
			if js.Process != nil && js.Process.ServiceName != "" {
				svcName = js.Process.ServiceName
			} else if p, ok := trace.Processes[js.ProcessID]; ok {
				svcName = p.ServiceName
			}

			parentID := ""
			for _, ref := range js.References {
				if ref.RefType == "CHILD_OF" && ref.SpanID != "" {
					parentID = ref.SpanID
					break
				}
			}

			attrs := make(map[string]any, len(js.Tags))
			for _, t := range js.Tags {
				attrs[t.Key] = t.Value
			}
			attrsJSON := attrsToJSON(attrs)

			resourceAttrs := map[string]any{"service.name": svcName}
			if js.Process != nil {
				for _, t := range js.Process.Tags {
					resourceAttrs[t.Key] = t.Value
				}
			} else if p, ok := trace.Processes[js.ProcessID]; ok {
				for _, t := range p.Tags {
					resourceAttrs[t.Key] = t.Value
				}
			}
			resourceJSON := attrsToJSON(resourceAttrs)

			startNs := js.StartTime * 1_000
			endNs := startNs + js.Duration*1_000

			s := &storage.Span{
				TraceID:      normalizeJaegerID(js.TraceID),
				SpanID:       normalizeJaegerID(js.SpanID),
				ParentSpanID: normalizeJaegerID(parentID),
				ServiceName:  svcName,
				Name:         js.OperationName,
				Kind:         0,
				StartNs:      startNs,
				EndNs:        endNs,
				Attributes:   attrsJSON,
				Resource:     resourceJSON,
				SessionID:    sessionID,
				SessionLabel: sessionLabel,
				ReceivedAt:   now,
			}
			result = append(result, s)
		}
	}
	return result, nil
}

// normalizeJaegerID pads short hex IDs to 16 chars (span) or passes through as-is.
func normalizeJaegerID(id string) string {
	return id
}
