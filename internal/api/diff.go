package api

import (
	"math"
	"net/http"
	"strings"

	"github.com/zfogg/spaniel/internal/storage"
)

// ── diff types ─────────────────────────────────────────────────────────────────

type DiffSpan struct {
	Name               string  `json:"name"`
	ServiceName        string  `json:"service_name"`
	Status             string  `json:"status"` // added|removed|changed|unchanged
	BaselineDurationNs int64   `json:"baseline_duration_ns"`
	CompareDurationNs  int64   `json:"compare_duration_ns"`
	DeltaPct           float64 `json:"delta_pct"`
	Depth              int     `json:"depth"`
}

type DiffSummary struct {
	DurationDeltaNs  int64   `json:"duration_delta_ns"`
	DurationDeltaPct float64 `json:"duration_delta_pct"`
	SpansAdded       int     `json:"spans_added"`
	SpansRemoved     int     `json:"spans_removed"`
	DBCallDelta      int     `json:"db_call_delta"`
}

type DiffSessionInfo struct {
	SessionID       string `json:"session_id"`
	Label           string `json:"label"`
	TotalDurationNs int64  `json:"total_duration_ns"`
	SpanCount       int    `json:"span_count"`
	DBCalls         int    `json:"db_calls"`
}

type DiffResult struct {
	Baseline      DiffSessionInfo    `json:"baseline"`
	Compare       DiffSessionInfo    `json:"compare"`
	Summary       DiffSummary        `json:"summary"`
	Spans         []DiffSpan         `json:"spans"`
	BaselineSpans []*storage.Span    `json:"baseline_spans"`
	CompareSpans  []*storage.Span    `json:"compare_spans"`
}

// ── handler ────────────────────────────────────────────────────────────────────

func (r *Router) getDiff(w http.ResponseWriter, req *http.Request) {
	q := req.URL.Query()
	baselineID := q.Get("baseline")
	compareID := q.Get("compare")

	if baselineID == "" || compareID == "" {
		respondErr(w, req, 400, "baseline and compare query params required")
		return
	}

	baseSess, err := r.store.GetSession(baselineID)
	if err != nil || baseSess == nil {
		respondErr(w, req, 404, "baseline session not found")
		return
	}
	cmpSess, err := r.store.GetSession(compareID)
	if err != nil || cmpSess == nil {
		respondErr(w, req, 404, "compare session not found")
		return
	}

	baseSpans, err := r.store.GetSpansBySession(baselineID)
	if err != nil {
		respondErr(w, req, 500, err.Error())
		return
	}
	cmpSpans, err := r.store.GetSpansBySession(compareID)
	if err != nil {
		respondErr(w, req, 500, err.Error())
		return
	}

	result := computeDiff(baseSess, cmpSess, baseSpans, cmpSpans)
	respond(w, result, 1, 1)
}

// ── diff algorithm ─────────────────────────────────────────────────────────────

const zeroParent = "0000000000000000"

func isRoot(s *storage.Span) bool {
	return s.ParentSpanID == "" || s.ParentSpanID == zeroParent
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
	// build parent->children map to compute depth
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

func computeDiff(
	baseSess, cmpSess *storage.Session,
	baseSpans, cmpSpans []*storage.Span,
) DiffResult {
	baseAvg := avgDurationByKey(baseSpans)
	cmpAvg := avgDurationByKey(cmpSpans)
	baseDepth := depthByKey(baseSpans)
	cmpDepth := depthByKey(cmpSpans)

	// Pick the most representative trace's root spans for the waterfall display.
	// Use all spans from the session — the frontend picks which trace to show.
	baseRoot := rootDuration(baseSpans)
	cmpRoot := rootDuration(cmpSpans)

	baseDB := dbCallCount(baseSpans)
	cmpDB := dbCallCount(cmpSpans)

	baseInfo := DiffSessionInfo{
		SessionID:       baseSess.ID,
		Label:           baseSess.Label,
		TotalDurationNs: baseRoot,
		SpanCount:       len(baseSpans),
		DBCalls:         baseDB,
	}
	cmpInfo := DiffSessionInfo{
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

	var diffSpans []DiffSpan
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

		diffSpans = append(diffSpans, DiffSpan{
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

	summary := DiffSummary{
		DurationDeltaNs:  durationDeltaNs,
		DurationDeltaPct: durationDeltaPct,
		SpansAdded:       added,
		SpansRemoved:     removed,
		DBCallDelta:      cmpDB - baseDB,
	}

	return DiffResult{
		Baseline:      baseInfo,
		Compare:       cmpInfo,
		Summary:       summary,
		Spans:         diffSpans,
		BaselineSpans: baseSpans,
		CompareSpans:  cmpSpans,
	}
}
