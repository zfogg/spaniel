package receiver

import (
	"context"
	"testing"

	"google.golang.org/grpc/metadata"

	"github.com/zfogg/spaniel/internal/telemetry"
)

func TestSelfTelemetryContext(t *testing.T) {
	self := metadata.NewIncomingContext(context.Background(), metadata.Pairs(telemetry.SelfTelemetryHeader, "true"))
	if !isSelfTelemetryContext(self) {
		t.Fatal("Spaniel self-telemetry RPC was not identified")
	}
	if isSelfTelemetryContext(context.Background()) {
		t.Fatal("external OTLP RPC was misidentified as Spaniel self-telemetry")
	}
}
