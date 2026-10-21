package ingestion

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/zfogg/spaniel/internal/ws"
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
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				// There is no store: already canceled exports must not touch it.
				if err := ingest(&Pipeline{}, ctx); !errors.Is(err, context.Canceled) {
					t.Fatalf("error=%v", err)
				}
			})
			t.Run("cancelled while queued", func(t *testing.T) {
				p := NewPipeline(openTestDB(t), ws.NewHub())
				p.batches.window = time.Hour
				t.Cleanup(func() { _ = p.Close(context.Background()) })
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				done := make(chan error, 1)
				go func() { done <- ingest(p, ctx) }()
				deadline := time.Now().Add(5 * time.Second)
				for {
					p.batches.mu.Lock()
					n := p.batches.requests
					p.batches.mu.Unlock()
					if n == 1 {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("export never queued")
					}
					runtime.Gosched()
				}
				cancel()
				select {
				case err := <-done:
					if !errors.Is(err, context.Canceled) {
						t.Fatalf("error=%v", err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("canceled exporter remains queued")
				}
				p.batches.mu.Lock()
				defer p.batches.mu.Unlock()
				if p.batches.requests != 0 || p.batches.bytes != 0 {
					t.Fatal("canceled export retained capacity")
				}
			})
		})
	}
}

func TestIngestAdmissionReleasesAcceptedCapacity(t *testing.T) {
	p := NewPipeline(openTestDB(t), ws.NewHub())
	p.batches.window = time.Millisecond
	defer p.Close(context.Background())
	if err := p.IngestLogs(t.Context(), plog.NewLogs()); err != nil {
		t.Fatal(err)
	}
	p.batches.mu.Lock()
	defer p.batches.mu.Unlock()
	if p.batches.requests != 0 || p.batches.bytes != 0 {
		t.Fatal("accepted export retained capacity")
	}
}
