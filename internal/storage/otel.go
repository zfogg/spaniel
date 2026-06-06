package storage

import (
	"context"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"
)

// skipTracingKey marks a context whose GORM operations must NOT be traced. Used
// when storing Spaniel's own self-telemetry so persisting it doesn't generate
// new "db.*" spans (which would feed back into the self-monitor loop).
type skipTracingKey struct{}

// WithoutTracing returns ctx with GORM span creation suppressed for the storage
// OTel plugin. See pipeline self-telemetry handling.
func WithoutTracing(ctx context.Context) context.Context {
	return context.WithValue(ctx, skipTracingKey{}, true)
}

func skipTracing(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, _ := ctx.Value(skipTracingKey{}).(bool)
	return v
}

// gormOTelPlugin instruments every GORM operation with an OTel span.
// It stores the span in the Statement.Context so after callbacks can
// retrieve it via trace.SpanFromContext — no extra map key needed.
type gormOTelPlugin struct {
	tracer  trace.Tracer
	latency metric.Float64Histogram
}

func newGORMPlugin() *gormOTelPlugin {
	meter := otel.Meter("spaniel/storage")
	hist, _ := meter.Float64Histogram("spaniel.db.query.latency",
		metric.WithDescription("DuckDB query latency"),
		metric.WithUnit("ms"),
	)
	return &gormOTelPlugin{
		tracer:  otel.Tracer("spaniel/storage"),
		latency: hist,
	}
}

func (p *gormOTelPlugin) Name() string { return "spaniel:otel" }

func (p *gormOTelPlugin) Initialize(db *gorm.DB) error {
	for _, op := range []string{"query", "create", "update", "delete", "row", "raw"} {
		gormOp := "gorm:" + op
		before := p.before(op)
		after := p.after
		switch op {
		case "query":
			_ = db.Callback().Query().Before(gormOp).Register("otel:before_"+op, before)
			_ = db.Callback().Query().After(gormOp).Register("otel:after_"+op, after)
		case "create":
			_ = db.Callback().Create().Before(gormOp).Register("otel:before_"+op, before)
			_ = db.Callback().Create().After(gormOp).Register("otel:after_"+op, after)
		case "update":
			_ = db.Callback().Update().Before(gormOp).Register("otel:before_"+op, before)
			_ = db.Callback().Update().After(gormOp).Register("otel:after_"+op, after)
		case "delete":
			_ = db.Callback().Delete().Before(gormOp).Register("otel:before_"+op, before)
			_ = db.Callback().Delete().After(gormOp).Register("otel:after_"+op, after)
		case "row":
			_ = db.Callback().Row().Before(gormOp).Register("otel:before_"+op, before)
			_ = db.Callback().Row().After(gormOp).Register("otel:after_"+op, after)
		case "raw":
			_ = db.Callback().Raw().Before(gormOp).Register("otel:before_"+op, before)
			_ = db.Callback().Raw().After(gormOp).Register("otel:after_"+op, after)
		}
	}
	return nil
}

func (p *gormOTelPlugin) before(op string) func(*gorm.DB) {
	return func(db *gorm.DB) {
		ctx := db.Statement.Context
		if ctx == nil {
			ctx = context.Background()
		}
		if skipTracing(ctx) {
			return
		}
		ctx, _ = p.tracer.Start(ctx, "db."+op,
			trace.WithSpanKind(trace.SpanKindClient),
			trace.WithAttributes(
				semconv.DBSystemKey.String("duckdb"),
				semconv.DBOperationNameKey.String(op),
			),
		)
		db.Statement.Context = ctx
		db.Set("otel:start", time.Now())
	}
}

func (p *gormOTelPlugin) after(db *gorm.DB) {
	if db.Statement != nil && skipTracing(db.Statement.Context) {
		return
	}
	span := trace.SpanFromContext(db.Statement.Context)
	defer span.End()

	if startVal, ok := db.Get("otel:start"); ok {
		if t, ok := startVal.(time.Time); ok {
			attrs := []attribute.KeyValue{semconv.DBSystemKey.String("duckdb")}
			if db.Statement != nil {
				attrs = append(attrs, semconv.DBCollectionNameKey.String(db.Statement.Table))
			}
			p.latency.Record(db.Statement.Context,
				float64(time.Since(t).Milliseconds()),
				metric.WithAttributes(attrs...),
			)
		}
	}

	if db.Statement != nil && db.Statement.SQL.Len() > 0 {
		span.SetAttributes(
			semconv.DBQueryTextKey.String(db.Statement.SQL.String()),
			semconv.DBCollectionNameKey.String(db.Statement.Table),
		)
	}

	if db.Error != nil && db.Error != gorm.ErrRecordNotFound {
		span.RecordError(db.Error)
		span.SetStatus(codes.Error, db.Error.Error())
	}
}

// registerDBSizeGauge registers an observable gauge reporting the DuckDB
// file size in bytes. Called from Open after the DB is initialised.
func registerDBSizeGauge(path string) {
	if path == "" || path == ":memory:" {
		return
	}
	meter := otel.Meter("spaniel/storage")
	_, _ = meter.Int64ObservableGauge("spaniel.db.file_size",
		metric.WithDescription("DuckDB file size on disk"),
		metric.WithUnit("By"),
		metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
			if fi, err := os.Stat(path); err == nil {
				o.Observe(fi.Size())
			}
			return nil
		}),
	)
}
