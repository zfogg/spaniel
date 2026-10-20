package storage

import "testing"

func TestUpsertTraceIssueWithTraceIndex(t *testing.T) {
	db := openTestDB(t)
	// Older persistent collectors retain this performance index. Fresh test
	// databases may omit it, so reproduce that schema explicitly.
	if _, err := db.SQL().Exec("CREATE INDEX IF NOT EXISTS idx_trace_issues_trace_id ON trace_issues(trace_id)"); err != nil {
		t.Fatal(err)
	}
	issue := &TraceIssue{ID: "indexed-n1", TraceID: "trace", SessionID: "session", Kind: "n_plus_one", Fingerprint: "SELECT channels", Count: 6, WastedNs: 33800000, ParentSpanID: "root", ExampleSpanID: "first"}
	if err := db.UpsertTraceIssue(issue); err != nil {
		t.Fatalf("insert with trace index: %v", err)
	}
	issue.Count = 7
	issue.WastedNs = 40000000
	issue.ExampleSpanID = "late"
	if err := db.UpsertTraceIssue(issue); err != nil {
		t.Fatalf("update with trace index: %v", err)
	}
	got, err := db.GetTraceIssues("trace")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Count != 7 || got[0].WastedNs != 40000000 || got[0].ExampleSpanID != "late" || got[0].SessionID != "session" || got[0].Kind != "n_plus_one" {
		t.Fatalf("finding not updated correctly: %+v", got)
	}
}
