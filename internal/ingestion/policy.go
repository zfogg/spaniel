package ingestion

import (
	"github.com/spf13/viper"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

type ingestPolicy struct {
	services, routes               []string
	maxBytes, requests, queueBytes int
}

func (p *Pipeline) Configure(v *viper.Viper) {
	policy := &ingestPolicy{v.GetStringSlice("ingestion_excluded_services"), v.GetStringSlice("ingestion_excluded_routes"), v.GetInt("ingestion_max_request_mb") << 20, v.GetInt("ingestion_queue_requests"), v.GetInt("ingestion_queue_mb") << 20}
	if policy.maxBytes <= 0 {
		policy.maxBytes = 64 << 20
	}
	if policy.requests <= 0 {
		policy.requests = 256
	}
	if policy.queueBytes <= 0 {
		policy.queueBytes = 64 << 20
	}
	p.policy.Store(policy)
	if p.limiter != nil {
		p.limiter.Configure(v.GetFloat64("source_rps"), v.GetInt("source_burst"))
	}
	p.batches.mu.Lock()
	p.batches.limitRequests = policy.requests
	p.batches.limitBytes = policy.queueBytes
	p.batches.mu.Unlock()
}
func (p *Pipeline) MaxRequestBytes() int {
	if v := p.policy.Load(); v != nil {
		return v.maxBytes
	}
	return 64 << 20
}
func contains(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}
func (p *Pipeline) excluded(a pcommon.Map) bool {
	v := p.policy.Load()
	if v == nil {
		return false
	}
	s, _ := a.Get("service.name")
	return contains(v.services, s.Str())
}
func (p *Pipeline) filterTraces(td ptrace.Traces) {
	before := td.SpanCount()
	td.ResourceSpans().RemoveIf(func(r ptrace.ResourceSpans) bool {
		if p.excluded(r.Resource().Attributes()) {
			return true
		}
		r.ScopeSpans().RemoveIf(func(s ptrace.ScopeSpans) bool {
			s.Spans().RemoveIf(func(span ptrace.Span) bool {
				v := p.policy.Load()
				if v == nil {
					return false
				}
				route, _ := span.Attributes().Get("http.route")
				return contains(v.routes, route.Str())
			})
			return s.Spans().Len() == 0
		})
		return r.ScopeSpans().Len() == 0
	})
	p.filtered.Add(int64(before - td.SpanCount()))
}
func (p *Pipeline) filterLogs(ld plog.Logs) {
	before := ld.LogRecordCount()
	ld.ResourceLogs().RemoveIf(func(r plog.ResourceLogs) bool { return p.excluded(r.Resource().Attributes()) })
	p.filtered.Add(int64(before - ld.LogRecordCount()))
}
func (p *Pipeline) filterMetrics(md pmetric.Metrics) {
	before := md.DataPointCount()
	md.ResourceMetrics().RemoveIf(func(r pmetric.ResourceMetrics) bool { return p.excluded(r.Resource().Attributes()) })
	p.filtered.Add(int64(before - md.DataPointCount()))
}
func (p *Pipeline) IngestionStatus() map[string]int64 {
	b := p.batches
	b.mu.Lock()
	defer b.mu.Unlock()
	return map[string]int64{"queued_requests": int64(b.requests), "queued_bytes": int64(b.bytes), "rejected_requests": p.rejected.Load(), "filtered_records": p.filtered.Load()}
}

func (p *Pipeline) RecordRejectedRequest() { p.rejected.Add(1) }
