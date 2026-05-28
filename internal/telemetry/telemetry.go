package telemetry

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

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

// goruntimeOnce ensures goruntime.Start is called exactly once per process.
// It registers observable callbacks on the global MeterProvider; after a
// hot-swap those callbacks automatically target the new provider via
// delegation, so re-registering them is both unnecessary and causes failures.
var goruntimeOnce sync.Once

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
// installs Go runtime metrics (once), enables exemplar collection, and wires
// slog to the OTel log bridge. Safe to call multiple times — on hot-swap the
// previous providers are replaced and the caller must call the old shutdown.
// If cfg.Endpoint is empty a no-op shutdown is returned and no providers are
// changed.
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

	// Build resource. Some detectors (process owner, OS description) can fail
	// on certain systems; resource.New returns ErrPartialResource in that case
	// but still gives us a valid partial resource. Treat partial failures as
	// non-fatal — we proceed with whatever attributes were detected.
	attrs := []attribute.KeyValue{
		semconv.ServiceName(cfg.ServiceName),
		semconv.ServiceVersion(cfg.Version),
	}
	if cfg.DBPath != "" {
		attrs = append(attrs, semconv.DBNamespaceKey.String(cfg.DBPath))
	}
	res, resErr := resource.New(ctx,
		resource.WithTelemetrySDK(),
		resource.WithHost(),
		resource.WithOS(),
		resource.WithProcess(),
		resource.WithAttributes(attrs...),
	)
	if resErr != nil && res == nil {
		return nil, resErr
	}
	if resErr != nil {
		slog.Debug("resource detection partially failed", "err", resErr)
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
	// Export every 10 s so data appears quickly in the UI.
	metricExporter, err := otlpmetricgrpc.New(ctx,
		otlpmetricgrpc.WithEndpoint(cfg.Endpoint),
		otlpmetricgrpc.WithDialOption(dialOpts...),
	)
	if err != nil {
		_ = tp.Shutdown(ctx)
		return nil, err
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter,
			sdkmetric.WithInterval(10*time.Second))),
		sdkmetric.WithResource(res),
		sdkmetric.WithExemplarFilter(exemplar.AlwaysOnFilter),
	)
	otel.SetMeterProvider(mp)
	shutdownFuncs = append(shutdownFuncs, mp.Shutdown)

	// Go runtime metrics are started once; the goroutine/callback uses the
	// global MeterProvider which delegates automatically after each hot-swap.
	goruntimeOnce.Do(func() {
		if err := goruntime.Start(); err != nil {
			slog.Warn("runtime metrics unavailable", "err", err)
		}
	})

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
