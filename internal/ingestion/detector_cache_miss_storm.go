package ingestion

import (
	"time"

	"github.com/zfogg/spaniel/internal/storage"
)

const (
	cacheMissStormWindow    = 50 * time.Millisecond
	cacheMissStormThreshold = 10
)

// CacheMissStormDetector fires when ≥ 10 cache-miss spans cluster within a 50ms window.
type CacheMissStormDetector struct{}

func (*CacheMissStormDetector) Kind() string { return "cache_miss_storm" }

func (*CacheMissStormDetector) Analyze(traceID, sessionID string, spans []*storage.Span, now int64) []*storage.TraceIssue {
	// Collect cache spans that missed.
	var misses []*storage.Span
	for _, s := range spans {
		if !isCacheSpan(s) {
			continue
		}
		hit, found := extractAttrBool(s.Attributes, "cache.hit")
		if found && !hit {
			misses = append(misses, s)
			continue
		}
		// Also treat spans named "miss" or with miss in name as misses when no explicit attr.
		if !found {
			misses = append(misses, s)
		}
	}
	if len(misses) < cacheMissStormThreshold {
		return nil
	}

	// Sort by start time (insertion sort — span count is small).
	for i := 1; i < len(misses); i++ {
		for j := i; j > 0 && misses[j].StartNs < misses[j-1].StartNs; j-- {
			misses[j], misses[j-1] = misses[j-1], misses[j]
		}
	}

	// Slide a 50ms window; find the densest cluster.
	bestStart, bestEnd := 0, 0
	bestCount := 0
	windowNs := int64(cacheMissStormWindow)
	for i := range misses {
		j := i
		for j < len(misses) && misses[j].StartNs-misses[i].StartNs <= windowNs {
			j++
		}
		if j-i > bestCount {
			bestCount = j - i
			bestStart = i
			bestEnd = j
		}
	}
	if bestCount < cacheMissStormThreshold {
		return nil
	}

	cluster := misses[bestStart:bestEnd]
	var totalNs int64
	parentCounts := map[string]int{}
	for _, s := range cluster {
		totalNs += s.EndNs - s.StartNs
		parentCounts[s.ParentSpanID]++
	}
	topParent := ""
	maxCnt := 0
	for pid, cnt := range parentCounts {
		if cnt > maxCnt {
			maxCnt = cnt
			topParent = pid
		}
	}
	return []*storage.TraceIssue{{
		ID:            issueID(traceID, "cache_miss_storm", cluster[0].SpanID),
		TraceID:       traceID,
		SessionID:     sessionID,
		Kind:          "cache_miss_storm",
		Fingerprint:   "cache miss storm",
		Count:         bestCount,
		WastedNs:      totalNs,
		ParentSpanID:  topParent,
		ExampleSpanID: cluster[0].SpanID,
		CreatedAt:     now,
	}}
}
