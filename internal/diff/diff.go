// Package diff computes a structural/timing comparison between two sessions'
// spans. It is shared by the HTTP API (/api/diff) and the MCP diff_sessions
// tool. JSON tags match the original API wire format.
package diff

import (
	"math"
	"strings"

	"github.com/zfogg/spaniel/internal/storage"
)

// Span is one (service, operation) row in a diff.
type Span struct {
	Name               string  `json:"name"`
	ServiceName        string  `json:"service_name"`
	Status             string  `json:"status"` // added|removed|changed|unchanged
	BaselineDurationNs int64   `json:"baseline_duration_ns"`
	CompareDurationNs  int64   `json:"compare_duration_ns"`
	DeltaPct           float64 `json:"delta_pct"`
	Depth              int     `json:"depth"`
}

type Summary struct {
	DurationDeltaNs  int64   `json:"duration_delta_ns"`
	DurationDeltaPct float64 `json:"duration_delta_pct"`
	SpansAdded       int     `json:"spans_added"`
	SpansRemoved     int     `json:"spans_removed"`
	DBCallDelta      int     `json:"db_call_delta"`
}

type SessionInfo struct {
	SessionID       string `json:"session_id"`
	Label           string `json:"label"`
	TotalDurationNs int64  `json:"total_duration_ns"`
	SpanCount       int    `json:"span_count"`
	DBCalls         int    `json:"db_calls"`
}

type Result struct {
	Baseline      SessionInfo     `json:"baseline"`
	Compare       SessionInfo     `json:"compare"`
	Summary       Summary         `json:"summary"`
	Spans         []Span          `json:"spans"`
	BaselineSpans []*storage.Span `json:"baseline_spans"`
	CompareSpans  []*storage.Span `json:"compare_spans"`
}

// ZeroParent is the all-zero parent span id some exporters use for roots.
const ZeroParent = "0000000000000000"

func isRoot(s *storage.Span) bool {
	return s.ParentSpanID == "" || s.ParentSpanID == ZeroParent
}

func isDB(s *storage.Span) bool {
	return strings.Contains(s.Attributes, `"db.system"`) ||
		strings.Contains(s.Attributes, `"db.statement"`) ||
		strings.Contains(s.Attributes, `"db.operation"`)
}

type spanKey struct{ svc, name string }

// avgDurationByKey groups spans by (service_name, name) and returns average duration per key.
func avgDurationByKey(spans []*storage.Span) map[spanKey]int64 {
	sum := map[spanKey]int64{}
	cnt := map[spanKey]int{}
	for _, s := range spans {
		k := spanKey{s.ServiceName, s.Name}
		sum[k] += s.DurationNs
		cnt[k]++
	}
	avg := make(map[spanKey]int64, len(sum))
	for k, s := range sum {
		avg[k] = s / int64(cnt[k])
	}
	return avg
}

// depthByKey picks the minimum depth (closest to root) for a key in a span list.
func depthByKey(spans []*storage.Span) map[spanKey]int {
	byID := make(map[string]*storage.Span, len(spans))
	for _, s := range spans {
		byID[s.SpanID] = s
	}
	depth := func(s *storage.Span) int {
		d := 0
		cur := s
		for !isRoot(cur) {
			p, ok := byID[cur.ParentSpanID]
			if !ok {
				break
			}
			d++
			cur = p
		}
		return d
	}
	m := map[spanKey]int{}
	for _, s := range spans {
		k := spanKey{s.ServiceName, s.Name}
		if d := depth(s); d < m[k] || m[k] == 0 {
			m[k] = d
		}
	}
	return m
}

func rootDuration(spans []*storage.Span) int64 {
	for _, s := range spans {
		if isRoot(s) {
			return s.DurationNs
		}
	}
	return 0
}

func dbCallCount(spans []*storage.Span) int {
	n := 0
	for _, s := range spans {
		if isDB(s) {
			n++
		}
	}
	return n
}

// Compute diffs baseline vs compare sessions by their spans.
func Compute(
	baseSess, cmpSess *storage.Session,
	baseSpans, cmpSpans []*storage.Span,
) Result {
	baseAvg := avgDurationByKey(baseSpans)
	cmpAvg := avgDurationByKey(cmpSpans)
	baseDepth := depthByKey(baseSpans)
	cmpDepth := depthByKey(cmpSpans)

	baseRoot := rootDuration(baseSpans)
	cmpRoot := rootDuration(cmpSpans)

	baseDB := dbCallCount(baseSpans)
	cmpDB := dbCallCount(cmpSpans)

	baseInfo := SessionInfo{
		SessionID:       baseSess.ID,
		Label:           baseSess.Label,
		TotalDurationNs: baseRoot,
		SpanCount:       len(baseSpans),
		DBCalls:         baseDB,
	}
	cmpInfo := SessionInfo{
		SessionID:       cmpSess.ID,
		Label:           cmpSess.Label,
		TotalDurationNs: cmpRoot,
		SpanCount:       len(cmpSpans),
		DBCalls:         cmpDB,
	}

	// Build ordered key list: baseline keys first, then compare-only keys.
	seen := map[spanKey]bool{}
	var keys []spanKey
	for k := range baseAvg {
		keys = append(keys, k)
		seen[k] = true
	}
	for k := range cmpAvg {
		if !seen[k] {
			keys = append(keys, k)
		}
	}

	var diffSpans []Span
	added, removed := 0, 0

	for _, k := range keys {
		bDur, inBase := baseAvg[k]
		cDur, inCmp := cmpAvg[k]

		var status string
		var deltaPct float64
		switch {
		case inBase && inCmp:
			if bDur > 0 {
				deltaPct = math.Round((float64(cDur-bDur)/float64(bDur))*1000) / 10
			}
			if math.Abs(deltaPct) >= 5 {
				status = "changed"
			} else {
				status = "unchanged"
			}
		case inBase && !inCmp:
			status = "removed"
			removed++
		case !inBase && inCmp:
			status = "added"
			added++
		}

		depth := baseDepth[k]
		if d, ok := cmpDepth[k]; ok && (depth == 0 || d < depth) {
			depth = d
		}

		diffSpans = append(diffSpans, Span{
			Name:               k.name,
			ServiceName:        k.svc,
			Status:             status,
			BaselineDurationNs: bDur,
			CompareDurationNs:  cDur,
			DeltaPct:           deltaPct,
			Depth:              depth,
		})
	}

	var durationDeltaNs int64
	var durationDeltaPct float64
	if baseRoot > 0 {
		durationDeltaNs = cmpRoot - baseRoot
		durationDeltaPct = math.Round((float64(durationDeltaNs)/float64(baseRoot))*1000) / 10
	}

	summary := Summary{
		DurationDeltaNs:  durationDeltaNs,
		DurationDeltaPct: durationDeltaPct,
		SpansAdded:       added,
		SpansRemoved:     removed,
		DBCallDelta:      cmpDB - baseDB,
	}

	return Result{
		Baseline:      baseInfo,
		Compare:       cmpInfo,
		Summary:       summary,
		Spans:         diffSpans,
		BaselineSpans: baseSpans,
		CompareSpans:  cmpSpans,
	}
}
