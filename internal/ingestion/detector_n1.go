package ingestion

import (
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/xwb1989/sqlparser"
	"github.com/zfogg/spaniel/internal/ingestion/linter"
	"github.com/zfogg/spaniel/internal/storage"
)

// N1Detector fires when the same SQL fingerprint is executed more than once
// under a single parent operation. Two executions are already an N+1: a loop
// of one item is simply the smallest observable case.
type N1Detector struct{}

func (*N1Detector) Kind() string { return "n_plus_one" }

func (*N1Detector) Analyze(traceID, sessionID string, spans []*storage.Span, now int64) []*storage.TraceIssue {
	type group struct {
		spans []*storage.Span
		fp    string
	}
	groups := map[string]*group{}
	seen := map[string]bool{}
	for _, s := range spans {
		// Retransmitting an exported span is not another query execution.
		if s.SpanID != "" {
			if seen[s.SpanID] {
				continue
			}
			seen[s.SpanID] = true
		}
		// Check both db.statement (older OTel convention) and db.query.text
		// (newer convention used by otelpgx and other modern instrumentations).
		raw := extractAttrString(s.Attributes, "db.statement")
		if raw == "" {
			raw = extractAttrString(s.Attributes, "db.query.text")
		}
		if raw == "" {
			continue
		}
		if isN1LifecycleSpan(s, raw) {
			continue
		}
		fp := fingerprintSQL(raw)
		// SQL reused by independent operations is not an N+1. Keep the parent
		// in the key so the issue points at the loop that issued it.
		key := s.ParentSpanID + "\x00" + fp
		if g, ok := groups[key]; ok {
			g.spans = append(g.spans, s)
		} else {
			groups[key] = &group{spans: []*storage.Span{s}, fp: fp}
		}
	}

	var issues []*storage.TraceIssue
	for _, g := range groups {
		if len(g.spans) < 2 {
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
			// SQL templates commonly share their first 12 characters. Hash
			// the full parent/template key instead of truncating the SQL.
			ID:            fmt.Sprintf("%s-n_plus_one-%x", traceID, sha256.Sum256([]byte(loopSpanID+"\x00"+g.fp))),
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

// Preparation and transaction boundaries are not repeated data executions.
// Keep them available to slow/error detectors, which still expose contention.
func isN1LifecycleSpan(span *storage.Span, sql string) bool {
	if linter.IsStatementPreparation(span) {
		return true
	}
	operation := extractAttrString(span.Attributes, "db.operation.name")
	if operation == "" {
		operation = extractAttrString(span.Attributes, "db.operation")
	}
	switch strings.ToUpper(operation) {
	case "BEGIN", "COMMIT", "ROLLBACK", "SAVEPOINT", "RELEASE":
		return true
	}
	if operation != "" {
		return false
	}
	// Older instrumentation may provide only SQL. Parse it rather than
	// matching words that could also occur in a real data query or literal.
	statement, err := sqlparser.Parse(sql)
	if err != nil {
		return false
	}
	switch statement.(type) {
	case *sqlparser.Begin, *sqlparser.Commit, *sqlparser.Rollback:
		return true
	}
	return false
}
