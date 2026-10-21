package ingestion

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/ws"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/otel"
)

func TestStorageQueryNamePreparationDoesNotHideProcedureOrNamedQueryCollisions(t *testing.T) {
	for _, extra := range []map[string]any{
		{},
		{"db.stored_procedure.name": "stmtcache_6c77d355"},
		{"db.operation.name": "EXECUTE", "db.stored_procedure.name": "business_procedure"},
		{"db.operation.name": "SELECT", "db.operation.already_prepared": "false"},
		{"db.operation.name": "SELECT", "db.operation.already_prepared": nil},
		{"db.operation.name": "SELECT", "db.operation.already_prepared": 0},
	} {
		t.Run(fmt.Sprint(extra), func(t *testing.T) {
			var spans []*storage.Span
			for i, text := range []string{"SELECT a FROM a", "SELECT b FROM b"} {
				attrs := map[string]any{"db.system.name": "postgresql", "db.query.text": text}
				for k, v := range extra {
					attrs[k] = v
				}
				raw, err := json.Marshal(attrs)
				if err != nil {
					t.Fatal(err)
				}
				span := queryNameSpan(fmt.Sprint(i), "t", "other.storage.Prepare", text)
				span.Attributes = string(raw)
				spans = append(spans, span)
			}
			if got := storageQueryNameLints(spans, 0); len(got) != 2 {
				t.Fatalf("real executions exempted by incidental metadata: %+v", got)
			}
		})
	}
}

// Exercise OTLP -> attribute conversion -> persisted spans -> trace analysis ->
// persisted warnings. In particular, false must remain a present boolean.
func TestStorageQueryNamePrepareOTLPAndLateRealQueries(t *testing.T) {
	store := openTestDB(t)
	session, err := store.CreateSession("prepare-trace-regression", false)
	if err != nil {
		t.Fatal(err)
	}
	store.SetActiveSession(session.ID, session.Label)
	hub := ws.NewHub()
	pipeline := NewPipeline(store, hub)
	t.Cleanup(func() {
		// The batching change adds Close; committed synchronous pipelines only
		// own debounce timers. Keep this regression independently deployable.
		if closer, ok := any(pipeline).(interface{ Close(context.Context) error }); ok {
			_ = closer.Close(context.Background())
			return
		}
		pipeline.debounceMu.Lock()
		defer pipeline.debounceMu.Unlock()
		for _, timer := range pipeline.debounce {
			timer.Stop()
		}
	})
	tracer := otel.Tracer("prepare-regression")
	preparations := makeTraces("stonkpile-server", func(ss ptrace.ScopeSpans) {
		texts := []string{
			"SELECT pg_try_advisory_lock(hashtextextended(?, ?))::bool",
			"SELECT EXISTS(SELECT ? FROM tax_shared_summary WHERE profile_hash = ?)",
			"SELECT pg_advisory_unlock(hashtextextended(?, ?))",
		}
		for i, text := range texts {
			putSpan(ss, "c", fmt.Sprint(i), "", "stonkpile.storage.Prepare", ptrace.SpanKindClient, map[string]string{
				"db.system.name": "postgresql", "db.namespace": "stonkpile", "db.operation.name": "SELECT",
				"db.query.text": text, "db.stored_procedure.name": fmt.Sprintf("stmtcache_%d", i),
			})
			ss.Spans().At(i).Attributes().PutBool("db.operation.already_prepared", i == 2)
		}
	})
	traceID := preparations.ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(0).TraceID().String()
	ingest := func(td ptrace.Traces) {
		t.Helper()
		if err := pipeline.IngestTraces(t.Context(), td); err != nil {
			t.Fatal(err)
		}
		// Run the normal detector synchronously rather than sleeping for debounce.
		pipeline.debounceMu.Lock()
		if timer := pipeline.debounce[traceID]; timer != nil {
			timer.Stop()
			delete(pipeline.debounce, traceID)
		}
		pipeline.debounceMu.Unlock()
		runDetectors(traceID, store, hub, tracer)
	}
	warnings := func() map[string]bool {
		t.Helper()
		all, err := store.ListLintWarnings(session.ID)
		if err != nil {
			t.Fatal(err)
		}
		ids := map[string]bool{}
		for _, w := range all {
			if w.RuleID == storageQueryNameRule {
				ids[w.SpanID] = true
			}
		}
		return ids
	}
	ingest(preparations)
	if got := warnings(); len(got) != 0 {
		t.Fatalf("real pgx preparation triggered collisions: %v", got)
	}
	// A real query named Prepare is legal. Preparation spans sharing that name
	// must neither cause collisions nor suppress collisions between executions.
	first := makeTraces("stonkpile-server", func(ss ptrace.ScopeSpans) {
		putSpan(ss, "c", "a", "", "stonkpile.storage.Prepare", ptrace.SpanKindClient, map[string]string{"db.system.name": "postgresql", "db.query.text": "SELECT id FROM accounts", "db.operation.name": "SELECT"})
	})
	ingest(first)
	if got := warnings(); len(got) != 0 {
		t.Fatalf("prepare templates contaminated genuine query: %v", got)
	}
	second := makeTraces("stonkpile-server", func(ss ptrace.ScopeSpans) {
		putSpan(ss, "c", "b", "", "stonkpile.storage.Prepare", ptrace.SpanKindClient, map[string]string{"db.system.name": "postgresql", "db.query.text": "SELECT balance FROM escrows", "db.operation.name": "SELECT"})
	})
	ingest(second)
	got := warnings()
	for _, td := range []ptrace.Traces{first, second} {
		id := td.ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(0).SpanID().String()
		if !got[id] {
			t.Fatalf("late real collision missed: %v", got)
		}
	}
	if len(got) != 2 {
		t.Fatalf("expected only two execution warnings: %v", got)
	}
	// Retransmission and repeated analysis must not duplicate warnings.
	ingest(second)
	all, err := store.ListLintWarnings(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, w := range all {
		if w.RuleID == storageQueryNameRule {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("retransmission duplicated lints: %d", count)
	}
}
