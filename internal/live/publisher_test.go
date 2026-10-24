package live

import (
	"context"
	"github.com/coder/websocket"
	"github.com/zfogg/spaniel/internal/storage"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zfogg/spaniel/internal/ws"
)

func TestPublisherDoesNotQueryWithoutViewers(t *testing.T) {
	// No DB is necessary: a publisher without consumers must never access it.
	p := NewPublisher(nil, ws.NewHub(), nil, nil)
	p.Notify()
	if p.timer != nil {
		t.Fatal("scheduled a database summary without viewers")
	}
	// A previously scheduled timer must also stop when its last viewer leaves.
	p.timer = time.NewTimer(time.Hour)
	p.timer.Stop()
	p.publish()
	if p.timer != nil {
		t.Fatal("retained a summary timer without viewers")
	}
}

func TestPublisherGroupsSnapshotQueries(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous); _ = provider.Shutdown(context.Background()) })
	db, err := storage.Open(filepath.Join(t.TempDir(), "live.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	hub := ws.NewHub()
	server := httptest.NewServer(http.HandlerFunc(hub.ServeWS))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	for !hub.HasViewers() {
		select {
		case <-ctx.Done():
			t.Fatal("viewer not registered")
		case <-time.After(time.Millisecond):
		}
	}
	before := len(recorder.Ended())
	NewPublisher(db, hub, nil, nil).publish()
	var root sdktrace.ReadOnlySpan
	for _, span := range recorder.Ended()[before:] {
		if span.Name() == "spaniel.live.publish" {
			root = span
		}
	}
	if root == nil {
		t.Fatal("missing snapshot root")
	}
	count := 0
	for _, span := range recorder.Ended()[before:] {
		if strings.HasPrefix(span.Name(), "spaniel.storage.") {
			count++
			if span.Parent().SpanID() != root.SpanContext().SpanID() {
				t.Fatalf("orphan snapshot query %s", span.Name())
			}
		}
	}
	if count < 2 {
		t.Fatalf("expected stats/source queries; got %d", count)
	}
}
