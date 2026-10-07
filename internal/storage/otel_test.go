package storage

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TestWithContext_NestsDBSpans proves the core of issue #108: a DuckDB query run
// via store.WithContext(ctx) produces a source-named storage span nested under
// the caller's span, with the SQL and db.system recorded by the GORM OTel
// plugin.
func TestWithContext_NestsDBSpans(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	d, err := Open(filepath.Join(t.TempDir(), "t.duckdb"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { d.Close() })

	ctx, parent := otel.Tracer("test").Start(context.Background(), "parent")
	if _, err := d.WithContext(ctx).ListTraces(TraceFilter{Limit: 10}); err != nil {
		t.Fatalf("ListTraces: %v", err)
	}
	parent.End()

	parentID := parent.SpanContext().SpanID()
	var dbSpan sdktrace.ReadOnlySpan
	for _, s := range sr.Ended() {
		if s.Name() == "storage.ListTraces" && s.Parent().SpanID() == parentID {
			dbSpan = s
			break
		}
	}
	if dbSpan == nil {
		var names []string
		for _, s := range sr.Ended() {
			names = append(names, s.Name())
		}
		t.Fatalf("no storage.ListTraces span nested under parent; recorded spans: %v", names)
	}
	for _, s := range sr.Ended() {
		if s.Parent().SpanID() == parentID && (s.Name() == "db.query" || s.Name() == "db.row") {
			t.Errorf("generic database span name %q recorded; spans must be source-named", s.Name())
		}
	}

	// The plugin should record db.system=duckdb and the SQL text.
	var hasSystem, hasSQL bool
	for _, a := range dbSpan.Attributes() {
		switch a.Key {
		case "db.system":
			hasSystem = a.Value.AsString() == "duckdb"
		case "db.query.text":
			hasSQL = strings.Contains(strings.ToLower(a.Value.AsString()), "select")
		}
	}
	if !hasSystem {
		t.Errorf("db span missing db.system=duckdb; attrs=%v", dbSpan.Attributes())
	}
	if !hasSQL {
		t.Errorf("db span missing SQL text; attrs=%v", dbSpan.Attributes())
	}
}

// TestWithoutContext_NoParent confirms that a bare store call (no WithContext)
// doesn't attach to a caller span — the db span is a root, not nested. This is
// why request paths must use WithContext.
func TestWithoutContext_DBSpanIsRoot(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	d, err := Open(filepath.Join(t.TempDir(), "t.duckdb"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { d.Close() })

	ctx, parent := otel.Tracer("test").Start(context.Background(), "parent")
	if _, err := d.ListTraces(TraceFilter{Limit: 10}); err != nil { // NO WithContext
		t.Fatalf("ListTraces: %v", err)
	}
	parent.End()

	parentID := parent.SpanContext().SpanID()
	for _, s := range sr.Ended() {
		if s.Name() == "storage.ListTraces" && s.Parent().SpanID() == parentID {
			t.Errorf("storage span unexpectedly nested under parent without WithContext")
		}
	}
	_ = ctx
}

func TestWithContext_NamesGeneratedWriteSpan(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	d, err := Open(filepath.Join(t.TempDir(), "write.duckdb"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	ctx, parent := otel.Tracer("test").Start(context.Background(), "parent")
	if err := d.WithContext(ctx).SetSpanielVersion("test"); err != nil {
		t.Fatalf("SetSpanielVersion: %v", err)
	}
	parent.End()

	parentID := parent.SpanContext().SpanID()
	for _, s := range sr.Ended() {
		if s.Name() == "storage.SetSpanielVersion" && s.Parent().SpanID() == parentID {
			return
		}
	}
	t.Fatal("no source-named generated write span nested under parent")
}

func TestGORMPluginFallbackIsStorageNamed(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(previous); _ = tp.Shutdown(context.Background()) })

	d, err := Open(filepath.Join(t.TempDir(), "fallback.duckdb"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	// Deliberately bypass a DB method so storageCallerName has no source-owned
	// method to recover. This is the last-resort path that used to emit db.query.
	var one int
	if err := d.gorm.Raw("SELECT 1").Scan(&one).Error; err != nil {
		t.Fatalf("raw query: %v", err)
	}

	var names []string
	for _, span := range sr.Ended() {
		names = append(names, span.Name())
		if strings.HasPrefix(span.Name(), "db.") {
			t.Errorf("generic database span name %q recorded", span.Name())
		}
		if strings.HasPrefix(span.Name(), "storage.") {
			return
		}
	}
	t.Fatalf("no storage-named span recorded; spans: %v", names)
}

func TestWithContext_NamesActiveMetricSeriesSpan(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(previous); _ = tp.Shutdown(context.Background()) })

	d, err := Open(filepath.Join(t.TempDir(), "series.duckdb"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	ctx, parent := otel.Tracer("test").Start(context.Background(), "parent")
	if _, err := d.WithContext(ctx).ActiveMetricSeries(); err != nil {
		t.Fatalf("ActiveMetricSeries: %v", err)
	}
	parent.End()
	for _, span := range sr.Ended() {
		if span.Name() == "storage.ActiveMetricSeries" && span.Parent().SpanID() == parent.SpanContext().SpanID() {
			return
		}
	}
	t.Fatal("no storage.ActiveMetricSeries span nested under parent")
}

func TestWithContext_GetStatsEmitsOneStorageSpan(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(previous); _ = tp.Shutdown(context.Background()) })

	d, err := Open(filepath.Join(t.TempDir(), "stats.duckdb"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	ctx, parent := otel.Tracer("test").Start(context.Background(), "parent")
	if _, err := d.WithContext(ctx).GetStats(""); err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	parent.End()

	var count int
	for _, span := range sr.Ended() {
		if span.Name() == "storage.GetStats" && span.Parent().SpanID() == parent.SpanContext().SpanID() {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("storage.GetStats spans = %d, want 1", count)
	}
}
