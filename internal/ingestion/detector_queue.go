package ingestion

import (
	"sync"

	"github.com/zfogg/spaniel/internal/goroutine"
)

type detectorKey struct{ traceID, sessionID string }

// detectorQueue retains only trace/session keys while the bounded workers are busy.
// Coalescing pending IDs avoids retaining span payloads or spawning a blocked
// goroutine for every trace in an OTLP burst.
type detectorQueue struct {
	mu      sync.Mutex
	pending map[detectorKey]struct{}
	running int
}

func (q *detectorQueue) enqueue(key detectorKey, run func(detectorKey)) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.pending == nil {
		q.pending = make(map[detectorKey]struct{})
	}
	q.pending[key] = struct{}{}
	if q.running >= maxConcurrentDetectors {
		return
	}
	q.running++
	goroutine.Go(func() {
		for {
			q.mu.Lock()
			var id detectorKey
			found := false
			for next := range q.pending {
				id = next
				found = true
				delete(q.pending, next)
				break
			}
			if !found {
				q.running--
				q.mu.Unlock()
				return
			}
			q.mu.Unlock()
			run(id)
		}
	}, "subsystem", "detectors")
}
