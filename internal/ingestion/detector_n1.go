package ingestion

import (
	"github.com/zfogg/spaniel/internal/storage"
)

// N1Detector fires when ≥ 10 spans share a SQL fingerprint under the same parent.
type N1Detector struct{}

func (*N1Detector) Kind() string { return "n_plus_one" }

func (*N1Detector) Analyze(traceID, sessionID string, spans []*storage.Span, now int64) []*storage.TraceIssue {
	type group struct {
		spans []*storage.Span
		fp    string
	}
	groups := map[string]*group{}
	for _, s := range spans {
		// Check both db.statement (older OTel convention) and db.query.text
		// (newer convention used by otelpgx and other modern instrumentations).
		raw := extractAttrString(s.Attributes, "db.statement")
		if raw == "" {
			raw = extractAttrString(s.Attributes, "db.query.text")
		}
		if raw == "" {
			continue
		}
		fp := fingerprintSQL(raw)
		if g, ok := groups[fp]; ok {
			g.spans = append(g.spans, s)
		} else {
			groups[fp] = &group{spans: []*storage.Span{s}, fp: fp}
		}
	}

	var issues []*storage.TraceIssue
	for _, g := range groups {
		if len(g.spans) < 10 {
			continue
		}
		var totalNs int64
		parentCounts := map[string]int{}
		for _, s := range g.spans {
			if s.EndNs > s.StartNs {
				totalNs += s.EndNs - s.StartNs
			}
			parentCounts[s.ParentSpanID]++
		}
		var loopSpanID string
		maxCnt := 0
		for pid, cnt := range parentCounts {
			if cnt > maxCnt {
				maxCnt = cnt
				loopSpanID = pid
			}
		}
		issues = append(issues, &storage.TraceIssue{
			ID:            issueID(traceID, "n_plus_one", g.fp),
			TraceID:       traceID,
			SessionID:     sessionID,
			Kind:          "n_plus_one",
			Fingerprint:   g.fp,
			Count:         len(g.spans),
			WastedNs:      totalNs,
			ParentSpanID:  loopSpanID,
			ExampleSpanID: g.spans[0].SpanID,
			CreatedAt:     now,
		})
	}
	return issues
}
