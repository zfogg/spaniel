package ingestion

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/zfogg/spaniel/internal/storage"
)

func delivery(trace, span, id, destination string, at int64, status int, extra string) *storage.Span {
	return &storage.Span{TraceID: trace, SpanID: span, SessionID: "session", ServiceName: "consumer", StartNs: at, StatusCode: status, Attributes: fmt.Sprintf(`{"messaging.system":"rabbitmq","messaging.operation.name":"process","messaging.destination.name":%q,"messaging.message.id":%q%s}`, destination, id, extra)}
}
func TestRedeliveryCorrelation(t *testing.T) {
	now := time.Now().UnixNano()
	var d redeliveryDetector
	a := delivery("t1", "s1", "secret-message", "orders", now, 2, "")
	b := delivery("t2", "s2", "secret-message", "orders", now+2, 2, "")
	c := delivery("t3", "s3", "secret-message", "orders", now+1, 2, "")
	if got := d.analyze([]*storage.Span{a, b}, now); len(got) != 0 {
		t.Fatal(got)
	}
	got := d.analyze([]*storage.Span{c}, now)
	if len(got) != 1 || got[0].Count != 3 || strings.Contains(fmt.Sprintf("%+v", got[0]), "secret-message") {
		t.Fatalf("finding: %+v", got)
	}
	first := got[0]
	got = d.analyze([]*storage.Span{a, c}, now)
	if len(got) != 1 || got[0].Count != 3 || got[0].ID != first.ID || got[0].TraceID != first.TraceID {
		t.Fatalf("dedup: %+v", got)
	}
	success := delivery("t4", "s4", "secret-message", "orders", now+3, 1, "")
	got = d.analyze([]*storage.Span{success}, now)
	if len(got) != 1 || got[0].Count != 3 {
		t.Fatalf("eventual success: %+v", got)
	}
}
func TestRedeliveryConservativeEvidence(t *testing.T) {
	now := time.Now().UnixNano()
	var d redeliveryDetector
	var spans []*storage.Span
	for i := 0; i < 4; i++ {
		spans = append(spans, delivery(fmt.Sprint(i), "s", fmt.Sprint(i), "orders", now+int64(i), 2, ""))
	}
	spans = append(spans, delivery("missing", "s", "", "orders", now, 2, ""), delivery("batch", "s", "batch", "orders", now, 2, `,"messaging.batch.message_count":10`))
	if got := d.analyze(spans, now); len(got) != 0 {
		t.Fatal(got)
	}
	explicit := delivery("explicit", "s", "", "orders", now, 2, `,"messaging.message.delivery_attempt":3`)
	if got := d.analyze([]*storage.Span{explicit}, now); len(got) != 1 || got[0].Count != 3 {
		t.Fatal(got)
	}
	var scoped redeliveryDetector
	x := delivery("a", "s", "same", "one", now, 2, "")
	y := delivery("b", "s", "same", "two", now, 2, "")
	z := delivery("c", "s", "same", "one", now, 2, "")
	z.SessionID = "other"
	if got := scoped.analyze([]*storage.Span{x, y, z}, now); len(got) != 0 {
		t.Fatal(got)
	}
}
func TestRedeliveryWindowAndExpiry(t *testing.T) {
	now := time.Now().UnixNano()
	var d redeliveryDetector
	a := delivery("a", "s", "id", "q", now, 2, "")
	b := delivery("b", "s", "id", "q", now+1, 2, "")
	c := delivery("c", "s", "id", "q", now+redeliveryWindow+1, 2, "")
	if got := d.analyze([]*storage.Span{a, b, c}, now); len(got) != 0 {
		t.Fatal(got)
	}
	if got := d.analyze([]*storage.Span{c}, now+redeliveryWindow+1); len(got) != 0 {
		t.Fatal(got)
	}
}

func TestRedeliveryPersistsAcrossTraces(t *testing.T) {
	db := openTestDB(t)
	session, err := db.CreateSession("redelivery", false)
	if err != nil {
		t.Fatal(err)
	}
	p := &Pipeline{store: db}
	now := time.Now().UnixNano()
	for i := 0; i < 3; i++ {
		trace := fmt.Sprintf("trace-%d", i)
		s := delivery(trace, fmt.Sprintf("span-%d", i), "private-id", "orders", now+int64(i), 2, "")
		s.SessionID = session.ID
		if err := db.InsertSpan(s); err != nil {
			t.Fatal(err)
		}
		if err := db.FlushBatch(); err != nil {
			t.Fatal(err)
		}
		p.runRedeliveryDetector(trace)
	}
	p.runRedeliveryDetector("trace-0")
	issues, err := db.ListTraceIssuesBySession(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 || issues[0].Kind != "messaging_redelivery" || issues[0].Count != 3 || strings.Contains(fmt.Sprintf("%+v", issues[0]), "private-id") {
		t.Fatalf("issues: %+v", issues)
	}
	last := delivery("trace-3", "span-3", "private-id", "orders", now+3, 2, "")
	last.SessionID = session.ID
	if err := db.InsertSpan(last); err != nil {
		t.Fatal(err)
	}
	if err := db.FlushBatch(); err != nil {
		t.Fatal(err)
	}
	p.runRedeliveryDetector(last.TraceID)
	issues, err = db.ListTraceIssuesBySession(session.ID)
	if err != nil || len(issues) != 1 || issues[0].Count != 4 || !strings.Contains(issues[0].Fingerprint, "trace-3/span-3") {
		t.Fatalf("updated evidence: %+v %v", issues, err)
	}
	original := *issues[0]
	// Reconstruct in a different arrival order after a restart. Existing
	// evidence and the original trace association must remain intact.
	restarted := &Pipeline{store: db}
	for _, id := range []string{"trace-3", "trace-2", "trace-1"} {
		restarted.runRedeliveryDetector(id)
	}
	issues, err = db.ListTraceIssuesBySession(session.ID)
	if err != nil || len(issues) != 1 || issues[0].ID != original.ID || issues[0].Count != 4 || issues[0].TraceID != original.TraceID || issues[0].ExampleSpanID != original.ExampleSpanID {
		t.Fatalf("restart deduplication: %+v %v", issues, err)
	}
	for _, group := range restarted.redeliveries.groups {
		group.touched = time.Now().UnixNano() - redeliveryWindow - 1
	}
	for _, id := range []string{"trace-2", "trace-1", "trace-0"} {
		restarted.runRedeliveryDetector(id)
	}
	issues, err = db.ListTraceIssuesBySession(session.ID)
	if err != nil || len(issues) != 1 || issues[0].ID != original.ID || issues[0].Count != 4 || issues[0].ExampleSpanID != original.ExampleSpanID {
		t.Fatalf("expiry deduplication: %+v %v", issues, err)
	}
	trace, err := db.GetTrace(issues[0].TraceID, session.ID)
	if err != nil || len(trace) != 1 || trace[0].SpanID != issues[0].ExampleSpanID {
		t.Fatalf("correlation: %+v %v", trace, err)
	}
}
