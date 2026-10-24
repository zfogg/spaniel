package ingestion

import (
	"context"
	"errors"
	"github.com/spf13/viper"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"strings"
	"testing"
)

func TestPolicyFiltersAndQueueLimits(t *testing.T) {
	p := &Pipeline{batches: newIngestBatcher(nil, nil)}
	v := viper.New()
	v.Set("ingestion_excluded_services", []string{"noise"})
	v.Set("ingestion_excluded_routes", []string{"/health"})
	v.Set("ingestion_queue_requests", 1)
	p.Configure(v)
	td := ptrace.NewTraces()
	r := td.ResourceSpans().AppendEmpty()
	r.Resource().Attributes().PutStr("service.name", "noise")
	r.ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	r = td.ResourceSpans().AppendEmpty()
	r.Resource().Attributes().PutStr("service.name", "keep")
	spans := r.ScopeSpans().AppendEmpty().Spans()
	spans.AppendEmpty().Attributes().PutStr("http.route", "/health")
	spans.AppendEmpty().SetName("keep")
	p.filterTraces(td)
	if td.SpanCount() != 1 {
		t.Fatal("trace filter failed")
	}
	ld := plog.NewLogs()
	rl := ld.ResourceLogs().AppendEmpty()
	rl.Resource().Attributes().PutStr("service.name", "noise")
	rl.ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
	p.filterLogs(ld)
	if ld.LogRecordCount() != 0 {
		t.Fatal("log filter failed")
	}
	md := pmetric.NewMetrics()
	rm := md.ResourceMetrics().AppendEmpty()
	rm.Resource().Attributes().PutStr("service.name", "noise")
	rm.ScopeMetrics().AppendEmpty().Metrics().AppendEmpty().SetEmptyGauge().DataPoints().AppendEmpty()
	p.filterMetrics(md)
	if md.DataPointCount() != 0 {
		t.Fatal("metric filter failed")
	}
	b := p.batches
	b.running = true
	b.idle = make(chan struct{})
	if _, e := b.enqueue(context.Background(), 1, 1, func(context.Context) error { return nil }); e != nil {
		t.Fatal(e)
	}
	if _, e := b.enqueue(context.Background(), 1, 1, nil); !errors.Is(e, ErrIngestBusy) {
		t.Fatal("queue limit not enforced")
	}
	if p.IngestionStatus()["filtered_records"] != 4 {
		t.Fatal("filter count mismatch")
	}
}

func TestPolicyFiltersDoNotBypassRequestLimit(t *testing.T) {
	p := &Pipeline{batches: newIngestBatcher(nil, nil)}
	v := viper.New()
	v.Set("ingestion_excluded_services", []string{"noise"})
	v.Set("ingestion_max_request_mb", 1)
	p.Configure(v)
	td := ptrace.NewTraces()
	r := td.ResourceSpans().AppendEmpty()
	r.Resource().Attributes().PutStr("service.name", "noise")
	r.ScopeSpans().AppendEmpty().Spans().AppendEmpty().SetName(strings.Repeat("x", 1<<20))
	if e := p.IngestTraces(context.Background(), td); !errors.Is(e, ErrIngestTooLarge) {
		t.Fatal("filters bypassed incoming request limit")
	}
}
