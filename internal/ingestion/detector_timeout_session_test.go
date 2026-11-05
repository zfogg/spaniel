package ingestion

import (
	"testing"
	"time"

	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/ws"
	"go.opentelemetry.io/otel"
)

func TestTimeoutDetectorSessionIsolationAndLateSwitch(t *testing.T) {
	db := openTestDB(t)
	a, err := db.CreateSession("a", false)
	if err != nil {
		t.Fatal(err)
	}
	b, err := db.CreateSession("b", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, session := range []string{a.ID, b.ID} {
		if err := db.InsertSpan(&storage.Span{TraceID: "same-trace", SpanID: "same-span", SessionID: session, Attributes: `{"error.type":"timeout"}`, StartNs: 1, EndNs: 2}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.FlushBatch(); err != nil {
		t.Fatal(err)
	}
	hub := ws.NewHub()
	p := NewPipeline(db, hub)
	// The session changes before queued work executes. Its key must retain A.
	db.SetActiveSession(b.ID, b.Label)
	p.scheduleDetectorRun("same-trace", a.ID)
	deadline := time.Now().Add(10 * time.Second)
	for {
		issues, err := db.ListTraceIssuesBySession(a.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(issues) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("queued A finding missing after session switch")
		}
		time.Sleep(20 * time.Millisecond)
	}
	runDetectors("same-trace", b.ID, db, hub, otel.Tracer("session-test"))
	runDetectors("same-trace", a.ID, db, hub, otel.Tracer("session-test"))
	var ids []string
	for _, session := range []string{a.ID, b.ID} {
		issues, err := db.ListTraceIssuesBySession(session)
		if err != nil {
			t.Fatal(err)
		}
		var timeout []*storage.TraceIssue
		for _, issue := range issues {
			if issue.Kind == "timeout" {
				timeout = append(timeout, issue)
			}
		}
		if len(timeout) != 1 || timeout[0].Count != 1 || timeout[0].SessionID != session {
			t.Fatalf("session %s: %+v", session, timeout)
		}
		ids = append(ids, timeout[0].ID)
	}
	if ids[0] == ids[1] {
		t.Fatalf("shared finding identity: %s", ids[0])
	}
}
