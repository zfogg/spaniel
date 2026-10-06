package storage

import (
	"fmt"
	"strings"

	"github.com/zfogg/spaniel/internal/model"
)

// SearchResult is one item returned by the global search.
type SearchResult = model.SearchResult

// parseFilters splits a query into field:value filters and the remaining
// free-text terms. e.g. "lint:n+1 orders" -> {"lint":"n+1"}, "orders".
func parseFilters(query string) (map[string]string, string) {
	filters := map[string]string{}
	var rest []string
	for _, tok := range strings.Fields(query) {
		if k, v, ok := strings.Cut(tok, ":"); ok && k != "" && v != "" {
			filters[strings.ToLower(k)] = v
			continue
		}
		rest = append(rest, tok)
	}
	return filters, strings.Join(rest, " ")
}

// Search runs a cross-table search against spans, logs, sessions, and services.
// It also supports field:value filters; currently lint:<rule> (with the alias
// n+1 == n_plus_one) which returns traces flagged by the linter or detectors.
func (d *DB) Search(query, sessionID string, limit int) ([]*SearchResult, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}

	filters, freeText := parseFilters(query)
	if rule, ok := filters["lint"]; ok {
		return d.searchLint(rule, sessionID, limit)
	}
	if freeText != "" {
		query = freeText
	}

	pat := "%" + query + "%"
	tracePrefix := query + "%"
	quarter := limit / 4
	if quarter < 3 {
		quarter = 3
	}

	var results []*SearchResult

	// ── Traces (spans grouped by trace_id) ───────────────────────────────────
	traces, err := d.query.Span.SearchTraces(sessionID, pat, tracePrefix, quarter)
	if err != nil {
		return nil, err
	}
	results = append(results, searchResultPointers(traces)...)

	// ── Spans (individual child spans) ────────────────────────────────────────
	spans, err := d.query.Span.SearchChildSpans(sessionID, pat, quarter)
	if err != nil {
		return nil, err
	}
	results = append(results, searchResultPointers(spans)...)

	// ── Sessions ──────────────────────────────────────────────────────────────
	sessions, err := d.query.Session.SearchByLabel(pat, quarter)
	if err != nil {
		return nil, err
	}
	results = append(results, searchResultPointers(sessions)...)

	// ── Services ──────────────────────────────────────────────────────────────
	services, err := d.query.Span.SearchServices(sessionID, pat, quarter)
	if err != nil {
		return nil, err
	}
	results = append(results, searchResultPointers(services)...)

	// ── Logs ──────────────────────────────────────────────────────────────────
	logs, err := d.query.Log.SearchByBody(sessionID, pat, quarter)
	if err != nil {
		return nil, err
	}
	results = append(results, searchResultPointers(logs)...)

	return results, nil
}

func searchResultPointers(rows []model.SearchResult) []*SearchResult {
	out := make([]*SearchResult, len(rows))
	for i := range rows {
		out[i] = &rows[i]
	}
	return out
}

// searchLint returns traces flagged by the linter or detectors for the given
// rule. The rule "n+1" (and "n1") is aliased to the detector kind "n_plus_one";
// any other value is matched against lint_warnings.rule_id.
func (d *DB) searchLint(rule, sessionID string, limit int) ([]*SearchResult, error) {
	norm := strings.ToLower(strings.TrimSpace(rule))
	var results []*SearchResult

	if norm == "n+1" || norm == "n1" || norm == "n_plus_one" {
		rows, err := d.query.TraceIssue.SearchNPlusOne(sessionID, limit)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			results = append(results, &SearchResult{
				Kind:      "trace",
				TraceID:   row.TraceID,
				Title:     row.Title,
				Subtitle:  fmt.Sprintf("N+1 · %d queries", row.Count),
				SessionID: row.SessionID,
			})
		}
		return results, nil
	}

	// Other lint rules: match lint_warnings.rule_id, grouped by trace.
	rows, err := d.query.LintWarning.SearchByRule(sessionID, "%"+rule+"%", limit)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		results = append(results, &SearchResult{
			Kind:      "trace",
			TraceID:   row.TraceID,
			Title:     row.Title,
			Subtitle:  "lint · " + row.RuleID,
			SessionID: row.SessionID,
		})
	}
	return results, nil
}
