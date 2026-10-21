package telemetry

import (
	"context"
	"log/slog"
	"net"
	"os"
	"runtime"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/log/global"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
)

type resourceReceiver struct {
	collector.UnimplementedTraceServiceServer
	requests chan *collector.ExportTraceServiceRequest
}

func (r *resourceReceiver) Export(ctx context.Context, req *collector.ExportTraceServiceRequest) (*collector.ExportTraceServiceResponse, error) {
	r.requests <- req
	return &collector.ExportTraceServiceResponse{}, nil
}

// Verify the application setup and actual OTLP payload, rather than only the
// SDK defaults. This must continue to work after shared-library upgrades.
func TestSetupExportsResource(t *testing.T) {
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "test.resource=preserved")
	t.Setenv("OTEL_SERVICE_NAME", "")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	previous := otel.GetTracerProvider()
	previousMeter, previousLogs, previousLogger := otel.GetMeterProvider(), global.GetLoggerProvider(), slog.Default()
	defer otel.SetTracerProvider(previous)
	defer otel.SetMeterProvider(previousMeter)
	defer global.SetLoggerProvider(previousLogs)
	defer slog.SetDefault(previousLogger)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	receiver := &resourceReceiver{requests: make(chan *collector.ExportTraceServiceRequest, 8)}
	server := grpc.NewServer()
	collector.RegisterTraceServiceServer(server, receiver)
	go server.Serve(listener)
	defer server.Stop()
	shutdown, err := Setup(ctx, Config{Endpoint: listener.Addr().String(), ServiceName: "spaniel-resource-test", Version: "resource-test", DBPath: "test.duckdb", Insecure: true})
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown(ctx)
	_, span := otel.Tracer("resource-test").Start(ctx, "resource-probe")
	span.End()
	if err := otel.GetTracerProvider().(*sdktrace.TracerProvider).ForceFlush(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case req := <-receiver.requests:
		if len(req.ResourceSpans) == 0 {
			t.Fatal("no resource spans")
		}
		attrs := map[string]string{}
		pid := int64(0)
		for _, a := range req.ResourceSpans[0].Resource.Attributes {
			attrs[a.Key] = a.Value.GetStringValue()
			if a.Key == "process.pid" {
				pid = a.Value.GetIntValue()
			}
		}
		for _, key := range []string{"host.name", "os.type", "os.description", "process.executable.name", "process.executable.path", "process.runtime.description", "telemetry.sdk.version"} {
			if attrs[key] == "" {
				t.Errorf("missing %s", key)
			}
		}
		for key, want := range map[string]string{"service.name": "spaniel-resource-test", "service.version": "resource-test", "db.namespace": "test.duckdb", "process.runtime.version": runtime.Version(), "telemetry.sdk.name": "opentelemetry", "telemetry.sdk.language": "go", "test.resource": "preserved"} {
			if attrs[key] != want {
				t.Errorf("%s = %q, want %q", key, attrs[key], want)
			}
		}
		if pid != int64(os.Getpid()) {
			t.Errorf("process.pid = %d", pid)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
