package storage

import (
	"path/filepath"
	"testing"
)

func TestBatcherMetadataSurvivesMaintenanceAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metadata.duckdb")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	appendMetadata := func(id string) {
		t.Helper()
		if err := db.AppendSpan(&Span{TraceID: "metadata", SpanID: id, Name: "with metadata", StartNs: 10, EndNs: 20, Attributes: "{}", Resource: "{}", SessionID: "session"}); err != nil {
			t.Fatal(err)
		}
		if err := db.AppendSpanEvents([]*SpanEvent{{SpanID: id, TraceID: "metadata", SessionID: "session", TimeNs: 12, Name: "exception", Attributes: `{"message":"persist me","nested":{"count":2}}`}}); err != nil {
			t.Fatal(err)
		}
		if err := db.AppendSpanLinks([]*SpanLink{{SpanID: id, TraceID: "metadata", SessionID: "session", LinkedTraceID: "producer", LinkedSpanID: "origin", TraceState: "vendor=value", Attributes: `{"causal":true}`}}); err != nil {
			t.Fatal(err)
		}
	}
	appendMetadata("before")
	// Closes, flushes and reopens every pinned appender, including metadata.
	if err := db.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	appendMetadata("after")
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"before", "after"} {
		events, err := db.ListEventsBySpan(id)
		if err != nil || len(events) != 1 {
			t.Fatalf("events %s: %v, %v", id, events, err)
		}
		if events[0].Attributes != `{"message":"persist me","nested":{"count":2}}` || events[0].TimeNs != 12 || events[0].SessionID != "session" {
			t.Fatalf("event round trip: %+v", events[0])
		}
		links, err := db.ListIncomingLinks("producer")
		if err != nil || len(links) != 2 {
			t.Fatalf("links: %v, %v", links, err)
		}
		for _, l := range links {
			if l.Attributes != `{"causal":true}` || l.TraceState != "vendor=value" || l.LinkedSpanID != "origin" {
				t.Fatalf("link round trip: %+v", l)
			}
		}
	}
}
