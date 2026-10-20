package ingestion

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/collector/pdata/ptrace"

	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/ws"
)

func TestN1DetectorRatioNotificationChannels(t *testing.T) {
	spans := []*storage.Span{}
	for i := range 6 {
		spans = append(spans, &storage.Span{SpanID: itoa(i), ParentSpanID: "request", Attributes: `{"db.system.name":"postgresql","db.query.text":"SELECT * FROM \\\"notification_channels\\\" WHERE user_id=? AND provider=?"}`, StartNs: int64(i) * 100, EndNs: int64(i)*100 + 50})
	}
	issues := (&N1Detector{}).Analyze("ratio-trace", "session", spans, 1)
	if len(issues) != 1 || issues[0].Count != 6 {
		t.Fatalf("expected six repeated provider reads, got %+v", issues)
	}
}

func TestN1DetectorDistinctIssuesForSharedSQLPrefixAndParents(t *testing.T) {
	var spans []*storage.Span
	for i := range 2 {
		spans = append(spans, makeDBSpan("users"+itoa(i), "p", "SELECT * FROM users WHERE id=1", 0), makeDBSpan("orders"+itoa(i), "p", "SELECT * FROM orders WHERE id=1", 0), makeDBSpan("other"+itoa(i), "q", "SELECT * FROM users WHERE id=1", 0))
	}
	got := (&N1Detector{}).Analyze("trace1", "session", spans, 1)
	ids := map[string]bool{}
	for _, issue := range got {
		ids[issue.ID] = true
	}
	if len(got) != 3 || len(ids) != 3 {
		t.Fatalf("distinct query groups collided: %+v", got)
	}
}

func TestN1DetectorIgnoresRetransmittedSpan(t *testing.T) {
	span := makeDBSpan("same", "parent", "SELECT * FROM users WHERE id=1", 0)
	if got := (&N1Detector{}).Analyze("trace1", "session", []*storage.Span{span, span}, 1); len(got) != 0 {
		t.Fatalf("retransmission counted as N+1: %+v", got)
	}
}

func TestPipelineRatioNotificationChannels(t *testing.T) {
	db := openTestDB(t)
	sess, err := db.CreateSession("ratio", false)
	if err != nil {
		t.Fatal(err)
	}
	db.SetActiveSession(sess.ID, sess.Label)
	p := NewPipeline(db, ws.NewHub())
	td := makeTraces("ratio-api", func(ss ptrace.ScopeSpans) {
		putSpan(ss, "c", "p", "", "GET", ptrace.SpanKindServer, map[string]string{})
		for i := range 6 {
			putSpan(ss, "c", string(rune('a'+i)), "p", "ratio.storage.ChannelsListChannel", ptrace.SpanKindClient, map[string]string{"db.system.name": "postgresql", "db.query.text": `SELECT * FROM "notification_channels" WHERE user_id=? AND provider=?`})
		}
	})
	if err := p.IngestTraces(context.Background(), td); err != nil {
		t.Fatal(err)
	}
	tid := td.ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(0).TraceID().String()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		issues, err := db.GetTraceIssues(tid)
		if err != nil {
			t.Fatal(err)
		}
		for _, issue := range issues {
			if issue.Kind == "n_plus_one" && issue.Count == 6 {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("six provider queries were not persisted as N+1")
}
