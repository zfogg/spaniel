package ingestion

import (
	"fmt"

	"github.com/zfogg/spaniel/internal/storage"
)

const (
	serialPromiseMinChildren    = 3
	serialPromiseMinChildFrac   = 0.8 // children must account for ≥ 80% of parent duration
)

// SerialPromiseDetector fires when ≥ 3 sequential outbound calls under one
// parent could be parallelised (total child time ≈ parent duration).
type SerialPromiseDetector struct{}

func (*SerialPromiseDetector) Kind() string { return "serial_promise" }

func (*SerialPromiseDetector) Analyze(traceID, sessionID string, spans []*storage.Span, now int64) []*storage.TraceIssue {
	// Build parent → outbound-children index.
	kids := map[string][]*storage.Span{}
	for _, s := range spans {
		if s.ParentSpanID != "" && isClientSpan(s) {
			kids[s.ParentSpanID] = append(kids[s.ParentSpanID], s)
		}
	}

	var issues []*storage.TraceIssue
	for _, parent := range spans {
		children := kids[parent.SpanID]
		if len(children) < serialPromiseMinChildren {
			continue
		}
		if !areSerial(children) {
			continue
		}
		parentDur := parent.EndNs - parent.StartNs
		if parentDur <= 0 {
			continue
		}
		var totalChildNs, maxChildNs int64
		for _, c := range children {
			d := c.EndNs - c.StartNs
			totalChildNs += d
			if d > maxChildNs {
				maxChildNs = d
			}
		}
		if float64(totalChildNs) < serialPromiseMinChildFrac*float64(parentDur) {
			continue
		}
		// Savings = what parallel execution would save.
		wasted := totalChildNs - maxChildNs
		if wasted <= 0 {
			continue
		}
		issues = append(issues, &storage.TraceIssue{
			ID:            issueID(traceID, "serial_promise", parent.SpanID),
			TraceID:       traceID,
			SessionID:     sessionID,
			Kind:          "serial_promise",
			Fingerprint:   fmt.Sprintf("%s (%d serial calls)", parent.Name, len(children)),
			Count:         len(children),
			WastedNs:      wasted,
			ParentSpanID:  parent.ParentSpanID,
			ExampleSpanID: parent.SpanID,
			CreatedAt:     now,
		})
	}
	return issues
}
