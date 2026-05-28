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
var goruntimeOnce sync.Once

// Config controls Spaniel's own OTLP self-telemetry.
type Config struct {
	// Endpoint is the OTLP gRPC target (e.g. "localhost:4317").
	// Empty string disables exporting but still installs real SDK providers.
	Endpoint    string
	ServiceName string
	Version     string
	DBPath      string
	Insecure    bool
}

// Setup installs the global OTel providers. Always installs real SDK providers
// (not no-op) so that third-party instrumentation (otelhttp, otelgrpc) can
// cache them and work on hot-swap. If Endpoint is empty, no exporters are
// configured but the providers still exist and work.
func Setup(ctx context.Context, cfg Config) (shutdown func(context.Context) error, err error) {
	if cfg.ServiceName == "" {
		cfg.ServiceName = "spaniel"
	}

	dialOpts := []grpc.DialOption{}
	if cfg.Insecure {
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	// Resource: always build, partial errors are non-fatal.
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

	// Traces: always real SDK. Add exporter only if endpoint is configured.
	traceOpts := []sdktrace.TracerProviderOption{sdktrace.WithResource(res)}
	if cfg.Endpoint != "" {
		exp, err := otlptracegrpc.New(ctx,
			otlptracegrpc.WithEndpoint(cfg.Endpoint),
			otlptracegrpc.WithDialOption(dialOpts...),
		)
		if err != nil {
			return nil, err
		}
		traceOpts = append(traceOpts, sdktrace.WithBatcher(exp))
	}
	tp := sdktrace.NewTracerProvider(traceOpts...)
	otel.SetTracerProvider(tp)
	shutdownFuncs = append(shutdownFuncs, tp.Shutdown)

	// Metrics: always real SDK. Add exporter/reader only if endpoint configured.
	mpOpts := []sdkmetric.Option{
		sdkmetric.WithResource(res),
		sdkmetric.WithExemplarFilter(exemplar.AlwaysOnFilter),
	}
	if cfg.Endpoint != "" {
		exp, err := otlpmetricgrpc.New(ctx,
			otlpmetricgrpc.WithEndpoint(cfg.Endpoint),
			otlpmetricgrpc.WithDialOption(dialOpts...),
		)
		if err != nil {
			_ = tp.Shutdown(ctx)
			return nil, err
		}
		mpOpts = append(mpOpts, sdkmetric.WithReader(
			sdkmetric.NewPeriodicReader(exp,
				sdkmetric.WithInterval(10*time.Second))))
	}
	mp := sdkmetric.NewMeterProvider(mpOpts...)
	otel.SetMeterProvider(mp)
	shutdownFuncs = append(shutdownFuncs, mp.Shutdown)

	// Go runtime metrics: start once, uses delegating global meter.
	goruntimeOnce.Do(func() {
		if err := goruntime.Start(); err != nil {
			slog.Warn("runtime metrics unavailable", "err", err)
		}
	})

	// Logs: always real SDK. Add exporter only if endpoint is configured.
	logOpts := []sdklog.LoggerProviderOption{sdklog.WithResource(res)}
	if cfg.Endpoint != "" {
		exp, err := otlploggrpc.New(ctx,
			otlploggrpc.WithEndpoint(cfg.Endpoint),
			otlploggrpc.WithDialOption(dialOpts...),
		)
		if err != nil {
			_ = tp.Shutdown(ctx)
			_ = mp.Shutdown(ctx)
			return nil, err
		}
		logOpts = append(logOpts, sdklog.WithProcessor(sdklog.NewBatchProcessor(exp)))
	}
	lp := sdklog.NewLoggerProvider(logOpts...)
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
