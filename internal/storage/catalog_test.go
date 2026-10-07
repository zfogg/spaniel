package storage

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

func TestQueryCatalogSearchAndRecipes(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	for _, query := range []string{
		`INSERT INTO spans (trace_id, span_id, name, service_name, start_ns, end_ns, duration_ns, session_id, attributes, resource) VALUES ('trace-a','root','request','shop',1000000000,3000000000,2000000000,'a','{}','{}'), ('trace-a','child','database','shop',1100000000,1200000000,100000000,'a','{"customer":"needle_%''"}','{}')`,
		`INSERT INTO spans (trace_id, span_id, name, service_name, start_ns, end_ns, duration_ns, session_id, attributes, resource) SELECT 'new-' || i, 'new-' || i, 'unrelated', 'shop', 4000000000+i, 5000000000+i, 1000000000, 'a', '{}', '{}' FROM range(100) t(i)`,
		`INSERT INTO logs (body, service_name, timestamp_ns, session_id, attributes) VALUES ('needle_%'' message','shop',1,'a','{}'), ('needle_%'' outside','shop',2,'b','{}')`,
		`INSERT INTO metrics (name, service_name, type, unit, timestamp_ns, value, attributes, session_id) VALUES ('latency','shop','histogram','ms',1,5,'{"percentile":"p95","customer":"needle_%''"}','a'), ('count','shop','counter','1',1,20,'{}','a'), ('load','shop','gauge','1',1,2,'{}','a')`,
	} {
		if err := d.gorm.Exec(query).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := d.gorm.Exec("UPDATE spans SET kind = 2").Error; err != nil {
		t.Fatal(err)
	}
	entries, err := d.QueryCatalog(context.Background(), "", "needle_%'", "a")
	if err != nil {
		t.Fatal(err)
	}
	types := map[string]bool{}
	for _, entry := range entries {
		if !strings.Contains(entry.Query, "$session_id") || strings.Contains(entry.Query, "session_id = 'a'") {
			t.Fatalf("catalog froze session: %s", entry.Query)
		}
		types[entry.DisplayType] = true
		if err := ValidateReadOnlySQL(entry.Query); err != nil {
			t.Fatalf("%s: %v", entry.Name, err)
		}
		rows, err := d.SQL().Query(entry.Query, sql.Named("session_id", "a"))
		if err != nil {
			t.Fatalf("%s: %v\n%s", entry.Name, err, entry.Query)
		}
		if !rows.Next() {
			t.Errorf("%s has no matching rows", entry.Name)
		}
		rows.Close()
		if entry.DisplayType == "trace_list" {
			var result struct {
				SpanCount  int
				DurationNs int64
			}
			if err := d.gorm.Raw(entry.Query, sql.Named("session_id", "a")).Scan(&result).Error; err != nil {
				t.Fatal(err)
			}
			if result.SpanCount != 2 || result.DurationNs != 2000000000 {
				t.Fatalf("trace not expanded: %+v", result)
			}
		}
		if entry.DisplayType == "log_list" {
			var result []Log
			if err := d.gorm.Raw(entry.Query, sql.Named("session_id", "a")).Scan(&result).Error; err != nil {
				t.Fatal(err)
			}
			if len(result) != 1 || strings.Contains(result[0].Body, "outside") {
				t.Fatalf("wrong log scope: %+v", result)
			}
		}
	}
	for _, kind := range []string{"single_value", "time_series", "table", "heatmap", "entity_list", "span_list", "trace_list", "log_list"} {
		if !types[kind] {
			t.Errorf("missing %s", kind)
		}
	}
	empty, err := d.QueryCatalog(context.Background(), "", "not-found-unique", "a")
	if err != nil || len(empty) != 0 {
		t.Fatalf("no-match: %v %v", empty, err)
	}
	examples, err := d.QueryCatalog(context.Background(), "", "", "empty-session")
	if err != nil || len(examples) != 21 {
		t.Fatalf("empty workspace examples: %d %v", len(examples), err)
	}
	for _, entry := range examples {
		rows, err := d.SQL().Query(entry.Query, sql.Named("session_id", "empty-session"))
		if err != nil {
			t.Fatalf("example %s: %v", entry.Name, err)
		}
		rows.Close()
	}
	starter, err := d.QueryCatalog(context.Background(), "", "", "a")
	if err != nil {
		t.Fatal(err)
	}
	firstTypes := map[string]bool{}
	for _, entry := range starter[:8] {
		firstTypes[entry.DisplayType] = true
	}
	if len(firstTypes) != 8 {
		t.Fatalf("first eight should cover every renderer: %v", firstTypes)
	}
	for _, entry := range starter {
		if entry.Signal == "metrics" {
			t.Error("metric variants should not crowd the initial examples")
		}
		if err := ValidateReadOnlySQL(entry.Query); err != nil {
			t.Fatalf("unsafe example %s: %v", entry.Name, err)
		}
	}
	filtered, err := d.QueryCatalog(context.Background(), "", "Slowest spans", "empty-session")
	if err != nil || len(filtered) != 1 || filtered[0].DisplayType != "table" {
		t.Fatalf("example search: %+v %v", filtered, err)
	}
	metrics, err := d.QueryCatalog(context.Background(), "metrics", "", "a")
	if err != nil || len(metrics) != 6 {
		t.Fatalf("metric recipes: %+v %v", metrics, err)
	}
	for _, entry := range metrics {
		if strings.Contains(entry.Name, "histogram") && entry.Attributes["percentile"] != "p95" {
			t.Error("metric attributes must reach the catalog UI")
		}
		if entry.DisplayType == "time_series" {
			if strings.Contains(entry.Name, "gauge") && !strings.Contains(entry.Query, "avg(value)") {
				t.Error("gauge should use average")
			}
			if strings.Contains(entry.Name, "counter") && !strings.Contains(entry.Name, "not a rate") {
				t.Error("counter temporality must not be invented")
			}
			if strings.Contains(entry.Name, "histogram") && !strings.Contains(entry.Name, "p95") {
				t.Error("histogram must identify its percentile")
			}
		}
		rows, err := d.SQL().Query(entry.Query, sql.Named("session_id", "a"))
		if err != nil {
			t.Fatalf("metric %s: %v", entry.Name, err)
		}
		rows.Close()
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := d.QueryCatalog(ctx, "", "needle", "a"); err == nil {
		t.Error("cancellation must not look like an empty search")
	}
}
