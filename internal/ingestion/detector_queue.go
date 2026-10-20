package ingestion

import (
	"sync"

	"github.com/zfogg/spaniel/internal/goroutine"
)

// detectorQueue retains only trace IDs while the bounded workers are busy.
// Coalescing pending IDs avoids retaining span payloads or spawning a blocked
// goroutine for every trace in an OTLP burst.
type detectorQueue struct {
	mu      sync.Mutex
	pending map[string]struct{}
	running int
}

func (q *detectorQueue) enqueue(traceID string, run func(string)) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.pending == nil {
		q.pending = make(map[string]struct{})
	}
	q.pending[traceID] = struct{}{}
	if q.running >= maxConcurrentDetectors {
		return
	}
	q.running++
	goroutine.Go(func() {
		for {
			q.mu.Lock()
			var id string
			for next := range q.pending {
				id = next
				delete(q.pending, next)
				break
			}
			if id == "" {
				q.running--
				q.mu.Unlock()
				return
			}
			q.mu.Unlock()
			run(id)
		}
	}, "subsystem", "detectors")
}
