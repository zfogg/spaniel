package storage

import (
	"context"
	"path/filepath"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestStorageQueryNameValidation(t *testing.T) {
	for _, name := range []string{"A", "z", "Ab", "z0", "ListAlertRules", "Query_123", "a_", "Z09_a"} {
		if got, want := storageQueryName(name), "spaniel.storage."+name; got != want {
			t.Errorf("storageQueryName(%q) = %q, want %q", name, got, want)
		}
	}
	for _, name := range []string{"", " ", "0", "1Query", "_Query", "Query.Name", "Query-Name", "Query Name", "Query/Name", "Query\tName", "Query\nName", "Query!", "Query\x00", "éQuery", "Queryé", "Query１"} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Errorf("storageQueryName(%q) did not panic", name)
				}
			}()
			storageQueryName(name)
		})
	}
}

func TestWithQueryNamePrefixesSimpleNames(t *testing.T) {
	parent := WithQueryName(context.Background(), "Parent")
	ctx := WithQueryName(parent, "  DashboardLatency  ")
	if got := queryNameFromContext(ctx); got != "spaniel.storage.DashboardLatency" {
		t.Errorf("query name = %q, want spaniel.storage.DashboardLatency", got)
	}
	if got := queryNameFromContext(parent); got != "spaniel.storage.Parent" {
		t.Errorf("parent query name changed to %q", got)
	}
}

func TestWithQueryNamePanicsOnInvalidName(t *testing.T) {
	for _, name := range []string{"", " ", "\t\n", "1Query", "_Query", "Query.Name", "Query Name"} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Errorf("WithQueryName(%q) did not panic", name)
				}
			}()
			WithQueryName(context.Background(), name)
		})
	}
}

func TestSQLSummary(t *testing.T) {
	for _, tt := range []struct{ sql, want string }{
		{"SELECT ratio_now()", "SELECT ratio_now()"},
		{"SELECT * FROM spans WHERE trace_id = 'secret' ORDER BY start_ns DESC", "SELECT … FROM spans WHERE … ORDER BY …"},
		{"SELECT id, service_name FROM spans WHERE trace_id = 'secret' AND kind = 2", "SELECT … FROM spans WHERE …"},
		{"WITH recent AS (SELECT * FROM spans) SELECT * FROM recent", "WITH recent AS (SELECT … FROM spans) SELECT … FROM recent"},
		{"INSERT INTO sessions (id) VALUES (?)", "INSERT INTO sessions VALUES …"},
		{"UPDATE sessions SET label = ?", "UPDATE sessions SET …"},
		{"DELETE FROM spans WHERE trace_id = $1", "DELETE FROM spans WHERE …"},
		{"EXPLAIN SELECT * FROM spans", "EXPLAIN SELECT … FROM spans"},
		{"SELECT * FROM spans s LEFT JOIN LATERAL (SELECT * FROM logs WHERE trace_id=s.trace_id LIMIT 1) l ON true LIMIT ?", "SELECT … FROM spans AS s LEFT JOIN LATERAL (SELECT … FROM logs WHERE … LIMIT 1) AS l ON ? LIMIT ?"},
	} {
		if got := sqlSummary(tt.sql); got != tt.want {
			t.Errorf("sqlSummary(%q) = %q, want %q", tt.sql, got, tt.want)
		}
	}
}

func TestSanitizeSQL(t *testing.T) {
	got, ok := sanitizeSQL("SELECT * FROM spans WHERE trace_id = 'private' AND n = 42 -- comment")
	if !ok {
		t.Fatal("sanitizeSQL rejected valid SQL")
	}
	if want := "SELECT * FROM spans WHERE trace_id = ? AND n = ?"; got != want {
		t.Errorf("sanitizeSQL() = %q, want %q", got, want)
	}
	if _, ok := sanitizeSQL("SELECT 'unterminated"); ok {
		t.Fatal("sanitizeSQL accepted unterminated literal")
	}
}

func TestReadOnlyQueryNamesAndSanitizesUserSQL(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous); _ = provider.Shutdown(context.Background()) })

	d, err := Open(filepath.Join(t.TempDir(), "query.duckdb"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if _, _, _, err := d.ReadOnlyQuery(context.Background(), "SELECT 'secret' AS value", 1); err != nil {
		t.Fatalf("ReadOnlyQuery: %v", err)
	}

	for _, span := range recorder.Ended() {
		if span.Name() != "spaniel.storage.ReadOnlyQuery" {
			continue
		}
		attrs := map[string]string{}
		for _, attr := range span.Attributes() {
			attrs[string(attr.Key)] = attr.Value.AsString()
		}
		if attrs["db.query.summary"] != "SELECT …" {
			t.Errorf("summary = %q", attrs["db.query.summary"])
		}
		if attrs["db.query.text"] != "SELECT ? AS value" {
			t.Errorf("query text = %q", attrs["db.query.text"])
		}
		return
	}
	t.Fatal("no read-only SQL span recorded")
}

func TestReadOnlyQueryUsesExplicitUserName(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous); _ = provider.Shutdown(context.Background()) })

	d, err := Open(filepath.Join(t.TempDir(), "named-query.duckdb"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	ctx := WithQueryName(context.Background(), "DashboardLatency")
	if _, _, _, err := d.ReadOnlyQuery(ctx, "SELECT 'secret' AS value", 1); err != nil {
		t.Fatalf("ReadOnlyQuery: %v", err)
	}

	for _, span := range recorder.Ended() {
		if span.Name() != "spaniel.storage.DashboardLatency" {
			continue
		}
		foundSummary := false
		for _, attr := range span.Attributes() {
			if string(attr.Key) == "db.query.summary" {
				foundSummary = true
				if attr.Value.AsString() != "SELECT …" {
					t.Errorf("summary = %q, want SELECT …", attr.Value.AsString())
				}
			}
		}
		if !foundSummary {
			t.Error("explicitly named span missing db.query.summary")
		}
		return
	}
	t.Fatal("no explicitly named read-only SQL span recorded")
}

// TestReadOnlyQueryStructuralSummary verifies complex SQL actually executes in
// DuckDB and that the emitted named span preserves its query topology and bounds.
func TestReadOnlyQueryStructuralSummary(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous); _ = provider.Shutdown(context.Background()) })
	d, err := Open(filepath.Join(t.TempDir(), "structural.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	const sql = `WITH recent AS MATERIALIZED (
		SELECT trace_id FROM spans ORDER BY start_ns DESC LIMIT 1
	) SELECT r.trace_id FROM recent r LEFT JOIN LATERAL (
		SELECT trace_id FROM logs LIMIT 1
	) l ON l.trace_id=r.trace_id AND l.trace_id <> 'private-token' UNION ALL SELECT trace_id FROM spans LIMIT CAST(5 AS BIGINT)`
	ctx := WithQueryName(context.Background(), "VerifyStructuralSummary")
	if _, _, _, err := d.ReadOnlyQuery(ctx, sql, 10); err != nil {
		t.Fatal(err)
	}
	const want = "WITH recent AS MATERIALIZED (SELECT … FROM spans ORDER BY … LIMIT 1) SELECT … FROM recent AS r LEFT JOIN LATERAL (SELECT … FROM logs LIMIT 1) AS l ON l.trace_id=r.trace_id AND l.trace_id <> ? UNION ALL SELECT … FROM spans LIMIT ?"
	for _, span := range recorder.Ended() {
		if span.Name() != "spaniel.storage.VerifyStructuralSummary" {
			continue
		}
		for _, attr := range span.Attributes() {
			if string(attr.Key) == "db.query.summary" {
				if got := attr.Value.AsString(); got != want {
					t.Fatalf("summary = %q; want %q", got, want)
				}
				return
			}
		}
		t.Fatal("storage span missing summary")
	}
	t.Fatal("no complex read-only SQL span recorded")
}
