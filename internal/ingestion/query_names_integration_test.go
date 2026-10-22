package ingestion

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/zfogg/spaniel/internal/ingestion/linter"
	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/ws"
	"go.opentelemetry.io/otel"
)

func queryNameSpan(id, traceID, name, text string) *storage.Span {
	attrs, _ := json.Marshal(map[string]string{"db.query.text": text, "db.system.name": "postgresql"})
	start := int64(1000 + len(id)*100 + int(id[0]))
	return &storage.Span{SpanID: id, TraceID: traceID, Name: name, ServiceName: "ratio-api", Kind: 3, Attributes: string(attrs), StartNs: start, EndNs: start + 1000}
}

func TestStorageQueryNameCollisionFlagsEveryOccurrence(t *testing.T) {
	spans := []*storage.Span{
		queryNameSpan("a", "trace", "ratio.storage.Shared", "SELECT cash FROM accounts"),
		queryNameSpan("b", "trace", "ratio.storage.Shared", "SELECT cash FROM accounts"),
		queryNameSpan("c", "trace", "ratio.storage.Shared", "SELECT balance FROM pair_escrows"),
		queryNameSpan("d", "other", "ratio.storage.Shared", "SELECT supply FROM pairs"),
		queryNameSpan("e", "trace", "ratio.storage.Separate", "SELECT supply FROM pairs"),
		queryNameSpan("f", "trace", "HTTP GET", "SELECT supply FROM pairs"),
		queryNameSpan("g", "trace", "HTTP GET", "SELECT cash FROM accounts"),
		queryNameSpan("h", "trace", "ratio.storage.Shared", ""),
	}
	warnings := linter.QueryNameLints(spans, 123)
	if len(warnings) != 3 {
		t.Fatalf("got %d warnings, want all 3 colliding spans", len(warnings))
	}
	seen := map[string]bool{}
	for _, w := range warnings {
		seen[w.SpanID] = true
		if w.RuleID != linter.StorageQueryNameRule {
			t.Fatal(w)
		}
	}
	for _, id := range []string{"a", "b", "c"} {
		if !seen[id] {
			t.Fatalf("missed earlier/repeated span %s", id)
		}
	}
}

func TestStorageQueryNameIdenticalTemplatesAreAllowed(t *testing.T) {
	spans := []*storage.Span{queryNameSpan("a", "t", "app.storage.Account", "SELECT id FROM accounts WHERE id=?"), queryNameSpan("b", "t", "app.storage.Account", "SELECT id FROM accounts WHERE id=?")}
	if got := linter.QueryNameLints(spans, 0); len(got) != 0 {
		t.Fatal(got)
	}
}

func TestStorageQueryNamePreparationUsesMetadataNotName(t *testing.T) {
	for _, attrs := range []string{
		`"db.operation.name":"PREPARE"`,
		`"db.operation.name":"prepare"`,
		`"db.operation":"PREPARE"`,
		`"db.operation.name":"SELECT","db.operation.already_prepared":false`,
		`"db.operation.name":"SELECT","db.operation.already_prepared":true`,
	} {
		a := queryNameSpan("a", "t", "other.storage.Arbitrary", "SELECT a FROM a")
		b := queryNameSpan("b", "t", "other.storage.Arbitrary", "SELECT b FROM b")
		for _, s := range []*storage.Span{a, b} {
			s.Attributes = strings.TrimSuffix(s.Attributes, "}") + "," + attrs + "}"
		}
		if got := linter.QueryNameLints([]*storage.Span{a, b}, 0); len(got) != 0 {
			t.Fatalf("prepare metadata %s: %+v", attrs, got)
		}
	}
	// A real named query is still checked even when its name is Prepare.
	spans := []*storage.Span{
		queryNameSpan("a", "t", "app.storage.Prepare", "SELECT a FROM a"),
		queryNameSpan("b", "t", "app.storage.Prepare", "SELECT b FROM b"),
	}
	if got := linter.QueryNameLints(spans, 0); len(got) != 2 {
		t.Fatalf("name alone suppressed collision: %+v", got)
	}
}

func TestStorageQueryNameLateSpansPersistWithoutDuplicateWarnings(t *testing.T) {
	db := openTestDB(t)
	session, err := db.CreateSession("query-names", false)
	if err != nil {
		t.Fatal(err)
	}
	db.SetActiveSession(session.ID, session.Label)
	add := func(id, text string) {
		s := queryNameSpan(id, "late", "ratio.storage.Shared", text)
		s.SessionID = session.ID
		if err := db.InsertSpan(s); err != nil {
			t.Fatal(err)
		}
		if err := db.FlushBatch(); err != nil {
			t.Fatal(err)
		}
	}
	hub := ws.NewHub()
	tracer := otel.Tracer("query-name-test")
	add("first", "SELECT id FROM accounts")
	runDetectors("late", db, hub, tracer)
	add("second", "SELECT id FROM pairs")
	runDetectors("late", db, hub, tracer)
	add("third", "SELECT id FROM accounts")
	runDetectors("late", db, hub, tracer)
	runDetectors("late", db.WithContext(t.Context()), hub, tracer)
	warnings, err := db.ListLintWarnings(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, w := range warnings {
		if w.RuleID == linter.StorageQueryNameRule {
			count++
		}
	}
	if count != 3 {
		t.Fatalf("got %d persisted warnings, want exactly 3", count)
	}
	_, total, err := db.ListNotifications("lint", 1, 100)
	if err != nil || total != 1 {
		t.Fatalf("notification flood: count=%d err=%v warnings=%+v", total, err, warnings)
	}
}

func TestChildStartingBeforeParentPersistsOnce(t *testing.T) {
	db := openTestDB(t)
	session, err := db.CreateSession("child-start-order", false)
	if err != nil {
		t.Fatal(err)
	}
	db.SetActiveSession(session.ID, session.Label)
	for _, span := range []*storage.Span{
		{SessionID: session.ID, TraceID: "temporal", SpanID: "workflow", Name: "ratio.temporal.workflow.LifecycleWorkflow", StartNs: 1_000_000_000, EndNs: 2_000_000_000},
		{SessionID: session.ID, TraceID: "temporal", SpanID: "schedule", ParentSpanID: "workflow", Name: "ratio.temporal.activity.schedule.Lifecycle", StartNs: 998_900_800, EndNs: 1_000_000_100},
	} {
		if err := db.InsertSpan(span); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.FlushBatch(); err != nil {
		t.Fatal(err)
	}
	hub := ws.NewHub()
	tracer := otel.Tracer("child-start-order-test")
	runDetectors("temporal", db, hub, tracer)
	runDetectors("temporal", db, hub, tracer)

	warnings, err := db.ListLintWarnings(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, warning := range warnings {
		if warning.RuleID == linter.ChildStartsBeforeParentRule {
			count++
			if warning.SpanID != "schedule" {
				t.Fatalf("warning attached to %q, want scheduling span", warning.SpanID)
			}
		}
	}
	if count != 1 {
		t.Fatalf("got %d timestamp-order warnings, want exactly 1: %+v", count, warnings)
	}
}

func TestStorageQueryNameBufferedSamplerAuditsLateSpans(t *testing.T) {
	db := openTestDB(t)
	session, err := db.CreateSession("buffered-query-names", false)
	if err != nil {
		t.Fatal(err)
	}
	db.SetActiveSession(session.ID, session.Label)
	p := NewPipelineWithSampler(db, ws.NewHub(), NewSampler(100000, KeepNPlusOne))
	if !p.sampler.NeedsBuffer() {
		t.Fatal("test must exercise buffered sampling")
	}
	for _, s := range []*storage.Span{
		queryNameSpan("a", "buffered", "ratio.storage.Shared", "SELECT cash FROM accounts"),
		queryNameSpan("b", "buffered", "ratio.storage.Shared", "SELECT balance FROM pair_escrows"),
	} {
		s.SessionID = session.ID
		p.bufferSpan("buffered", s, nil, nil)
	}
	p.flushBuffer("buffered")
	late := queryNameSpan("c", "buffered", "ratio.storage.Shared", "SELECT cash FROM accounts")
	late.SessionID = session.ID
	p.bufferSpan("buffered", late, nil, nil)
	p.flushBuffer("buffered")
	warnings, err := db.ListLintWarnings(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, w := range warnings {
		if w.RuleID == linter.StorageQueryNameRule {
			count++
		}
	}
	if count != 3 {
		t.Fatalf("buffered sampling lost query-name lints: got %d want 3", count)
	}
}

func TestStorageQueryNameConcurrentAnalysisDeduplicates(t *testing.T) {
	db := openTestDB(t)
	warnings := linter.QueryNameLints([]*storage.Span{
		queryNameSpan("a", "concurrent", "app.storage.Shared", "SELECT id FROM accounts"),
		queryNameSpan("b", "concurrent", "app.storage.Shared", "SELECT id FROM pairs"),
	}, 123)
	var work sync.WaitGroup
	results := make(chan int, 8)
	errors := make(chan error, 8)
	for range 8 {
		work.Go(func() {
			count, err := db.WithContext(t.Context()).InsertMissingTraceLints("concurrent", linter.StorageQueryNameRule, warnings)
			results <- count
			errors <- err
		})
	}
	work.Wait()
	close(results)
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	total := 0
	for count := range results {
		total += count
	}
	if total != 2 {
		t.Fatalf("concurrent analysis inserted %d warnings, want 2", total)
	}
}
