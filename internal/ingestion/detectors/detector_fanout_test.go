package detectors

import (
	"github.com/zfogg/spaniel/internal/storage"
	"strings"
	"testing"
)

func TestFanoutEvidence(t *testing.T) {
	parent := &storage.Span{SpanID: "parent", Name: "checkout", StartNs: 0, EndNs: 100}
	calls := []*storage.Span{parent}
	for i := 0; i < 5; i++ {
		calls = append(calls, &storage.Span{SpanID: string(rune('a' + i)), ParentSpanID: "parent", Kind: 3, Name: string(rune('A' + i%3)), Attributes: `{"server.address":"backend"}`, StartNs: int64(i * 10), EndNs: int64(i*10 + 10)})
	}
	d := &FanoutDetector{}
	got := d.Analyze("trace", "session", calls, 1)
	if len(got) != 1 || got[0].Count != 5 || got[0].WastedNs != 0 || !strings.Contains(got[0].Fingerprint, "covered 50ns") || !strings.Contains(got[0].Fingerprint, "overlapping 0ns") {
		t.Fatalf("sequential: %+v", got)
	}
	for _, s := range calls[1:] {
		s.StartNs = 10
		s.EndNs = 20
	}
	got = d.Analyze("trace", "session", calls, 2)
	if len(got) != 1 || !strings.Contains(got[0].Fingerprint, "covered 10ns") || !strings.Contains(got[0].Fingerprint, "overlapping 40ns") {
		t.Fatalf("parallel: %+v", got)
	}
	// Retransmission and server-side counterparts do not count as calls.
	calls = append(calls, calls[1], &storage.Span{SpanID: "server", ParentSpanID: "a", Kind: 2, Name: "server", StartNs: 10, EndNs: 20})
	got = d.Analyze("trace", "session", calls, 3)
	if len(got) != 1 || got[0].Count != 5 || got[0].ID != d.Analyze("trace", "session", calls[:6], 1)[0].ID {
		t.Fatalf("duplicates: %+v", got)
	}
	if got = d.Analyze("trace", "session", calls[1:], 1); len(got) != 0 {
		t.Fatalf("missing parent: %+v", got)
	}
	if got = d.Analyze("trace", "session", calls[:5], 1); len(got) != 0 {
		t.Fatalf("normal fanout: %+v", got)
	}
	if got = d.Analyze("trace", "session", calls[:6], 1); len(got) != 1 {
		t.Fatalf("late arrival: %+v", got)
	}
}

func TestFanoutExcludesRepetitionAndPartial(t *testing.T) {
	parent := &storage.Span{SpanID: "p", Name: "op", EndNs: 100}
	spans := []*storage.Span{parent}
	for i := 0; i < 5; i++ {
		spans = append(spans, &storage.Span{SpanID: string(rune('a' + i)), ParentSpanID: "p", Kind: 3, Name: "GET", Attributes: `{"server.address":"host","http.request.method":"GET"}`, EndNs: 10})
	}
	d := &FanoutDetector{}
	if got := d.Analyze("t", "s", spans, 0); len(got) != 0 {
		t.Fatalf("repeated group: %+v", got)
	}
	parent.Attributes = `{"spaniel.trace.partial":true}`
	if SupportsTrace(d, spans) {
		t.Fatal("partial trace must be skipped")
	}
}

func TestFanoutLargeRPCAndLateEvidence(t *testing.T) {
	p := &storage.Span{SpanID: "p", Name: "parent", EndNs: 100}
	spans := []*storage.Span{p}
	for i := 0; i < 5; i++ {
		spans = append(spans, &storage.Span{SpanID: string(rune('a' + i)), ParentSpanID: "p", Kind: 3, Name: "RPC", Attributes: `{"server.address":"rpc","rpc.system":"grpc"}`, StartNs: int64(i * 10), EndNs: int64(i*10 + 10)})
	}
	spans = append(spans, &storage.Span{SpanID: "x", ParentSpanID: "p", Kind: 4, Name: "publish", Attributes: `{"messaging.destination.name":"queue"}`, StartNs: 50, EndNs: 60}, &storage.Span{SpanID: "y", ParentSpanID: "p", Kind: 3, Name: "other", Attributes: `{"server.address":"other"}`, StartNs: 60, EndNs: 70})
	d := &FanoutDetector{}
	first := d.Analyze("t", "s", spans, 1)
	if len(first) != 1 || first[0].Count != 7 || !strings.Contains(first[0].Fingerprint, "rpc / RPC (5;") {
		t.Fatalf("large RPC group: %+v", first)
	}
	spans = append(spans, &storage.Span{SpanID: "z", ParentSpanID: "p", Kind: 3, Name: "RPC", Attributes: `{"server.address":"rpc","rpc.system":"grpc"}`, StartNs: 70, EndNs: 80})
	next := d.Analyze("t", "s", spans, 2)
	if len(next) != 1 || next[0].ID != first[0].ID || next[0].Count != 8 || !strings.Contains(next[0].Fingerprint, "rpc / RPC (6;") || !strings.Contains(next[0].Fingerprint, "covered 80ns") {
		t.Fatalf("late evidence: %+v", next)
	}
}

func TestFanoutLateEvidencePersists(t *testing.T) {
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	// Retained databases may carry this legacy index.
	if _, err := db.SQL().Exec("CREATE INDEX IF NOT EXISTS idx_trace_issues_trace_id ON trace_issues(trace_id)"); err != nil {
		t.Fatal(err)
	}
	spans := []*storage.Span{{SpanID: "p", Name: "parent", EndNs: 100}}
	for i := 0; i < 5; i++ {
		spans = append(spans, &storage.Span{SpanID: string(rune('a' + i)), ParentSpanID: "p", Kind: 3, Name: string(rune('A' + i%3)), Attributes: `{"server.address":"backend"}`, StartNs: int64(i * 10), EndNs: int64(i*10 + 10)})
	}
	d := &FanoutDetector{}
	initial := d.Analyze("trace", "session", spans, 1)
	if len(initial) != 1 {
		t.Fatalf("initial: %+v", initial)
	}
	if err := db.UpsertTraceIssue(initial[0]); err != nil {
		t.Fatal(err)
	}
	spans = append(spans, &storage.Span{SpanID: "z", ParentSpanID: "p", Kind: 3, Name: "A", Attributes: `{"server.address":"backend"}`, StartNs: 50, EndNs: 60})
	updated := d.Analyze("trace", "session", spans, 2)
	if len(updated) != 1 || updated[0].ID != initial[0].ID {
		t.Fatalf("updated: %+v", updated)
	}
	if err := db.UpsertTraceIssue(updated[0]); err != nil {
		t.Fatal(err)
	}
	stored, err := db.GetTraceIssues("trace")
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 || stored[0].Count != 6 || stored[0].Fingerprint != updated[0].Fingerprint || !strings.Contains(stored[0].Fingerprint, "covered 60ns") || !strings.Contains(stored[0].Fingerprint, "backend / A (3;") {
		t.Fatalf("persisted evidence: %+v", stored)
	}
}
