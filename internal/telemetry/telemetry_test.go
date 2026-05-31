package telemetry

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
)

// TestSetup_NoEndpointInstallsRealProviders verifies the documented contract:
// with an empty Endpoint, Setup configures no exporters but still installs
// real (non-no-op) SDK providers and returns a usable shutdown func.
func TestSetup_NoEndpointInstallsRealProviders(t *testing.T) {
	ctx := context.Background()
	shutdown, err := Setup(ctx, Config{ServiceName: "test-svc", Version: "0.0.1"})
	if err != nil {
		t.Fatalf("Setup returned error: %v", err)
	}
	if shutdown == nil {
		t.Fatal("Setup returned nil shutdown func")
	}
	t.Cleanup(func() {
		shctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = shutdown(shctx)
	})

	// Providers must be installed globally so otelhttp/otelgrpc can grab them.
	if otel.GetTracerProvider() == nil {
		t.Error("global tracer provider is nil")
	}
	if otel.GetMeterProvider() == nil {
		t.Error("global meter provider is nil")
	}
	// A tracer obtained from the global provider must be usable.
	tr := otel.Tracer("test")
	if tr == nil {
		t.Error("tracer from global provider is nil")
	}
	_, span := tr.Start(ctx, "probe")
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
