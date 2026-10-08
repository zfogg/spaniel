package ingestion

import (
	"testing"
	"time"

	"github.com/zfogg/spaniel/internal/storage"
)

func makeDBSpan(spanID, parentID, stmt string, startNs int64) *storage.Span {
	return &storage.Span{
		TraceID: "trace1", SpanID: spanID, ParentSpanID: parentID,
		Name:       "SELECT",
		Attributes: `{"db.system":"postgresql","db.statement":"` + stmt + `"}`,
		StartNs:    startNs, EndNs: startNs + int64(10*time.Millisecond),
		SessionID: "sess",
	}
}

func TestN1Detector_SingleQueryIsNotNPlusOne(t *testing.T) {
	spans := []*storage.Span{makeDBSpan("one", "parent", "SELECT * FROM users WHERE id = 1", 0)}
	d := &N1Detector{}
	if got := d.Analyze("t", "s", spans, 0); len(got) != 0 {
		t.Errorf("expected 0 issues for one span, got %d", len(got))
	}
}

func TestN1Detector_RepeatedQueryIsNPlusOne(t *testing.T) {
	var spans []*storage.Span
	for i := range 2 {
		spans = append(spans, makeDBSpan(itoa(i), "parent", "SELECT * FROM users WHERE id = 1", int64(i)*int64(time.Millisecond)))
	}
	d := &N1Detector{}
	got := d.Analyze("t", "s", spans, 0)
	if len(got) != 1 {
		t.Fatalf("expected 1 issue for 2 spans, got %d", len(got))
	}
	if got[0].Kind != "n_plus_one" {
		t.Errorf("wrong kind: %q", got[0].Kind)
	}
	if got[0].Count != 2 {
		t.Errorf("Count = %d, want 2", got[0].Count)
	}
}

func TestN1Detector_DoesNotCombineIndependentParents(t *testing.T) {
	spans := []*storage.Span{
		makeDBSpan("one", "parent-a", "SELECT * FROM users WHERE id = 1", 0),
		makeDBSpan("two", "parent-b", "SELECT * FROM users WHERE id = 1", int64(time.Millisecond)),
	}
	if got := (&N1Detector{}).Analyze("t", "s", spans, 0); len(got) != 0 {
		t.Errorf("expected independent parents not to be combined, got %d issues", len(got))
	}
}

func TestN1Detector_TwoFingerprintsIndependent(t *testing.T) {
	var spans []*storage.Span
	for i := range 2 {
		spans = append(spans, makeDBSpan("a"+itoa(i), "p", "SELECT * FROM users WHERE id = 1", int64(i)*int64(time.Millisecond)))
		spans = append(spans, makeDBSpan("b"+itoa(i), "p", "SELECT * FROM orders WHERE id = 1", int64(i)*int64(time.Millisecond)))
	}
	d := &N1Detector{}
	got := d.Analyze("t", "s", spans, 0)
	if len(got) != 2 {
		t.Errorf("expected 2 issues for 2 distinct repeated fingerprints, got %d", len(got))
	}
}

func TestN1Detector_NonDBSpansIgnored(t *testing.T) {
	spans := []*storage.Span{
		{SpanID: "s1", Attributes: `{"http.url":"https://api.example.com"}`, SessionID: "sess"},
		{SpanID: "s2", Attributes: `{}`, SessionID: "sess"},
	}
	d := &N1Detector{}
	if got := d.Analyze("t", "s", spans, 0); len(got) != 0 {
		t.Errorf("expected 0 issues for non-DB spans, got %d", len(got))
	}
}
