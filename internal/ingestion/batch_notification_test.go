package ingestion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/zfogg/spaniel/internal/ws"
	"github.com/zfogg/spaniel/internal/storage"
	"go.opentelemetry.io/collector/pdata/pmetric"
)

// Exercise the actual metric path and WebSocket, rather than only checking a
// generic deferred callback. Query invalidation must follow successful flush.
func TestSharedBatchMetricNotificationsFollowCommit(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "committed", true: "failed"}[fail], func(t *testing.T) {
			hub := ws.NewHub()
			conn, server := dialPipelineHub(t, hub)
			defer server.Close()
			defer conn.CloseNow()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			events := make(chan error, 1)
			go func() {
				for {
					_, body, err := conn.Read(ctx)
					if err != nil {
						events <- err
						return
					}
					var event struct {
						Type    string `json:"type"`
						Payload struct {
							Row *storage.Metric `json:"row"`
						} `json:"payload"`
					}
					if err = json.Unmarshal(body, &event); err != nil {
						events <- err
						return
					}
					if event.Type == "metric" {
						if event.Payload.Row == nil || event.Payload.Row.Name != "commit.metric" {
							events <- fmt.Errorf("metric event missing persisted row: %#v", event.Payload.Row)
							return
						}
						events <- nil
						return
					}
				}
			}()
			p := NewPipeline(openTestDB(t), hub)
			p.SetSelfService("batch-metric")
			p.batches.window = time.Millisecond
			entered, allow := make(chan struct{}), make(chan struct{})
			var once sync.Once
			release := func() { once.Do(func() { close(allow) }) }
			defer release()
			failure := errors.New("injected metric flush failure")
			flush := p.batches.flush
			p.batches.flush = func(ctx context.Context, instrument bool) error {
				close(entered)
				<-allow
				if fail {
					return failure
				}
				return flush(ctx, instrument)
			}
			md := pmetric.NewMetrics()
			rm := md.ResourceMetrics().AppendEmpty()
			rm.Resource().Attributes().PutStr("service.name", "batch-metric")
			m := rm.ScopeMetrics().AppendEmpty().Metrics().AppendEmpty()
			m.SetName("commit.metric")
			m.SetEmptyGauge().DataPoints().AppendEmpty().SetDoubleValue(1)
			done := make(chan error, 1)
			go func() { done <- p.IngestMetrics(ctx, md) }()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("metric export never reached flush")
			}
			select {
			case err := <-events:
				t.Fatalf("metric event before shared flush: %v", err)
			case <-time.After(50 * time.Millisecond):
			}
			release()
			if err := <-done; !errors.Is(err, map[bool]error{true: failure, false: nil}[fail]) {
				t.Fatalf("export error: %v", err)
			}
			if fail {
				select {
				case err := <-events:
					t.Fatalf("failed flush published metric: %v", err)
				case <-time.After(50 * time.Millisecond):
				}
			} else {
				select {
				case err := <-events:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("committed metric event missing")
				}
			}
			if err := p.Close(t.Context()); err != nil {
				t.Fatal(err)
			}
		})
	}
}
