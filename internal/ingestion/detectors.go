package ingestion

import (
	"strings"
	"time"

	"vitess.io/vitess/go/vt/sqlparser"

	"github.com/zfogg/spaniel/internal/storage"
)

// fingerprintSQL normalizes a SQL statement by replacing all literal values
// with ? using the Vitess SQL parser. Falls back to raw statement on parse error.
func fingerprintSQL(stmt string) string {
	parser, err := sqlparser.New(sqlparser.Options{})
	if err != nil {
		return fallbackFingerprint(stmt)
	}
	parsed, err := parser.Parse(stmt)
	if err != nil {
		return fallbackFingerprint(stmt)
	}
	buf := sqlparser.NewTrackedBuffer(func(buf *sqlparser.TrackedBuffer, node sqlparser.SQLNode) {
		switch node.(type) {
		case *sqlparser.Literal:
			buf.WriteString("?")
		case sqlparser.ListArg:
			buf.WriteString("(?)")
		default:
			node.Format(buf)
		}
	})
	parsed.Format(buf)
	return strings.TrimSpace(buf.String())
}

// fallbackFingerprint uses a simple approach when the Vitess parser fails
// (non-standard SQL dialects, truncated statements, etc.)
func fallbackFingerprint(stmt string) string {
	fields := strings.Fields(stmt)
	return strings.Join(fields, " ")
}

// runDetectors runs all post-ingestion trace analysis after the 500ms quiet window.
func runDetectors(traceID string, store *storage.DB) {
	detectN1(traceID, store)
}

func detectN1(traceID string, store *storage.DB) {
	spans, err := store.GetTrace(traceID)
	if err != nil || len(spans) == 0 {
		return
	}

	// Group DB spans by statement fingerprint
	type group struct {
		spans []*storage.Span
		fp    string
	}
	groups := map[string]*group{}
	for _, s := range spans {
		if !findSubstr(s.Attributes, `"db.statement"`) {
			continue
		}
		raw := extractAttrString(s.Attributes, "db.statement")
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

	sessionID := store.ActiveSessionID()
	now := time.Now().UnixNano()

	for _, g := range groups {
		if len(g.spans) < 10 {
			continue
		}

		// Sum total duration of all repeated calls
		var totalNs int64
		parentCounts := map[string]int{}
		for _, s := range g.spans {
			if s.EndNs > s.StartNs {
				totalNs += s.EndNs - s.StartNs
			}
			parentCounts[s.ParentSpanID]++
		}

		// Find the most common parent span (the loop)
		var loopSpanID string
		maxCnt := 0
		for pid, cnt := range parentCounts {
			if cnt > maxCnt {
				maxCnt = cnt
				loopSpanID = pid
			}
		}

		issue := &storage.TraceIssue{
			ID:            traceID + "-n1-" + itoa(len(g.fp)),
			TraceID:       traceID,
			SessionID:     sessionID,
			Kind:          "n_plus_one",
			Fingerprint:   g.fp,
			Count:         len(g.spans),
			WastedNs:      totalNs,
			ParentSpanID:  loopSpanID,
			ExampleSpanID: g.spans[0].SpanID,
			CreatedAt:     now,
		}
		_ = store.UpsertTraceIssue(issue)
	}
}
