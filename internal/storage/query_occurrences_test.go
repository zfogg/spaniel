package storage

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestQueryOccurrencesSkipsMalformedAttributes(t *testing.T) {
	d := openTestDB(t)
	fp := "sequel:v1:postgresql:" + strings.Repeat("a", 64)
	for _, s := range []*Span{
		{TraceID: "t1", SpanID: "good", SessionID: "s", ServiceName: "svc", Attributes: `{"gotel.db.query.fingerprint":"` + fp + `"}`, Resource: "{}"},
		{TraceID: "t2", SpanID: "bad", SessionID: "s", ServiceName: "svc", Attributes: `{"bad":`, Resource: "{}"},
	} {
		if err := d.InsertSpan(s); err != nil {
			t.Fatal(err)
		}
	}
	bare, err := d.GetTraceInSessionForDetection("t1", "s")
	if err != nil || len(bare) != 1 || bare[0].QueryOccurrences != 0 {
		t.Fatalf("detection trace: %#v, %v", bare, err)
	}
	got, err := d.GetTraceInSession("t1", "s")
	if err != nil || len(got) != 1 || got[0].QueryOccurrences != 1 {
		t.Fatalf("enriched trace: %#v, %v", got, err)
	}
	got, err = d.GetTraceInSession("t2", "s")
	if err != nil || len(got) != 1 || got[0].QueryOccurrences != 0 {
		t.Fatalf("malformed trace: %#v, %v", got, err)
	}
}

func TestSessionQueryOccurrencesScopePaginationAndRetransmits(t *testing.T) {
	d := openTestDB(t)
	fp := "sequel:v1:postgresql:" + strings.Repeat("a", 64)
	insert := func(trace, span, session, service, name string, attrs map[string]any) {
		t.Helper()
		data, err := json.Marshal(attrs)
		if err != nil {
			t.Fatal(err)
		}
		if err = d.InsertSpan(&Span{TraceID: trace, SpanID: span, SessionID: session, ServiceName: service, Name: name, Attributes: string(data), Resource: "{}", StartNs: 100, EndNs: 200}); err != nil {
			t.Fatal(err)
		}
	}
	base := func() map[string]any {
		return map[string]any{"gotel.db.query.fingerprint": fp, "db.namespace": "app", "server.address": "db", "server.port": 5432}
	}
	for i := range 5 {
		insert(fmt.Sprint("trace-", i), fmt.Sprint("span-", i), "session-a", "api", []string{"visible", "visible", "hidden", "hidden", "hidden"}[i], base())
	}
	insert("trace-0", "span-0", "session-a", "api", "visible", base()) // retransmission, not another execution
	insert("other-session", "s-other", "session-b", "api", "name-0", base())
	insert("other-service", "svc-other", "session-a", "worker", "name-0", base())
	for i, change := range []func(map[string]any){
		func(a map[string]any) { a["db.namespace"] = "other" },
		func(a map[string]any) { a["server.address"] = "other" },
		func(a map[string]any) { a["server.port"] = 5433 },
		func(a map[string]any) {
			a["gotel.db.query.fingerprint"] = "sequel:v1:sqlite:" + strings.Repeat("a", 64)
		},
		func(a map[string]any) {
			a["gotel.db.query.fingerprint"] = "sequel:v2:postgresql:" + strings.Repeat("a", 64)
		},
	} {
		a := base()
		change(a)
		insert(fmt.Sprint("different-", i), fmt.Sprint("different-", i), "session-a", "api", "name-0", a)
	}
	// Display filters and pages select rows, not the session-wide population.
	for page := 1; page <= 2; page++ {
		rows, err := d.ListSpans(SpanFilter{SessionID: "session-a", Service: "api", Name: "visible", Limit: 1, Page: page})
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 {
			t.Fatalf("page %d has %d rows", page, len(rows))
		}
		row := rows[0]
		if row.QueryOccurrences != 5 {
			t.Fatalf("filtered/page count=%d want 5", row.QueryOccurrences)
		}
	}
	got, err := d.GetSpan("span-1")
	if err != nil || got.QueryOccurrences != 5 {
		t.Fatalf("get count=%v err=%v", got, err)
	}
	rows, err := d.GetTrace("trace-2")
	if err != nil || len(rows) != 1 || rows[0].QueryOccurrences != 5 {
		t.Fatalf("trace count=%v err=%v", rows, err)
	}
	got, err = d.GetSpan("s-other")
	if err != nil || got.QueryOccurrences != 1 {
		t.Fatalf("session isolation: %v %v", got, err)
	}
	got, err = d.GetSpan("svc-other")
	if err != nil || got.QueryOccurrences != 1 {
		t.Fatalf("service isolation: %v %v", got, err)
	}
	got, err = d.GetSpan("different-4")
	if err != nil || got.QueryOccurrences != 0 {
		t.Fatalf("unknown fingerprint version counted: %v %v", got, err)
	}
}

func TestSessionQueryOccurrencesLegacyIdentityAndNonQueries(t *testing.T) {
	d := openTestDB(t)
	fp := "sequel:v1:sqlite:" + strings.Repeat("b", 64)
	for i, attrs := range []map[string]any{
		{"gotel.db.query.fingerprint": fp, "db.name": "app", "net.peer.name": "db", "net.peer.port": 1234},
		{"gotel.db.query.fingerprint": fp, "db.namespace": "app", "server.address": "db", "server.port": 1234},
		{"db.system.name": "redis", "db.query.text": "GET key"},
		{},
		{"gotel.db.query.fingerprint": fp},
		{"gotel.db.query.fingerprint": fp},
	} {
		data, _ := json.Marshal(attrs)
		if err := d.InsertSpan(&Span{TraceID: "trace", SpanID: fmt.Sprint(i), SessionID: "s", ServiceName: "api", Name: "q", Attributes: string(data), Resource: "{}"}); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := d.GetTrace("trace")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range rows {
		want := int64(0)
		if s.SpanID == "0" || s.SpanID == "1" || s.SpanID == "4" || s.SpanID == "5" {
			want = 2
		}
		if s.QueryOccurrences != want {
			t.Fatalf("span %s count=%d want %d", s.SpanID, s.QueryOccurrences, want)
		}
	}
}
