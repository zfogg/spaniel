package storage

import (
	"fmt"
	"strings"
	"testing"
)

func TestScopedTraceRetainsSessionQueryOccurrences(t *testing.T) {
	d := openTestDB(t)
	attrs := `{"gotel.db.query.fingerprint":"sequel:v1:postgresql:` + strings.Repeat("a", 64) + `"}`
	for session, count := range map[string]int{"session-a": 2, "session-b": 3} {
		for i := range count {
			spanID := fmt.Sprintf("%s-%d", session, i)
			if err := d.InsertSpan(&Span{TraceID: "shared-trace", SpanID: spanID, SessionID: session, ServiceName: "api", Name: "query", Attributes: attrs, Resource: "{}", StartNs: int64(100 + i), EndNs: 200}); err != nil {
				t.Fatal(err)
			}
			if err := d.InsertSpanEvents([]*SpanEvent{{TraceID: "shared-trace", SpanID: spanID, SessionID: session, TimeNs: int64(100 + i), Name: "query event", Attributes: "{}"}}); err != nil {
				t.Fatal(err)
			}
		}
	}
	for session, count := range map[string]int{"session-a": 2, "session-b": 3, "missing": 0} {
		rows, err := d.GetTrace("shared-trace", session)
		if err != nil || len(rows) != count {
			t.Fatalf("session %s: rows=%d want=%d err=%v", session, len(rows), count, err)
		}
		for _, row := range rows {
			if row.SessionID != session || row.QueryOccurrences != int64(count) {
				t.Fatalf("scoped trace lost session or occurrence population: %+v", row)
			}
		}
		events, err := d.ListEventsByTrace("shared-trace", session)
		if err != nil || len(events) != count {
			t.Fatalf("scoped events %s: rows=%d want=%d err=%v", session, len(events), count, err)
		}
		for _, event := range events {
			if event.SessionID != session {
				t.Fatalf("event escaped recording scope: %+v", event)
			}
		}
	}
	rows, err := d.GetTrace("shared-trace")
	if err != nil || len(rows) != 5 {
		t.Fatalf("unscoped compatibility: rows=%d err=%v", len(rows), err)
	}
	events, err := d.ListEventsByTrace("shared-trace")
	if err != nil || len(events) != 5 {
		t.Fatalf("unscoped event compatibility: rows=%d err=%v", len(events), err)
	}
}
