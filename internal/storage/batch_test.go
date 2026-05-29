package storage

import (
	"fmt"
	"sync"
	"testing"
)

// TestBatcherAppendFlushRoundTrip verifies that spans/logs/metrics appended via
// the Appender become visible after a flush, with JSON columns stored verbatim
// (not double-encoded) and the generated duration_ns computed.
func TestBatcherAppendFlushRoundTrip(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := db.AppendSpan(&Span{
		TraceID: "t1", SpanID: "s1", ServiceName: "svc", Name: "op",
		Kind: 2, StartNs: 100, EndNs: 250, StatusCode: 2, StatusMessage: "boom",
		Attributes: `{"db.system":"postgresql"}`, Resource: `{"service.name":"svc"}`,
		SessionID: "sess", SessionLabel: "lbl", ReceivedAt: 999, Sampled: true,
	}); err != nil {
		t.Fatalf("AppendSpan: %v", err)
	}
	if err := db.AppendLog(&Log{
		TimestampNs: 5, TraceID: "t1", SpanID: "s1", Severity: 9, Body: "hello",
		Attributes: `{"k":"v"}`, ServiceName: "svc", SessionID: "sess", ReceivedAt: 1,
	}); err != nil {
		t.Fatalf("AppendLog: %v", err)
	}
	if err := db.AppendMetric(&Metric{
		Name: "http.latency", Type: "gauge", TimestampNs: 7, Value: 1.5,
		Attributes: `{"route":"/x"}`, Exemplars: `[]`, ServiceName: "svc", SessionID: "sess",
	}); err != nil {
		t.Fatalf("AppendMetric: %v", err)
	}

	// Before flush: not yet durable (nothing should be readable). We don't assert
	// emptiness (timing), but after flush the rows must be present.
	if err := db.FlushBatch(); err != nil {
		t.Fatalf("FlushBatch: %v", err)
	}

	spans, err := db.GetTrace("t1")
	if err != nil || len(spans) != 1 {
		t.Fatalf("GetTrace: err=%v rows=%d", err, len(spans))
	}
	s := spans[0]
	if s.DurationNs != 150 {
		t.Errorf("duration_ns = %d, want 150 (generated)", s.DurationNs)
	}
	if s.Attributes != `{"db.system":"postgresql"}` {
		t.Errorf("attributes round-trip corrupted: %q", s.Attributes)
	}
	if s.Resource != `{"service.name":"svc"}` {
		t.Errorf("resource round-trip corrupted: %q", s.Resource)
	}
	if s.StatusCode != 2 || s.StatusMessage != "boom" {
		t.Errorf("status not preserved: code=%d msg=%q", s.StatusCode, s.StatusMessage)
	}

	logs, err := db.ListLogs(LogFilter{TraceID: "t1"})
	if err != nil || len(logs) != 1 {
		t.Fatalf("ListLogs: err=%v rows=%d", err, len(logs))
	}
	if logs[0].Body != "hello" || logs[0].Attributes != `{"k":"v"}` {
		t.Errorf("log round-trip: body=%q attrs=%q", logs[0].Body, logs[0].Attributes)
	}

	series, err := db.GetMetricSeries(MetricSeriesFilter{Name: "http.latency"})
	if err != nil || len(series) != 1 {
		t.Fatalf("GetMetricSeries: err=%v rows=%d", err, len(series))
	}
	if series[0].Value != 1.5 || series[0].Attributes != `{"route":"/x"}` {
		t.Errorf("metric round-trip: value=%v attrs=%q", series[0].Value, series[0].Attributes)
	}
}

// TestBatcherFlushesAtRowThreshold checks that appending past batchMaxRows
// auto-flushes without an explicit FlushBatch call.
func TestBatcherFlushesAtRowThreshold(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	for i := range batchMaxRows + 10 {
		if err := db.AppendSpan(&Span{
			TraceID: "tt", SpanID: fmt.Sprintf("s%d", i), Name: "op",
			StartNs: 0, EndNs: 1, Attributes: "{}", Resource: "{}", SessionID: "sess",
		}); err != nil {
			t.Fatalf("AppendSpan %d: %v", i, err)
		}
	}
	// The first batchMaxRows rows must already be durable from the threshold
	// flush, even though we never called FlushBatch.
	spans, err := db.GetTrace("tt")
	if err != nil {
		t.Fatalf("GetTrace: %v", err)
	}
	if len(spans) < batchMaxRows {
		t.Fatalf("expected >= %d spans durable from threshold flush, got %d", batchMaxRows, len(spans))
	}
}

// TestBatcherConcurrentAppend exercises concurrent appends from multiple
// goroutines (the HTTP + gRPC receivers run in parallel) under -race.
func TestBatcherConcurrentAppend(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	const goroutines, perG = 8, 200
	var wg sync.WaitGroup
	for g := range goroutines {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := range perG {
				_ = db.AppendSpan(&Span{
					TraceID: "race", SpanID: fmt.Sprintf("g%d-%d", g, i), Name: "op",
					StartNs: 0, EndNs: 1, Attributes: "{}", Resource: "{}", SessionID: "sess",
				})
			}
		}(g)
	}
	wg.Wait()
	if err := db.FlushBatch(); err != nil {
		t.Fatalf("FlushBatch: %v", err)
	}
	spans, err := db.GetTrace("race")
	if err != nil {
		t.Fatalf("GetTrace: %v", err)
	}
	if len(spans) != goroutines*perG {
		t.Fatalf("expected %d spans, got %d", goroutines*perG, len(spans))
	}
}
