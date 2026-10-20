package ingestion

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"

	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

func TestIngestAdmissionHonorsCancellationForEverySignal(t *testing.T) {
	signals := map[string]func(*Pipeline, context.Context) error{
		"traces":  func(p *Pipeline, ctx context.Context) error { return p.IngestTraces(ctx, ptrace.NewTraces()) },
		"logs":    func(p *Pipeline, ctx context.Context) error { return p.IngestLogs(ctx, plog.NewLogs()) },
		"metrics": func(p *Pipeline, ctx context.Context) error { return p.IngestMetrics(ctx, pmetric.NewMetrics()) },
	}
	for name, ingest := range signals {
		t.Run(name, func(t *testing.T) {
			t.Run("already cancelled", func(t *testing.T) {
				p := &Pipeline{ingestSlots: make(chan struct{}, 1)}
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				// There is deliberately no store: canceled work must never touch it.
				if err := ingest(p, ctx); !errors.Is(err, context.Canceled) {
					t.Fatalf("error=%v", err)
				}
				if p.ingestPending.Load() != 0 || len(p.ingestSlots) != 0 {
					t.Fatal("canceled admission leaked capacity")
				}
			})
			t.Run("cancelled while queued", func(t *testing.T) {
				p := &Pipeline{ingestSlots: make(chan struct{}, 1)}
				p.ingestSlots <- struct{}{}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				done := make(chan error, 1)
				go func() { done <- ingest(p, ctx) }()
				deadline := time.After(5 * time.Second)
				for p.ingestPending.Load() != 1 {
					select {
					case err := <-done:
						t.Fatalf("ingest left before cancellation: %v", err)
					case <-deadline:
						t.Fatal("ingest never queued")
					default:
						runtime.Gosched()
					}
				}
				cancel()
				select {
				case err := <-done:
					if !errors.Is(err, context.Canceled) {
						t.Fatalf("error=%v", err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("canceled exporter remains blocked behind admission")
				}
				if p.ingestPending.Load() != 0 || len(p.ingestSlots) != 1 {
					t.Fatal("canceled waiter leaked pending count or released another request's slot")
				}
			})
		})
	}
}

func TestIngestAdmissionReleasesAcceptedCapacity(t *testing.T) {
	p := &Pipeline{ingestSlots: make(chan struct{}, 1)}
	finish, err := p.beginIngest(t.Context())
	if err != nil || finish == nil || p.ingestPending.Load() != 1 || len(p.ingestSlots) != 1 {
		t.Fatalf("admission error=%v", err)
	}
	finish()
	if p.ingestPending.Load() != 0 || len(p.ingestSlots) != 0 {
		t.Fatal("accepted request leaked capacity")
	}
}
