package detectors

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
	d := NewRedeliveryDetector([32]byte{})
	a := delivery("t1", "s1", "secret-message", "orders", now, 2, "")
	b := delivery("t2", "s2", "secret-message", "orders", now+2, 2, "")
	c := delivery("t3", "s3", "secret-message", "orders", now+1, 2, "")
	if got := d.Analyze([]*storage.Span{a, b}, now); len(got) != 0 {
		t.Fatal(got)
	}
	got := d.Analyze([]*storage.Span{c}, now)
	if len(got) != 1 || got[0].Count != 3 || strings.Contains(fmt.Sprintf("%+v", got[0]), "secret-message") {
		t.Fatalf("finding: %+v", got)
	}
	first := got[0]
	got = d.Analyze([]*storage.Span{a, c}, now)
	if len(got) != 1 || got[0].Count != 3 || got[0].ID != first.ID || got[0].TraceID != first.TraceID {
		t.Fatalf("dedup: %+v", got)
	}
	success := delivery("t4", "s4", "secret-message", "orders", now+3, 1, "")
	got = d.Analyze([]*storage.Span{success}, now)
	if len(got) != 1 || got[0].Count != 3 {
		t.Fatalf("eventual success: %+v", got)
	}
}
func TestRedeliveryConservativeEvidence(t *testing.T) {
	now := time.Now().UnixNano()
	d := NewRedeliveryDetector([32]byte{})
	var spans []*storage.Span
	for i := 0; i < 4; i++ {
		spans = append(spans, delivery(fmt.Sprint(i), "s", fmt.Sprint(i), "orders", now+int64(i), 2, ""))
	}
	spans = append(spans, delivery("missing", "s", "", "orders", now, 2, ""), delivery("batch", "s", "batch", "orders", now, 2, `,"messaging.batch.message_count":10`))
	if got := d.Analyze(spans, now); len(got) != 0 {
		t.Fatal(got)
	}
	explicit := delivery("explicit", "s", "", "orders", now, 2, `,"messaging.message.delivery_attempt":3`)
	if got := d.Analyze([]*storage.Span{explicit}, now); len(got) != 1 || got[0].Count != 3 {
		t.Fatal(got)
	}
	scoped := NewRedeliveryDetector([32]byte{})
	x := delivery("a", "s", "same", "one", now, 2, "")
	y := delivery("b", "s", "same", "two", now, 2, "")
	z := delivery("c", "s", "same", "one", now, 2, "")
	z.SessionID = "other"
	if got := scoped.Analyze([]*storage.Span{x, y, z}, now); len(got) != 0 {
		t.Fatal(got)
	}
}
func TestRedeliveryWindowAndExpiry(t *testing.T) {
	now := time.Now().UnixNano()
	d := NewRedeliveryDetector([32]byte{})
	a := delivery("a", "s", "id", "q", now, 2, "")
	b := delivery("b", "s", "id", "q", now+1, 2, "")
	c := delivery("c", "s", "id", "q", now+redeliveryWindow+1, 2, "")
	if got := d.Analyze([]*storage.Span{a, b, c}, now); len(got) != 0 {
		t.Fatal(got)
	}
	if got := d.Analyze([]*storage.Span{c}, now+redeliveryWindow+1); len(got) != 0 {
		t.Fatal(got)
	}
	var key [32]byte
	d = NewRedeliveryDetector(key)
	c.StartNs = now + 2
	before := d.Analyze([]*storage.Span{a, b, c}, now)
	after := d.Analyze([]*storage.Span{c, b, a}, now+redeliveryWindow+1)
	if len(before) != 1 || len(after) != 1 || before[0].ID != after[0].ID {
		t.Fatalf("expiry identity: before=%+v after=%+v", before, after)
	}
}
