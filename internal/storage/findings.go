package storage

import (
	"fmt"

	"github.com/zfogg/spaniel/internal/model"
	"gorm.io/gorm/clause"
)

type LintWarning = model.LintWarning

type TraceIssue = model.TraceIssue

func (d *DB) InsertLintWarning(w *LintWarning) error {
	if err := d.namedQuery("InsertLintWarning").LintWarning.Create(w); err != nil {
		return err
	}
	return d.RecordNotification(&NotificationRecord{
		Source: "lint", SourceID: w.TraceID + ":" + w.SpanID, Severity: w.Severity,
		Title: "Lint: " + w.RuleID, Body: w.Message, Link: "/lint",
		DedupeKey: "lint:" + w.RuleID + ":" + w.SpanID,
	})
}

// InsertMissingTraceLints makes repeated analysis and late-arriving spans
// idempotent. One notification summarizes the rule; every offending span still
// gets its own lint record.
func (d *DB) InsertMissingTraceLints(traceID, ruleID string, warnings []*LintWarning) (int, error) {
	if len(warnings) == 0 {
		return 0, nil
	}
	d.traceLintMu.Lock()
	defer d.traceLintMu.Unlock()
	q := d.namedQuery("TraceLintExisting")
	existing, err := q.LintWarning.Where(q.LintWarning.TraceID.Eq(traceID), q.LintWarning.RuleID.Eq(ruleID)).Find()
	if err != nil {
		return 0, err
	}
	seen := make(map[string]bool, len(existing))
	for _, w := range existing {
		seen[w.SpanID] = true
	}
	missing := make([]*LintWarning, 0, len(warnings))
	for _, w := range warnings {
		if !seen[w.SpanID] {
			missing = append(missing, w)
			seen[w.SpanID] = true
		}
	}
	if len(missing) == 0 {
		return 0, nil
	}
	if err = d.namedQuery("TraceLintInsertMissing").LintWarning.Create(missing...); err != nil {
		return 0, err
	}
	w := missing[0]
	err = d.RecordNotification(&NotificationRecord{
		Source: "lint", SourceID: traceID + ":" + ruleID, Severity: w.Severity,
		Title: "Lint: " + ruleID, Body: w.Message, Link: "/traces/" + traceID,
		DedupeKey: "trace-lint:" + traceID + ":" + ruleID,
	})
	return len(missing), err
}

func (d *DB) ListLintWarnings(sessionID string) ([]*LintWarning, error) {
	rows, err := d.query.LintWarning.ListWithTraceIssues(sessionID)
	if err != nil {
		return nil, err
	}
	result := make([]*LintWarning, len(rows))
	for i := range rows {
		result[i] = &rows[i]
	}
	return result, nil
}

func (d *DB) UpsertTraceIssue(issue *TraceIssue) error {
	// DuckDB rejects ON CONFLICT assignments to indexed columns even when
	// their values do not change. A finding's identity is immutable; only its
	// accumulated evidence changes when late spans trigger another analysis.
	if err := d.namedQuery("UpsertTraceIssue").TraceIssue.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"count", "wasted_ns", "parent_span_id", "example_span_id", "created_at"}),
	}).Create(issue); err != nil {
		return err
	}
	return d.RecordNotification(&NotificationRecord{
		Source: "issue", SourceID: issue.ID, Severity: "warning",
		Title: "Performance issues", Body: fmt.Sprintf("%s (%d)", issue.Kind, issue.Count),
		Link: "/traces/" + issue.TraceID, DedupeKey: "issue:" + issue.ID,
	})
}

func (d *DB) GetTraceIssues(traceID string) ([]*TraceIssue, error) {
	result, err := d.query.TraceIssue.Where(d.query.TraceIssue.TraceID.Eq(traceID)).
		Order(d.query.TraceIssue.WastedNs.Desc()).Find()
	if err != nil {
		return nil, err
	}
	if result == nil {
		result = []*TraceIssue{}
	}
	return result, nil
}

// ListTraceIssuesBySession returns every detector finding for a session.
func (d *DB) ListTraceIssuesBySession(sessionID string) ([]*TraceIssue, error) {
	return d.query.TraceIssue.Where(d.query.TraceIssue.SessionID.Eq(sessionID)).
		Order(d.query.TraceIssue.WastedNs.Desc()).Find()
}
