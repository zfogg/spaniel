package detectors

import (
	"strings"
	"testing"

	"github.com/zfogg/spaniel/internal/storage"
)

func TestTimeoutEvidenceAndLateReanalysis(t *testing.T) {
	d := &TimeoutDetector{}
	parent := &storage.Span{SpanID: "parent", StatusCode: 1}
	child := &storage.Span{SpanID: "child", ParentSpanID: "parent", StartNs: 100, EndNs: 220, Attributes: `{"error.type":"context_deadline_exceeded","deadline.unix_nano":"200","secret":"do-not-copy"}`}
	cancel := &storage.Span{SpanID: "cancel", ParentSpanID: "child", Attributes: `{"error.type":"context_canceled"}`}
	first := d.Analyze("trace", "session", []*storage.Span{child}, 1)
	late := d.Analyze("trace", "session", []*storage.Span{cancel, parent, child}, 2)
	if len(first) != 1 || len(late) != 1 || first[0].ID != late[0].ID {
		t.Fatal("late evidence must update stable finding")
	}
	got := late[0]
	for _, want := range []string{"1 observed timeout", "1 with successful parent", "1 adjacent cancellation", "budget 100 ns, remaining -20 ns"} {
		if !strings.Contains(got.Fingerprint, want) {
			t.Errorf("%q missing %q", got.Fingerprint, want)
		}
	}
	if strings.Contains(got.Fingerprint, "do-not-copy") || got.ExampleSpanID != "child" || got.ParentSpanID != "parent" {
		t.Fatalf("unsafe or unlinked finding: %+v", got)
	}
}

func TestTimeoutSignals(t *testing.T) {
	tests := []struct {
		attrs, message string
		want           bool
	}{
		{`{"rpc.grpc.status_code":4}`, "", true},
		{`{"error.type":"timeout"}`, "", true},
		{`{}`, "context deadline exceeded", true},
		{`{"rpc.grpc.status_code":1}`, "", false},
		{`{"error.type":"context_canceled"}`, "context deadline exceeded", false},
		{`{"error.type":"database_error"}`, "database timeout while querying secret", false},
		{`{}`, "", false},
	}
	for _, tt := range tests {
		s := &storage.Span{SpanID: "s", Attributes: tt.attrs, StatusMessage: tt.message}
		got := (&TimeoutDetector{}).Analyze("t", "s", []*storage.Span{s}, 0)
		if (len(got) > 0) != tt.want {
			t.Errorf("%s %q: %v", tt.attrs, tt.message, got)
		}
		if len(got) > 0 && strings.Contains(got[0].Fingerprint, "budget") {
			t.Error("guessed budget")
		}
	}
}

func TestTimeoutPropagationAndPartial(t *testing.T) {
	spans := []*storage.Span{{SpanID: "a", Attributes: `{"spaniel.trace.partial":true,"rpc.grpc.status_code":4}`}, {SpanID: "b", ParentSpanID: "a", Attributes: `{"error.type":"deadline_exceeded"}`}}
	d := &TimeoutDetector{}
	if !SupportsTrace(d, spans) {
		t.Fatal("explicit partial evidence should be supported")
	}
	got := d.Analyze("t", "s", spans, 0)
	if len(got) != 1 || got[0].Count != 2 || strings.Contains(got[0].Fingerprint, "successful parent") && !strings.Contains(got[0].Fingerprint, "0 with successful parent") {
		t.Fatalf("propagation: %+v", got)
	}
}

func TestTimeoutIndependentChainsAndLateAncestor(t *testing.T) {
	d := &TimeoutDetector{}
	a := &storage.Span{SpanID: "a", ParentSpanID: "up", Attributes: `{"error.type":"timeout"}`}
	b := &storage.Span{SpanID: "b", ParentSpanID: "root", Attributes: `{"error.type":"deadline_exceeded"}`}
	early := d.Analyze("trace", "s", []*storage.Span{a, b}, 1)
	upstream := &storage.Span{SpanID: "up", ParentSpanID: "root", Attributes: `{"rpc.grpc.status_code":4}`}
	late := d.Analyze("trace", "s", []*storage.Span{upstream, b, a}, 2)
	if len(early) != 2 || len(late) != 2 || early[0].ID != late[0].ID || early[1].ID != late[1].ID {
		t.Fatalf("independent chains or stable IDs: early=%+v late=%+v", early, late)
	}
	if late[0].Count != 2 || late[1].Count != 1 {
		t.Fatalf("late ancestor did not join the right chain: %+v", late)
	}
}
