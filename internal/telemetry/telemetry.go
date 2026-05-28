package telemetry

import (
	"context"
	"errors"
	"log/slog"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	goruntime "go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/log/global"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/exemplar"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Config controls Spaniel's own OTLP self-telemetry.
type Config struct {
	// Endpoint is the OTLP gRPC target (e.g. "localhost:4317").
	// Empty string disables self-telemetry entirely.
	Endpoint    string
	ServiceName string
	Version     string
	DBPath      string // included as spaniel.db_path resource attribute
	Insecure    bool
}

// Setup initialises the global TracerProvider, MeterProvider, and LoggerProvider,
// installs Go runtime metrics, enables exemplar collection on all histograms,
// and wires slog to the OTel log bridge.
// If cfg.Endpoint is empty, no-op providers are installed so instrumented
// code continues to compile and run with zero overhead.
// The returned shutdown func must be called on process exit to flush spans.
func Setup(ctx context.Context, cfg Config) (shutdown func(context.Context) error, err error) {
	if cfg.Endpoint == "" {
		return func(context.Context) error { return nil }, nil
	}

	if cfg.ServiceName == "" {
		cfg.ServiceName = "spaniel"
	}

	dialOpts := []grpc.DialOption{}
	if cfg.Insecure {
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	// Build a rich resource: SDK info, host, OS, process, and Spaniel-specific attrs.
	attrs := []attribute.KeyValue{
		semconv.ServiceName(cfg.ServiceName),
		semconv.ServiceVersion(cfg.Version),
	}
	if cfg.DBPath != "" {
		attrs = append(attrs, semconv.DBNamespaceKey.String(cfg.DBPath))
	}
	res, err := resource.New(ctx,
		resource.WithTelemetrySDK(),
		resource.WithHost(),
		resource.WithOS(),
		resource.WithProcess(),
		resource.WithAttributes(attrs...),
	)
	if err != nil {
		return nil, err
	}

	var shutdownFuncs []func(context.Context) error

	// Traces
	traceExporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(cfg.Endpoint),
		otlptracegrpc.WithDialOption(dialOpts...),
	)
	if err != nil {
		return nil, err
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	shutdownFuncs = append(shutdownFuncs, tp.Shutdown)

	// Metrics — AlwaysOnFilter attaches trace exemplars to every histogram
	// bucket, linking latency percentiles to the specific traces that caused them.
	metricExporter, err := otlpmetricgrpc.New(ctx,
		otlpmetricgrpc.WithEndpoint(cfg.Endpoint),
		otlpmetricgrpc.WithDialOption(dialOpts...),
	)
	if err != nil {
		_ = tp.Shutdown(ctx)
		return nil, err
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter)),
		sdkmetric.WithResource(res),
		sdkmetric.WithExemplarFilter(exemplar.AlwaysOnFilter),
	)
	otel.SetMeterProvider(mp)
	shutdownFuncs = append(shutdownFuncs, mp.Shutdown)

	// Go runtime metrics: goroutines, heap alloc, GC pause, thread count.
	if err := goruntime.Start(); err != nil {
		_ = tp.Shutdown(ctx)
		_ = mp.Shutdown(ctx)
		return nil, err
	}

	// Logs
	logExporter, err := otlploggrpc.New(ctx,
		otlploggrpc.WithEndpoint(cfg.Endpoint),
		otlploggrpc.WithDialOption(dialOpts...),
	)
	if err != nil {
		_ = tp.Shutdown(ctx)
		_ = mp.Shutdown(ctx)
		return nil, err
	}
	lp := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExporter)),
		sdklog.WithResource(res),
	)
	global.SetLoggerProvider(lp)
	shutdownFuncs = append(shutdownFuncs, lp.Shutdown)

	// Wire slog to the OTel log bridge so all slog output is exported as
	// OTel log records and shows up in Spaniel's own Logs tab when dogfooding.
	slog.SetDefault(slog.New(otelslog.NewHandler("spaniel")))

	shutdown = func(ctx context.Context) error {
		var errs []error
		for _, fn := range shutdownFuncs {
			errs = append(errs, fn(ctx))
		}
		return errors.Join(errs...)
	}
	return shutdown, nil
}
