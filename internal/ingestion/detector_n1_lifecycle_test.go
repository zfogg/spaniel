package ingestion

import (
	"encoding/json"
	"testing"

	"github.com/zfogg/spaniel/internal/storage"
)

func TestN1LifecycleBoundariesAndPrepareAreNotDataLoops(t *testing.T) {
	for _, tc := range []struct {
		name, sql string
		attrs     map[string]any
	}{
		{"legacy begin", "/* driver transaction */ BEGIN", nil},
		{"legacy rollback", "ROLLBACK", nil},
		{"legacy commit", "COMMIT", nil},
		{"current begin", "BEGIN", map[string]any{"db.operation.name": "BEGIN"}},
		{"legacy metadata", "ROLLBACK", map[string]any{"db.operation": "rollback"}},
		{"savepoint", "SAVEPOINT batch", map[string]any{"db.operation.name": "SAVEPOINT"}},
		{"release", "RELEASE SAVEPOINT batch", map[string]any{"db.operation.name": "RELEASE"}},
		{"explicit prepare", "SELECT * FROM accounts WHERE id=?", map[string]any{"db.operation.name": "PREPARE"}},
		{"first pgx prepare", "SELECT * FROM accounts WHERE id=?", map[string]any{"db.operation.name": "SELECT", "db.operation.already_prepared": false}},
		{"reused pgx prepare", "SELECT * FROM accounts WHERE id=?", map[string]any{"db.operation.name": "SELECT", "db.operation.already_prepared": true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var spans []*storage.Span
			for i := range 3 {
				attrs := map[string]any{"db.query.text": tc.sql, "db.system.name": "postgresql"}
				for k, v := range tc.attrs {
					attrs[k] = v
				}
				raw, err := json.Marshal(attrs)
				if err != nil {
					t.Fatal(err)
				}
				span := makeDBSpan(itoa(i), "parent", tc.sql, 0)
				span.Name = "arbitrary.operation"
				span.Attributes = string(raw)
				spans = append(spans, span)
			}
			if got := (&N1Detector{}).Analyze("trace", "session", spans, 0); len(got) != 0 {
				t.Fatalf("driver lifecycle classified as N+1: %+v", got)
			}
		})
	}
}

func TestN1LifecycleFilterPreservesRealQueriesAndAdvisoryLocks(t *testing.T) {
	for _, sql := range []string{
		"SELECT pg_advisory_xact_lock(hashtextextended('tax-fill:account',0))",
		"SELECT id FROM accounts WHERE status='BEGIN'",
		"UPDATE leases SET heartbeat_at=now() WHERE lease_key='COMMIT'",
		"INSERT INTO cycle_phases(phase) VALUES ('ROLLBACK')",
		"SELECT * FROM prepare_orders WHERE id=1",
	} {
		spans := []*storage.Span{makeDBSpan("one", "parent", sql, 0), makeDBSpan("two", "parent", sql, 0)}
		for _, span := range spans {
			span.Name = "app.storage.prepare"
		}
		if got := (&N1Detector{}).Analyze("trace", "session", spans, 0); len(got) != 1 || got[0].Count != 2 {
			t.Fatalf("real repeated query hidden: %s %+v", sql, got)
		}
	}
}

func TestN1LifecycleFilterDoesNotSuppressSlowDatabaseFindings(t *testing.T) {
	spans := []*storage.Span{makeDBSpan("one", "parent", "BEGIN", 0), makeDBSpan("two", "parent", "BEGIN", 0)}
	for _, span := range spans {
		span.EndNs = 1_000_000_000
	}
	if got := (&N1Detector{}).Analyze("trace", "session", spans, 0); len(got) != 0 {
		t.Fatalf("transaction classified as N+1: %+v", got)
	}
	if got := (&SlowDBDetector{}).Analyze("trace", "session", spans, 0); len(got) != 2 {
		t.Fatalf("slow transaction findings hidden: %+v", got)
	}
}
