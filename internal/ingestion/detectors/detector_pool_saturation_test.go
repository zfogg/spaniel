package detectors

import (
	"testing"
	"time"

	"github.com/zfogg/spaniel/internal/storage"
)

func TestPoolSaturationEvidence(t *testing.T) {
	d := &PoolSaturationDetector{}
	wait := func(id string, duration time.Duration) *storage.Span {
		return &storage.Span{SpanID: id, SessionID: "session", ServiceName: "api", Attributes: `{"db.operation.name":"connection.acquire","db.client.connection.pool.name":"orders"}`, EndNs: int64(duration)}
	}
	a, b, c := wait("a", 120*time.Millisecond), wait("b", 120*time.Millisecond), wait("c", 120*time.Millisecond)
	if got := d.Analyze("trace", "session", []*storage.Span{a, b}, 1); len(got) != 0 {
		t.Fatal("two waits are insufficient", got)
	}
	got := d.Analyze("trace", "session", []*storage.Span{a, b, c}, 1)
	if len(got) != 1 || got[0].Count != 3 || got[0].ExampleSpanID != "a" {
		t.Fatal(got)
	}
	again := d.Analyze("trace", "session", []*storage.Span{c, b, a}, 2)
	if again[0].ID != got[0].ID {
		t.Fatal("unstable identity")
	}
	c.SessionID = "other"
	if got := d.Analyze("trace", "session", []*storage.Span{a, b, c}, 1); len(got) != 0 {
		t.Fatal("cross-session evidence", got)
	}
	timeout := wait("timeout", time.Millisecond)
	timeout.StatusCode = 2
	timeout.Attributes = `{"db.operation.name":"connection.acquire","db.client.connection.pool.name":"orders","error.type":"connection_acquire_timeout"}`
	if got := d.Analyze("trace", "session", []*storage.Span{timeout}, 1); len(got) != 1 {
		t.Fatal("timeout missing", got)
	}
	query := wait("query", time.Second)
	query.Attributes = `{"db.operation.name":"SELECT","db.client.connection.pool.name":"orders"}`
	if got := d.Analyze("trace", "session", []*storage.Span{query, query, query}, 1); len(got) != 0 {
		t.Fatal("slow queries are not pool waits", got)
	}
}
