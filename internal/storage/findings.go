package storage

import "github.com/zfogg/spaniel/internal/model"

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
	return d.namedQuery("UpsertTraceIssue").TraceIssue.Save(issue)
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
