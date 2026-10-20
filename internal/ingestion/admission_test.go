package ingestion

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"

	"github.com/zfogg/spaniel/internal/storage"

	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

func TestQueuedIngestHonorsCancellation(t *testing.T) {
	for _, signal := range []string{"traces", "logs", "metrics"} {
		t.Run(signal, func(t *testing.T) {
			store, err := storage.Open(":memory:")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { store.Close() })
			synctest.Test(t, func(t *testing.T) {
				p := &Pipeline{store: store, ingestSlots: make(chan struct{}, 1)}
				p.ingestSlots <- struct{}{}
				ctx, cancel := context.WithCancel(t.Context())
				done := make(chan error, 1)
				go func() {
					switch signal {
					case "traces":
						done <- p.IngestTraces(ctx, ptrace.NewTraces())
					case "logs":
						done <- p.IngestLogs(ctx, plog.NewLogs())
					case "metrics":
						done <- p.IngestMetrics(ctx, pmetric.NewMetrics())
					}
				}()
				synctest.Wait()
				if p.ingestPending.Load() != 1 {
					t.Fatal("request did not queue")
				}
				cancel()
				synctest.Wait()
				if err := <-done; !errors.Is(err, context.Canceled) {
					t.Fatalf("got %v", err)
				}
				if p.ingestPending.Load() != 0 || len(p.ingestSlots) != 1 {
					t.Fatal("canceled request leaked accounting or consumed a slot")
				}
				<-p.ingestSlots
				release, err := p.beginIngest(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				release()
				if p.ingestPending.Load() != 0 {
					t.Fatal("admission did not recover")
				}
			})
		})
	}
}
