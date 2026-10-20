package ingestion

import (
	"testing"
	"time"
)

func TestDetectorQueueRetainsAndCoalescesWhileBusy(t *testing.T) {
	var q detectorQueue
	started := make(chan string, 10)
	release := make(chan struct{})
	defer close(release)
	run := func(id string) { started <- id; <-release }
	wait := func() string {
		t.Helper()
		select {
		case id := <-started:
			return id
		case <-time.After(5 * time.Second):
			t.Fatal("pending detection was lost")
			return ""
		}
	}
	q.enqueue("first", run)
	wait()
	q.enqueue("second", run)
	wait()
	for range 20 {
		q.enqueue("pending", run)
	}
	q.mu.Lock()
	pending, workers := len(q.pending), q.running
	q.mu.Unlock()
	if pending != 1 || workers != maxConcurrentDetectors {
		t.Fatalf("pending=%d workers=%d", pending, workers)
	}
	release <- struct{}{}
	if id := wait(); id != "pending" {
		t.Fatalf("got %s", id)
	}
}
