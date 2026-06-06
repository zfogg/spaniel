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
// via store.WithContext(ctx) produces a "db.*" span nested under the caller's
// span, with the SQL and db.system recorded by the GORM OTel plugin.
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
		if strings.HasPrefix(s.Name(), "db.") && s.Parent().SpanID() == parentID {
			dbSpan = s
			break
		}
	}
	if dbSpan == nil {
		var names []string
		for _, s := range sr.Ended() {
			names = append(names, s.Name())
		}
		t.Fatalf("no db.* span nested under parent; recorded spans: %v", names)
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
		if strings.HasPrefix(s.Name(), "db.") && s.Parent().SpanID() == parentID {
			t.Errorf("db span unexpectedly nested under parent without WithContext")
		}
	}
	_ = ctx
}
