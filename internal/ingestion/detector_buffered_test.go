package ingestion

import (
	"context"
	"testing"
	"time"

	"github.com/zfogg/spaniel/internal/ws"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

func TestBufferedSamplingRunsSessionScopedDetectors(t *testing.T) {
	db := openTestDB(t)
	a, err := db.CreateSession("sample-a", false)
	if err != nil {
		t.Fatal(err)
	}
	b, err := db.CreateSession("sample-b", false)
	if err != nil {
		t.Fatal(err)
	}
	p := NewPipelineWithSampler(db, ws.NewHub(), NewSampler(100000, KeepNPlusOne))
	p.SetSelfService("spaniel")
	makeTrace := func(service, trace string) ptrace.Traces {
		return makeTraces(service, func(ss ptrace.ScopeSpans) {
			s := ss.Spans().AppendEmpty()
			var tid pcommon.TraceID
			var sid pcommon.SpanID
			copy(tid[:], []byte(trace))
			copy(sid[:], []byte("same-span"))
			s.SetTraceID(tid)
			s.SetSpanID(sid)
			s.SetName("operation")
			s.SetStartTimestamp(100)
			s.SetEndTimestamp(200)
			s.Attributes().PutStr("error.type", "timeout")
		})
	}
	trace := makeTrace("app", "same-trace-12345")
	db.SetActiveSession(a.ID, a.Label)
	if err := p.IngestTraces(context.Background(), trace); err != nil {
		t.Fatal(err)
	}
	db.SetActiveSession(b.ID, b.Label)
	if err := p.IngestTraces(context.Background(), trace); err != nil {
		t.Fatal(err)
	}
	self := makeTrace("spaniel", "self-trace-12345")
	if err := p.IngestTraces(context.Background(), self); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		matched := 0
		for _, session := range []string{a.ID, b.ID} {
			issues, err := db.ListTraceIssuesBySession(session)
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, issue := range issues {
				if issue.Kind == "timeout" {
					count++
					if issue.Count != 1 {
						t.Fatalf("cross-session span mixing: %+v", issue)
					}
				}
			}
			if count > 1 {
				t.Fatalf("duplicate timeout findings: %+v", issues)
			}
			matched += count
		}
		if matched == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("got %d timeout findings, want 2", matched)
		}
		time.Sleep(20 * time.Millisecond)
	}
	p.debounceMu.Lock()
	defer p.debounceMu.Unlock()
	if len(p.debounce) != 0 {
		t.Fatalf("unexpected pending detector timers, including self telemetry: %v", p.debounce)
	}
	selfTraceID := self.ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(0).TraceID().String()
	issues, err := db.GetTraceIssues(selfTraceID)
	if err != nil || len(issues) != 0 {
		t.Fatalf("self telemetry produced findings: %+v %v", issues, err)
	}
}
