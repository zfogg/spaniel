package ingestion

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/zfogg/spaniel/internal/ingestion/detectors"
	"github.com/zfogg/spaniel/internal/storage"
)

func delivery(trace, span, id, destination string, at int64, status int, extra string) *storage.Span {
	return &storage.Span{TraceID: trace, SpanID: span, SessionID: "session", ServiceName: "consumer", StartNs: at, StatusCode: status, Attributes: fmt.Sprintf(`{"messaging.system":"rabbitmq","messaging.operation.name":"process","messaging.destination.name":%q,"messaging.message.id":%q%s}`, destination, id, extra)}
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
		p.runRedeliveryDetector(trace, session.ID)
	}
	p.runRedeliveryDetector("trace-0", session.ID)
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
	p.runRedeliveryDetector(last.TraceID, session.ID)
	issues, err = db.ListTraceIssuesBySession(session.ID)
	if err != nil || len(issues) != 1 || issues[0].Count != 4 || !strings.Contains(issues[0].Fingerprint, "trace-3/span-3") {
		t.Fatalf("updated evidence: %+v %v", issues, err)
	}
	original := *issues[0]
	// Reconstruct in a different arrival order after a restart. Existing
	// evidence and the original trace association must remain intact.
	restarted := &Pipeline{store: db}
	for _, id := range []string{"trace-3", "trace-2", "trace-1"} {
		restarted.runRedeliveryDetector(id, session.ID)
	}
	issues, err = db.ListTraceIssuesBySession(session.ID)
	if err != nil || len(issues) != 1 || issues[0].ID != original.ID || issues[0].Count != 4 || issues[0].TraceID != original.TraceID || issues[0].ExampleSpanID != original.ExampleSpanID {
		t.Fatalf("restart deduplication: %+v %v", issues, err)
	}
	key, err := db.RedeliveryKey()
	if err != nil {
		t.Fatal(err)
	}
	restarted.redeliveries.detector = detectors.NewRedeliveryDetector(key)
	for _, id := range []string{"trace-2", "trace-1", "trace-0"} {
		restarted.runRedeliveryDetector(id, session.ID)
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
