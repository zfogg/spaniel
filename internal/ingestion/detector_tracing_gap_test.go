package ingestion

import (
	"testing"
	"time"

	"github.com/zfogg/spaniel/internal/storage"
	"go.opentelemetry.io/otel/trace"
)

func gapSpan(id, parentID string, startMs, endMs int) *storage.Span {
	return &storage.Span{
		SpanID:       id,
		ParentSpanID: parentID,
		StartNs:      int64(startMs) * int64(time.Millisecond),
		EndNs:        int64(endMs) * int64(time.Millisecond),
		SessionID:    "sess",
	}
}

func TestTracingGap_FullyCovered(t *testing.T) {
	// Parent 0–100ms, child 0–100ms → gap = 0.
	spans := []*storage.Span{
		gapSpan("root", "", 0, 100),
		gapSpan("child", "root", 0, 100),
	}
	d := &TracingGapDetector{}
	if got := d.Analyze("t", "s", spans, 0); len(got) != 0 {
		t.Errorf("fully covered parent should not fire, got %d", len(got))
	}
}

func TestTracingGap_SmallGap_NoFire(t *testing.T) {
	// Parent 0–100ms, child 60–100ms → gap = 60ms but only 40ms uncovered, below 50ms threshold.
	spans := []*storage.Span{
		gapSpan("root", "", 0, 100),
		gapSpan("child", "root", 60, 100),
	}
	d := &TracingGapDetector{}
	// uncovered = 60ms; threshold = 50ms → should fire.
	got := d.Analyze("t", "s", spans, 0)
	if len(got) != 1 {
		t.Fatalf("60ms gap should fire, got %d", len(got))
	}
}

func TestTracingGap_BelowThreshold(t *testing.T) {
	// Parent 0–100ms, child 40–100ms → gap = 40ms < 50ms.
	spans := []*storage.Span{
		gapSpan("root", "", 0, 100),
		gapSpan("child", "root", 40, 100),
	}
	d := &TracingGapDetector{}
	if got := d.Analyze("t", "s", spans, 0); len(got) != 0 {
		t.Errorf("40ms gap should not fire, got %d", len(got))
	}
}

func TestTracingGap_LargeGap(t *testing.T) {
	// Parent 0–500ms, child only covers 100–200ms → gap = 400ms.
	spans := []*storage.Span{
		gapSpan("root", "", 0, 500),
		gapSpan("child", "root", 100, 200),
	}
	d := &TracingGapDetector{}
	got := d.Analyze("t", "s", spans, 0)
	if len(got) != 1 {
		t.Fatalf("expected 1 issue for 400ms gap, got %d", len(got))
	}
	if got[0].Kind != "tracing_gap" {
		t.Errorf("wrong kind: %q", got[0].Kind)
	}
	wantGap := int64(400 * time.Millisecond)
	if got[0].WastedNs != wantGap {
		t.Errorf("WastedNs = %d, want %d", got[0].WastedNs, wantGap)
	}
}

func TestTracingGap_NoChildren(t *testing.T) {
	spans := []*storage.Span{gapSpan("root", "", 0, 500)}
	d := &TracingGapDetector{}
	if got := d.Analyze("t", "s", spans, 0); len(got) != 0 {
		t.Errorf("leaf span should not fire, got %d", len(got))
	}
}

func TestTracingGap_UpgradedConnectionIdleDoesNotHideChildGaps(t *testing.T) {
	for _, attrs := range []string{`{"http.response.status_code":101}`, `{"http.status_code":101}`} {
		t.Run(attrs, func(t *testing.T) {
			connection := gapSpan("connection", "", 0, 10000)
			connection.Name, connection.Kind, connection.Attributes = "GET", 2, attrs
			operation := gapSpan("message", "connection", 100, 500)
			query := gapSpan("query", "message", 200, 250)
			got := (&TracingGapDetector{}).Analyze("trace", "sess", []*storage.Span{connection, operation, query}, 0)
			if len(got) != 1 || got[0].ExampleSpanID != "message" || got[0].WastedNs != int64(350*time.Millisecond) {
				t.Fatalf("expected only the real child operation gap, got %+v", got)
			}
		})
	}
}

func TestTracingGap_OrdinaryHTTPRequestStillReportsUncoveredTime(t *testing.T) {
	request := gapSpan("request", "", 0, 10000)
	request.Name, request.Kind, request.Attributes = "GET", 2, `{"http.response.status_code":200}`
	got := (&TracingGapDetector{}).Analyze("trace", "sess", []*storage.Span{request, gapSpan("work", "request", 100, 500)}, 0)
	if len(got) != 1 || got[0].ExampleSpanID != "request" {
		t.Fatalf("ordinary HTTP request gap was hidden: %+v", got)
	}
}

func TestTracingGap_ClientTransportTimeIsNotMissingApplicationWork(t *testing.T) {
	for _, protocol := range []string{"HTTP", "RPC", "database"} {
		t.Run(protocol, func(t *testing.T) {
			client := gapSpan("client", "", 0, 369)
			client.Kind, client.Name, client.ServiceName = int(trace.SpanKindClient), protocol, "caller"
			server := gapSpan("server", "client", 70, 294)
			server.Kind, server.ServiceName = int(trace.SpanKindServer), "remote"
			if got := (&TracingGapDetector{}).Analyze("trace", "sess", []*storage.Span{client, server}, 0); len(got) != 0 {
				t.Fatalf("normal transport/remote latency reported as a gap: %+v", got)
			}
		})
	}
}

func TestTracingGap_ClientDoesNotHideRealServerWorkWithClockSkew(t *testing.T) {
	client := gapSpan("client", "", 0, 1000)
	client.Kind, client.ServiceName = int(trace.SpanKindClient), "caller"
	// The remote clock is five seconds ahead. Its own coverage is still valid.
	server := gapSpan("server", "client", 5100, 5500)
	server.Kind, server.ServiceName = int(trace.SpanKindServer), "remote"
	work := gapSpan("work", "server", 5200, 5250)
	work.Kind, work.ServiceName = int(trace.SpanKindInternal), "remote"
	got := (&TracingGapDetector{}).Analyze("trace", "sess", []*storage.Span{client, server, work}, 0)
	if len(got) != 1 || got[0].ExampleSpanID != "server" || got[0].WastedNs != int64(350*time.Millisecond) {
		t.Fatalf("expected only the real server gap, got %+v", got)
	}
}

func TestTracingGap_InternalWorkAroundClientCallStillReports(t *testing.T) {
	operation := gapSpan("operation", "", 0, 1000)
	operation.Kind = int(trace.SpanKindInternal)
	client := gapSpan("client", "operation", 100, 200)
	client.Kind = int(trace.SpanKindClient)
	got := (&TracingGapDetector{}).Analyze("trace", "sess", []*storage.Span{operation, client}, 0)
	if len(got) != 1 || got[0].ExampleSpanID != "operation" || got[0].WastedNs != int64(900*time.Millisecond) {
		t.Fatalf("expected uncovered application work around the client call, got %+v", got)
	}
}
