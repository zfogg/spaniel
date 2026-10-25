package storage

import (
	"context"
	"path/filepath"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestListAlertRulesLoadsInstancesInOneQuery(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		_ = provider.Shutdown(context.Background())
	})

	d, err := Open(filepath.Join(t.TempDir(), "alerts.duckdb"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	for _, id := range []string{"rule-a", "rule-b", "rule-c"} {
		if err := d.CreateAlertRule(&AlertRule{ID: id, Name: id, QuerySQL: "SELECT 1 AS value", ConditionJSON: `{}`, GroupByJSON: `[]`}); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
		if err := d.UpsertAlertInstance(&AlertInstance{RuleID: id, GroupKey: "all", State: "resolved"}); err != nil {
			t.Fatalf("create instance for %s: %v", id, err)
		}
	}

	ctx, parent := otel.Tracer("test").Start(context.Background(), "parent")
	rules, err := d.WithContext(ctx).ListAlertRules()
	parent.End()
	if err != nil {
		t.Fatalf("ListAlertRules: %v", err)
	}
	if len(rules) != 3 {
		t.Fatalf("rules = %d, want 3", len(rules))
	}
	for _, rule := range rules {
		if len(rule.Instances) != 1 {
			t.Errorf("rule %s instances = %d, want 1", rule.ID, len(rule.Instances))
		}
	}

	counts := map[string]int{}
	for _, span := range recorder.Ended() {
		if span.Parent().SpanID() == parent.SpanContext().SpanID() {
			counts[span.Name()]++
		}
	}
	for _, name := range []string{"storage.duckdb.ListAlertRules", "storage.duckdb.ListAlertRuleInstances"} {
		if counts[name] != 1 {
			t.Errorf("%s spans = %d, want 1", name, counts[name])
		}
	}
	if len(counts) != 2 {
		t.Errorf("query spans = %v, want only rules and batched instances", counts)
	}
}
