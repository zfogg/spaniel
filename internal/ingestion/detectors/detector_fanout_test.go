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
