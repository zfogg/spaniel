package ingestion

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/zfogg/spaniel/internal/ws"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

// UI reads can occupy every general-purpose SQL connection. OTLP spans with
// events/links must use the reserved appender lane just like bare spans; an
// exception event must not make the rest of a batch queue behind those reads.
func TestIngestMetadataDoesNotWaitForReaders(t *testing.T) {
	for _, self := range []bool{false, true} {
		name := "application"
		if self {
			name = "self"
		}
		t.Run(name, func(t *testing.T) {
			db := openTestDB(t)
			p := NewPipeline(db, ws.NewHub())
			// This regression isolates reserved SQL connections from collection latency.
			p.batches.window = 10 * time.Millisecond
			if self {
				p.SetSelfService("metadata-pressure")
			}
			td := makeTraces("metadata-pressure", func(ss ptrace.ScopeSpans) {
				for i := range 32 {
					putSpan(ss, "m", string(rune('A'+i)), "", "observed operation", ptrace.SpanKindInternal, nil)
					s := ss.Spans().At(i)
					e := s.Events().AppendEmpty()
					e.SetName("exception")
					e.SetTimestamp(pcommon.Timestamp(1_001_000_000))
					e.Attributes().PutStr("exception.message", "must survive reader pressure")
					l := s.Links().AppendEmpty()
					l.SetTraceID(s.TraceID())
					l.SetSpanID(s.SpanID())
					l.TraceState().FromRaw("vendor=value")
					l.Attributes().PutStr("causal", "yes")
				}
			})

			pool := db.SQL()
			available := pool.Stats().MaxOpenConnections - pool.Stats().InUse
			var held []*sql.Conn
			release := func() {
				for _, c := range held {
					_ = c.Close()
				}
				held = nil
			}
			defer release()
			for range available {
				c, err := pool.Conn(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				held = append(held, c)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- p.IngestTraces(ctx, td) }()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(2 * time.Second):
				cancel()
				release()
				<-done
				t.Fatal("span events/links waited for occupied reader connections")
			}
			release()
			spans, err := db.GetTrace(td.ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(0).TraceID().String())
			if err != nil || len(spans) != 32 {
				t.Fatalf("durable spans: %d, error: %v", len(spans), err)
			}
			for _, s := range spans {
				events, err := db.ListEventsBySpan(s.SpanID)
				if err != nil || len(events) != 1 || events[0].Name != "exception" {
					t.Fatalf("durable events for %s: %v, error: %v", s.SpanID, events, err)
				}
				links, err := db.ListLinksBySpan(s.SpanID)
				if err != nil || len(links) != 1 || links[0].TraceState != "vendor=value" {
					t.Fatalf("durable links for %s: %v, error: %v", s.SpanID, links, err)
				}
			}
		})
	}
}
