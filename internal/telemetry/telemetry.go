package telemetry

import (
	"context"
	"log/slog"
	"time"

	"github.com/zfogg/gotel/go"
	"github.com/zfogg/gotel/go/otelhost"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/exemplar"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

var application = gotel.NewApplication("spaniel")

func Application() gotel.Application       { return application }
func Tracer(component string) trace.Tracer { return application.Tracer(component) }

// SelfTelemetryHeader identifies Spaniel's own exports so its receiver can
// suppress recursive exporter telemetry.
const (
	SelfTelemetryHeader = "x-spaniel-self-telemetry"
	selfTelemetryValue  = "true"
)

// Config controls Spaniel's own OTLP self-telemetry.
type Config struct {
	Endpoint    string // OTLP gRPC target, such as localhost:4317.
	ServiceName string
	Version     string
	DBPath      string
	Insecure    bool
}

// Setup installs Spaniel's global OTel providers and shared self-monitoring
// headers. gotel supplies SDK/host/OS/process resources, extended by the standard
// OTEL_RESOURCE_ATTRIBUTES and OTEL_SERVICE_NAME environment overrides.
// An empty endpoint intentionally leaves the global delegate untouched
// so a later hot-enabled provider can back-fill already-created instruments.
func Setup(ctx context.Context, cfg Config) (func(context.Context) error, error) {
	if cfg.Endpoint == "" {
		return func(context.Context) error { return nil }, nil
	}
	if cfg.ServiceName == "" || cfg.ServiceName == "spaniel" {
		cfg.ServiceName = APIService
	}
	bodySizeBounds := []float64{0, 16, 32, 64, 128, 256, 512, 1 << 10, 2 << 10, 4 << 10, 8 << 10, 16 << 10, 32 << 10, 64 << 10, 128 << 10, 256 << 10, 512 << 10, 1 << 20, 2 << 20, 4 << 20}
	attrs := []attribute.KeyValue{}
	if cfg.DBPath != "" {
		attrs = append(attrs, attribute.String("db.namespace", cfg.DBPath))
	}
	traceOpts := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(cfg.Endpoint), otlptracegrpc.WithHeaders(map[string]string{SelfTelemetryHeader: selfTelemetryValue})}
	metricOpts := []otlpmetricgrpc.Option{otlpmetricgrpc.WithEndpoint(cfg.Endpoint), otlpmetricgrpc.WithHeaders(map[string]string{SelfTelemetryHeader: selfTelemetryValue})}
	if cfg.Insecure {
		traceOpts = append(traceOpts, otlptracegrpc.WithInsecure())
		metricOpts = append(metricOpts, otlpmetricgrpc.WithInsecure())
	}
	traceExporter, err := otlptracegrpc.New(ctx, traceOpts...)
	if err != nil {
		return nil, err
	}
	metricExporter, err := otlpmetricgrpc.New(ctx, metricOpts...)
	if err != nil {
		_ = traceExporter.Shutdown(ctx)
		return nil, err
	}
	runtime, err := otelhost.Start(ctx, otelhost.Config{
		ApplicationName: "spaniel", DisableSpanNamePrefix: true,
		ServiceName: cfg.ServiceName, ServiceVersion: cfg.Version,
		TraceExporter: serviceSpanExporter{traceExporter},
		TraceOptions:  []sdktrace.TracerProviderOption{sdktrace.WithSpanProcessor(serviceOwnerProcessor{})},
		MetricReader:  sdkmetric.NewPeriodicReader(serviceMetricExporter{metricExporter}, sdkmetric.WithInterval(10*time.Second)),
		Endpoint:      cfg.Endpoint, Protocol: otelhost.ProtocolGRPC, Insecure: cfg.Insecure,
		Headers:    map[string]string{SelfTelemetryHeader: selfTelemetryValue},
		Attributes: attrs, MetricInterval: 10 * time.Second,
		ResourceOptions: []resource.Option{resource.WithFromEnv()},
		MetricOptions: []sdkmetric.Option{
			sdkmetric.WithExemplarFilter(exemplar.AlwaysOnFilter),
			sdkmetric.WithView(sdkmetric.NewView(sdkmetric.Instrument{Name: "http.server.request.body.size"}, sdkmetric.Stream{Aggregation: sdkmetric.AggregationExplicitBucketHistogram{Boundaries: bodySizeBounds}})),
			sdkmetric.WithView(sdkmetric.NewView(sdkmetric.Instrument{Name: "http.server.response.body.size"}, sdkmetric.Stream{Aggregation: sdkmetric.AggregationExplicitBucketHistogram{Boundaries: bodySizeBounds}})),
		},
		Slog:           &otelhost.SlogConfig{Scope: "spaniel", Attributes: []slog.Attr{slog.String("spaniel.log.source", "spaniel")}},
		RuntimeMetrics: true,
	})
	if err != nil {
		_ = traceExporter.Shutdown(ctx)
		_ = metricExporter.Shutdown(ctx)
		return nil, err
	}
	InitMetrics()
	return runtime.Shutdown, nil
}
