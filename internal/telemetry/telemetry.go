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
)

// goruntimeOnce ensures goruntime.Start is called exactly once per process.
var goruntimeOnce sync.Once

// SelfTelemetryHeader marks OTLP exported by Spaniel itself. The receiver uses
// it to avoid tracing the exporter RPCs, which would otherwise create a
// perpetual stream of one-span collector traces in Spaniel's own trace view.
const (
	SelfTelemetryHeader = "x-spaniel-self-telemetry"
	selfTelemetryValue  = "true"
)

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

	// With no endpoint there's nowhere to export. Crucially, do NOT install SDK
	// providers in that case: the OTel global delegate only back-fills already-
	// created tracers on the FIRST SetTracerProvider. Installing a non-exporting
	// provider here would permanently bind every tracer obtained before the real
	// (self_monitor) setup — storage's GORM plugin, the ingestion pipeline, etc.
	// — to that dead provider, so their spans would never be exported. Leaving the
	// global as the no-op delegate lets the later real setup be the first
	// SetTracerProvider and back-fill all of them.
	if cfg.Endpoint == "" {
		return func(context.Context) error { return nil }, nil
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
		traceExpOpts := []otlptracegrpc.Option{
			otlptracegrpc.WithEndpoint(cfg.Endpoint),
			otlptracegrpc.WithHeaders(map[string]string{SelfTelemetryHeader: selfTelemetryValue}),
		}
		if cfg.Insecure {
			traceExpOpts = append(traceExpOpts, otlptracegrpc.WithInsecure())
		}
		exp, err := otlptracegrpc.New(ctx, traceExpOpts...)
		if err != nil {
			return nil, err
		}
		traceOpts = append(traceOpts, sdktrace.WithBatcher(exp))
	}
	tp := sdktrace.NewTracerProvider(traceOpts...)
	otel.SetTracerProvider(tp)
	shutdownFuncs = append(shutdownFuncs, tp.Shutdown)

	// Metrics: always real SDK. Add exporter/reader only if endpoint configured.
	// HTTP body-size histograms need useful buckets beyond the SDK's default
	// upper boundary. API responses such as metric queries routinely exceed
	// 10 KiB; without these, every high percentile is indistinguishable from
	// the final 10 KiB bucket.
	bodySizeBounds := []float64{0, 16, 32, 64, 128, 256, 512, 1 << 10, 2 << 10, 4 << 10, 8 << 10, 16 << 10, 32 << 10, 64 << 10, 128 << 10, 256 << 10, 512 << 10, 1 << 20, 2 << 20, 4 << 20}
	mpOpts := []sdkmetric.Option{
		sdkmetric.WithResource(res),
		sdkmetric.WithExemplarFilter(exemplar.AlwaysOnFilter),
		sdkmetric.WithView(sdkmetric.NewView(
			sdkmetric.Instrument{Name: "http.server.request.body.size"},
			sdkmetric.Stream{Aggregation: sdkmetric.AggregationExplicitBucketHistogram{Boundaries: bodySizeBounds}},
		)),
		sdkmetric.WithView(sdkmetric.NewView(
			sdkmetric.Instrument{Name: "http.server.response.body.size"},
			sdkmetric.Stream{Aggregation: sdkmetric.AggregationExplicitBucketHistogram{Boundaries: bodySizeBounds}},
		)),
	}
	if cfg.Endpoint != "" {
		metExpOpts := []otlpmetricgrpc.Option{
			otlpmetricgrpc.WithEndpoint(cfg.Endpoint),
			otlpmetricgrpc.WithHeaders(map[string]string{SelfTelemetryHeader: selfTelemetryValue}),
		}
		if cfg.Insecure {
			metExpOpts = append(metExpOpts, otlpmetricgrpc.WithInsecure())
		}
		exp, err := otlpmetricgrpc.New(ctx, metExpOpts...)
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
	InitMetrics()
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
		logExpOpts := []otlploggrpc.Option{
			otlploggrpc.WithEndpoint(cfg.Endpoint),
			otlploggrpc.WithHeaders(map[string]string{SelfTelemetryHeader: selfTelemetryValue}),
		}
		if cfg.Insecure {
			logExpOpts = append(logExpOpts, otlploggrpc.WithInsecure())
		}
		exp, err := otlploggrpc.New(ctx, logExpOpts...)
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

	// Every process log receives a stable origin attribute. Individual paths
	// (access, receiver, forwarder) may override it with a more specific value.
	slog.SetDefault(slog.New(otelslog.NewHandler("spaniel").WithAttrs([]slog.Attr{
		slog.String("spaniel.log.source", "spaniel"),
	})))

	shutdown = func(ctx context.Context) error {
		var errs []error
		for _, fn := range shutdownFuncs {
			errs = append(errs, fn(ctx))
		}
		return errors.Join(errs...)
	}
	return shutdown, nil
}
