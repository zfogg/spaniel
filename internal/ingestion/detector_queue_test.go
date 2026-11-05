package ingestion

import (
	"testing"
	"time"
)

func TestDetectorQueueRetainsAndCoalescesWhileBusy(t *testing.T) {
	var q detectorQueue
	started := make(chan detectorKey, 10)
	release := make(chan struct{})
	defer close(release)
	run := func(id detectorKey) { started <- id; <-release }
	wait := func() detectorKey {
		t.Helper()
		select {
		case id := <-started:
			return id
		case <-time.After(5 * time.Second):
			t.Fatal("pending detection was lost")
			return detectorKey{}
		}
	}
	q.enqueue(detectorKey{"first", "s"}, run)
	wait()
	q.enqueue(detectorKey{"second", "s"}, run)
	wait()
	for range 20 {
		q.enqueue(detectorKey{"pending", "s"}, run)
	}
	q.mu.Lock()
	pending, workers := len(q.pending), q.running
	q.mu.Unlock()
	if pending != 1 || workers != maxConcurrentDetectors {
		t.Fatalf("pending=%d workers=%d", pending, workers)
	}
	release <- struct{}{}
	if id := wait(); id.traceID != "pending" {
		t.Fatalf("got %s", id)
	}
}
