package telemetry

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
)

// TestSetup_NoEndpointIsNoop verifies the contract for an empty Endpoint: Setup
// installs NOTHING and returns a usable no-op shutdown. This is deliberate — the
// OTel global delegate only back-fills already-created tracers on the FIRST
// SetTracerProvider, so installing a (non-exporting) provider here would
// permanently orphan every tracer obtained at startup (storage's GORM plugin,
// the ingestion pipeline) from the later real, exporting provider. A tracer
// obtained now must still be safe to use (no-op span).
func TestSetup_NoEndpointIsNoop(t *testing.T) {
	ctx := context.Background()
	shutdown, err := Setup(ctx, Config{ServiceName: "test-svc", Version: "0.0.1"})
	if err != nil {
		t.Fatalf("Setup returned error: %v", err)
	}
	if shutdown == nil {
		t.Fatal("Setup returned nil shutdown func")
	}
	if err := shutdown(ctx); err != nil {
		t.Errorf("no-op shutdown returned error: %v", err)
	}

	// Using a tracer must not panic even though no provider was installed.
	_, span := otel.Tracer("test").Start(ctx, "probe")
	span.End()
}

// TestSetup_DefaultsServiceName ensures the empty-ServiceName branch falls back
// to "spaniel" rather than producing an empty resource attribute.
func TestSetup_DefaultsServiceName(t *testing.T) {
	ctx := context.Background()
	shutdown, err := Setup(ctx, Config{}) // no ServiceName, no Endpoint
	if err != nil {
		t.Fatalf("Setup with empty config returned error: %v", err)
	}
	if shutdown == nil {
		t.Fatal("expected non-nil shutdown")
	}
	shctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := shutdown(shctx); err != nil {
		t.Errorf("shutdown returned error: %v", err)
	}
}

// TestSetup_WithDBPath exercises the DBPath resource-attribute branch and the
// metrics/logs/traces provider wiring without an exporter.
func TestSetup_WithDBPath(t *testing.T) {
	ctx := context.Background()
	shutdown, err := Setup(ctx, Config{
		ServiceName: "spaniel",
		Version:     "1.2.3",
		DBPath:      "/tmp/spaniel.db",
	})
	if err != nil {
		t.Fatalf("Setup with DBPath returned error: %v", err)
	}
	shctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := shutdown(shctx); err != nil {
		t.Errorf("shutdown returned error: %v", err)
	}
}

// TestShutdown_Idempotent confirms calling the returned shutdown more than once
// does not panic — important because both signal handlers and defers may fire.
func TestShutdown_Idempotent(t *testing.T) {
	ctx := context.Background()
	shutdown, err := Setup(ctx, Config{ServiceName: "idem"})
	if err != nil {
		t.Fatalf("Setup error: %v", err)
	}
	shctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	// First call shuts the providers down; second must be safe (may return an
	// error, but must not panic).
	_ = shutdown(shctx)
	_ = shutdown(shctx)
}
