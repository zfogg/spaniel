package storage

import (
	"fmt"
	"testing"
)

func TestListTracesDistinctServices(t *testing.T) {
	db := openTestDB(t)
	for i, service := range []string{"gateway", "gateway", "worker", "database", "worker"} {
		parent := "root"
		if i == 0 {
			parent = ""
		}
		if err := db.InsertSpan(&Span{TraceID: "distributed", SpanID: fmt.Sprint(i),
			ParentSpanID: parent, ServiceName: service, Name: "operation", SessionID: "session-a",
			StartNs: int64(100 + i), EndNs: 200, Attributes: "{}", Resource: "{}"}); err != nil {
			t.Fatal(err)
		}
	}
	// Re-importing the same trace into another session must not inflate counts.
	if err := db.InsertSpan(&Span{TraceID: "distributed", SpanID: "other-session",
		ServiceName: "unrelated", Name: "operation", SessionID: "session-b",
		StartNs: 300, EndNs: 400, Attributes: "{}", Resource: "{}"}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		session, service string
		count, spans     int
	}{{"session-a", "gateway", 3, 5}, {"session-b", "unrelated", 1, 1}} {
		rows, err := db.ListTraces(TraceFilter{SessionID: tc.session, Service: tc.service, Limit: 1})
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 {
			t.Fatalf("%s: got %d traces", tc.session, len(rows))
		}
		row := rows[0]
		if row.ServiceName != tc.service || row.ServiceCount != tc.count || row.SpanCount != tc.spans {
			t.Errorf("%s: got root=%s services=%d spans=%d; want %s/%d/%d",
				tc.session, row.ServiceName, row.ServiceCount, row.SpanCount, tc.service, tc.count, tc.spans)
		}
	}
}
