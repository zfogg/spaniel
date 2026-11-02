package client

import (
	"context"
	"github.com/zfogg/spaniel/internal/liveevent"
	"testing"
	"time"
)

func TestSnapshotRaceCoalescingAndSessionGuard(t *testing.T) {
	r := NewRefreshes(context.Background(), nil)
	key := Key{"logs", "list"}
	now := time.Now()
	fetch := func(context.Context, *Client, string) (any, error) { return "snapshot", nil }
	r.Register(Spec{Key: key, Resources: []liveevent.Resource{"logs"}, Fetch: fetch})
	first := r.Due(now)
	if len(first) != 1 {
		t.Fatal("missing bootstrap")
	}
	for i := 0; i < 1000; i++ {
		r.Invalidate([]liveevent.Resource{"logs"})
	}
	if len(r.Due(now)) != 0 {
		t.Fatal("parallel duplicate fetch")
	}
	if !r.Complete(first[0].Run(), now) {
		t.Fatal("snapshot rejected")
	}
	if len(r.Due(now)) != 0 {
		t.Fatal("coalesce delay missing")
	}
	repair := r.Due(now.Add(time.Second))
	if len(repair) != 1 {
		t.Fatal("snapshot race lost dirty event")
	}
	r.SetSession("new")
	if r.Complete(repair[0].Run(), now) {
		t.Fatal("old session overwrote new selection")
	}
	fresh := r.Due(now)
	if len(fresh) != 1 || fresh[0].Session != "new" {
		t.Fatal("new session did not resync")
	}
	r.Register(Spec{Key: key, Fetch: fetch})
	replacement := r.Due(now)
	if r.Complete(fresh[0].Run(), now) {
		t.Fatal("old selection overwrote replacement")
	}
	if len(replacement) != 1 || !r.Complete(replacement[0].Run(), now) {
		t.Fatal("replacement result lost")
	}
}
func TestRefreshConcurrencyIdleAndDedupe(t *testing.T) {
	r := NewRefreshes(context.Background(), nil)
	now := time.Now()
	for _, name := range []string{"a", "b", "c", "d", "e", "f"} {
		r.Register(Spec{Key: Key{"metrics", name}, Interval: 10 * time.Second, Fetch: func(context.Context, *Client, string) (any, error) { return nil, nil }})
	}
	work := r.Due(now)
	if len(work) != 4 {
		t.Fatal("unbounded worker slots")
	}
	if len(r.Due(now)) != 0 {
		t.Fatal("worker cap bypassed")
	}
	for _, w := range work {
		r.Complete(w.Run(), now)
	}
	rest := r.Due(now)
	if len(rest) != 2 {
		t.Fatal("queued resource starved")
	}
	for _, w := range rest {
		r.Complete(w.Run(), now)
	}
	if len(r.Due(now.Add(9*time.Second))) != 0 || len(r.Due(now.Add(10*time.Second))) != 4 {
		t.Fatal("idle time-window refresh policy failed")
	}
	type row struct{ ID, Value string }
	merged := MergeByID([]row{{"a", "old"}}, []row{{"a", "new"}, {"b", "two"}}, 2, func(r row) string { return r.ID })
	if len(merged) != 2 || merged[0].Value != "new" {
		t.Fatal("duplicate row not replaced")
	}
}
