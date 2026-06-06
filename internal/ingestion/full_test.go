package ingestion

import (
	"context"
	"errors"
	"testing"

	"go.opentelemetry.io/collector/pdata/ptrace"

	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/ws"
)

// TestIngestTraces_RejectsWhenFull verifies that when storage is full, ingestion
// returns ErrStorageFull (→ 503) and stores nothing.
func TestIngestTraces_RejectsWhenFull(t *testing.T) {
	db := openTestDB(t)
	sess, _ := db.CreateSession("s", false)
	db.SetActiveSession(sess.ID, sess.Label)
	db.SetFull(true)

	p := NewPipeline(db, ws.NewHub())
	td := makeTraces("app", func(ss ptrace.ScopeSpans) {
		putSpan(ss, "a", "1", "", "GET /x", ptrace.SpanKindServer, nil)
	})

	err := p.IngestTraces(context.Background(), td)
	if !errors.Is(err, storage.ErrStorageFull) {
		t.Fatalf("expected ErrStorageFull, got %v", err)
	}

	tid := td.ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(0).TraceID().String()
	if spans, _ := db.GetTrace(tid); len(spans) != 0 {
		t.Errorf("expected nothing stored while full, got %d spans", len(spans))
	}
}

// Self-telemetry is best-effort and still stored even when full.
func TestIngestTraces_SelfAllowedWhenFull(t *testing.T) {
	db := openTestDB(t)
	sess, _ := db.CreateSession("s", false)
	db.SetActiveSession(sess.ID, sess.Label)
	db.SetFull(true)

	p := NewPipeline(db, ws.NewHub())
	p.SetSelfService("spaniel")
	td := makeTraces("spaniel", func(ss ptrace.ScopeSpans) {
		putSpan(ss, "a", "1", "", "self", ptrace.SpanKindServer, nil)
	})
	if err := p.IngestTraces(context.Background(), td); err != nil {
		t.Fatalf("self ingest while full should succeed, got %v", err)
	}
}
