package storage

import (
	"context"
	"path/filepath"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestSQLSummary(t *testing.T) {
	for _, tt := range []struct{ sql, want string }{
		{"SELECT * FROM spans WHERE trace_id = 'secret'", "SELECT spans"},
		{"WITH recent AS (SELECT * FROM spans) SELECT * FROM recent", "SELECT recent"},
		{"INSERT INTO sessions (id) VALUES (?)", "INSERT sessions"},
		{"UPDATE sessions SET label = ?", "UPDATE sessions"},
		{"DELETE FROM spans WHERE trace_id = $1", "DELETE spans"},
		{"EXPLAIN SELECT * FROM spans", "EXPLAIN SELECT spans"},
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
		if span.Name() != "SELECT" {
			continue
		}
		attrs := map[string]string{}
		for _, attr := range span.Attributes() {
			attrs[string(attr.Key)] = attr.Value.AsString()
		}
		if attrs["db.query.summary"] != "SELECT" {
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
		if span.Name() != "DashboardLatency" {
			continue
		}
		foundSummary := false
		for _, attr := range span.Attributes() {
			if string(attr.Key) == "db.query.summary" {
				foundSummary = true
				if attr.Value.AsString() != "DashboardLatency" {
					t.Errorf("summary = %q, want DashboardLatency", attr.Value.AsString())
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
