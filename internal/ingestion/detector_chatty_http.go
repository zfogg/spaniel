package ingestion

import (
	"fmt"

	"github.com/zfogg/spaniel/internal/storage"
)

const chattyHTTPThreshold = 5

// ChattyHTTPDetector fires when the same HTTP host is called ≥ 5 times in one trace.
type ChattyHTTPDetector struct{}

func (*ChattyHTTPDetector) Kind() string { return "chatty_http" }

func (*ChattyHTTPDetector) Analyze(traceID, sessionID string, spans []*storage.Span, now int64) []*storage.TraceIssue {
	type group struct {
		spans []*storage.Span
		host  string
	}
	groups := map[string]*group{}
	for _, s := range spans {
		host := httpHost(s.Attributes)
		if host == "" {
			continue
		}
		if g, ok := groups[host]; ok {
			g.spans = append(g.spans, s)
		} else {
			groups[host] = &group{spans: []*storage.Span{s}, host: host}
		}
	}

	var issues []*storage.TraceIssue
	for _, g := range groups {
		if len(g.spans) < chattyHTTPThreshold {
			continue
		}
		var totalNs int64
		parentCounts := map[string]int{}
		for _, s := range g.spans {
			totalNs += s.EndNs - s.StartNs
			parentCounts[s.ParentSpanID]++
		}
		var topParent string
		maxCnt := 0
		for pid, cnt := range parentCounts {
			if cnt > maxCnt {
				maxCnt = cnt
				topParent = pid
			}
		}
		issues = append(issues, &storage.TraceIssue{
			ID:            issueID(traceID, "chatty_http", g.host),
			TraceID:       traceID,
			SessionID:     sessionID,
			Kind:          "chatty_http",
			Fingerprint:   g.host,
			Count:         len(g.spans),
			WastedNs:      totalNs,
			ParentSpanID:  topParent,
			ExampleSpanID: g.spans[0].SpanID,
			CreatedAt:     now,
		})
		_ = fmt.Sprintf // keep import happy
	}
	return issues
}
