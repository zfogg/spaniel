package api

import (
	"context"
	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/ws"
	"path/filepath"
	"testing"
	"time"
)

func TestEventAlertAcknowledgementReplayAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.duckdb")
	store, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	rule := &storage.AlertRule{ID: "events", Name: "Events", QuerySQL: "SELECT 'a' AS event_id", ConditionJSON: `{"kind":"event"}`, GroupByJSON: `[]`, Enabled: true, BrowserEnabled: true}
	if err := store.CreateAlertRule(rule); err != nil {
		t.Fatal(err)
	}
	evaluate := func(sql string) *storage.AlertRule {
		t.Helper()
		r, err := store.GetAlertRule("events")
		if err != nil {
			t.Fatal(err)
		}
		r.QuerySQL = sql
		writes := newTestAlertWriteBuffer(store)
		queries := &alertQueryCache{results: map[string]alertQueryResult{}}
		if err := evaluateAlertRule(context.Background(), store, writes, queries, ws.NewHub(), r, []*storage.AlertRule{r}, time.Now()); err != nil {
			t.Fatal(err)
		}
		if err := writes.flush(); err != nil {
			t.Fatal(err)
		}
		got, err := store.GetAlertRule("events")
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	both := "SELECT 'a' AS event_id UNION ALL SELECT 'b' AS event_id"
	first := evaluate("SELECT 'a' AS event_id UNION ALL SELECT 'a' AS event_id")
	if len(first.Instances) != 1 || alertListState(first) != "firing" {
		t.Fatalf("first event: %#v", first.Instances)
	}
	if _, _, err := store.AcknowledgeAlertInstance("events", first.Instances[0].GroupKey, "handled a"); err != nil {
		t.Fatal(err)
	}
	if got := evaluate("SELECT 'a' AS event_id"); alertListState(got) != "acknowledged" {
		t.Fatalf("acknowledged event rearmed: %#v", got.Instances)
	}
	second := evaluate(both)
	if len(second.Instances) != 2 || alertListState(second) != "firing" {
		t.Fatalf("new event did not fire: %#v", second.Instances)
	}
	for _, i := range second.Instances {
		if i.AcknowledgedAt == nil {
			if _, _, err := store.AcknowledgeAlertInstance("events", i.GroupKey, "handled b"); err != nil {
				t.Fatal(err)
			}
		}
	}
	if got := evaluate(both); alertListState(got) != "acknowledged" {
		t.Fatal("acknowledgements did not suppress attention")
	}
	// Persisted acknowledgement survives restart while the same rows remain true.
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := evaluate(both); alertListState(got) != "acknowledged" {
		t.Fatal("restart rearmed acknowledged events")
	}
	if got := evaluate("SELECT '' AS event_id WHERE false"); alertListState(got) != "resolved" {
		t.Fatal("missing events did not resolve")
	}
	if got := evaluate(both); alertListState(got) != "resolved" {
		t.Fatal("replayed resolved events rearmed")
	}
}

func TestAcknowledgedConditionSuppressesRepeatsAndRearmsAfterRecovery(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "condition.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	rule := &storage.AlertRule{ID: "condition", Name: "Condition", QuerySQL: "SELECT 1 AS value", ConditionJSON: `{"kind":"threshold","operator":">","value":0}`, GroupByJSON: `[]`, Enabled: true, RepeatIntervalNs: 1, BrowserEnabled: true}
	if err := store.CreateAlertRule(rule); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	advance := func(breached bool) *storage.AlertRule {
		r, err := store.GetAlertRule(rule.ID)
		if err != nil {
			t.Fatal(err)
		}
		writes := newTestAlertWriteBuffer(store)
		if err := advanceAlertInstance(writes, ws.NewHub(), r, "all", map[string]string{}, 1, breached, now); err != nil {
			t.Fatal(err)
		}
		if err := writes.flush(); err != nil {
			t.Fatal(err)
		}
		got, err := store.GetAlertRule(rule.ID)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	advance(true)
	if _, _, err := store.AcknowledgeAlertInstance(rule.ID, "all", "handled"); err != nil {
		t.Fatal(err)
	}
	got := advance(true)
	if alertListState(got) != "acknowledged" || alertRepeatDue(ws.NewHub(), got, got.Instances[0], now.Add(time.Hour)) {
		t.Fatal("acknowledged condition still demands attention/repeats")
	}
	if got := advance(false); alertListState(got) != "resolved" {
		t.Fatal("condition did not recover")
	}
	if got := advance(true); alertListState(got) != "firing" || got.Instances[0].AcknowledgedAt != nil {
		t.Fatal("new condition episode did not rearm")
	}
}

func TestEventAlertPreservesLegacyAcknowledgement(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "legacy.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	acknowledged := time.Now().Add(-time.Minute).UnixNano()
	rule := &storage.AlertRule{ID: "legacy", Name: "Legacy", QuerySQL: "SELECT 'old' AS event_id, 1::BIGINT AS event_time_ns UNION ALL SELECT 'new', 9223372036854775807::BIGINT", ConditionJSON: `{"kind":"event"}`, GroupByJSON: `[]`, Enabled: true}
	if err := store.CreateAlertRule(rule); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertAlertInstance(&storage.AlertInstance{RuleID: rule.ID, GroupKey: "all", LabelsJSON: `{}`, State: "firing", AcknowledgedAt: &acknowledged}); err != nil {
		t.Fatal(err)
	}
	rule, err = store.GetAlertRule(rule.ID)
	if err != nil {
		t.Fatal(err)
	}
	writes := newTestAlertWriteBuffer(store)
	if err := evaluateAlertRule(context.Background(), store, writes, &alertQueryCache{results: map[string]alertQueryResult{}}, ws.NewHub(), rule, []*storage.AlertRule{rule}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := writes.flush(); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetAlertRule(rule.ID)
	if err != nil {
		t.Fatal(err)
	}
	var oldHandled, newFiring bool
	for _, i := range got.Instances {
		if i.GroupKey == "event_id=old" {
			oldHandled = i.State == "resolved" && i.AcknowledgedAt != nil
		}
		if i.GroupKey == "event_id=new" {
			newFiring = i.State == "firing" && i.AcknowledgedAt == nil
		}
	}
	if !oldHandled || !newFiring {
		t.Fatalf("migration: %#v", got.Instances)
	}
}
