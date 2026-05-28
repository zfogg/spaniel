package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/zfogg/spaniel/internal/storage"
)

// listMetrics returns one entry per (service, name) metric stream.
//   GET /api/metrics?sessionId=...
func (r *Router) listMetrics(w http.ResponseWriter, req *http.Request) {
	sessionID := req.URL.Query().Get("sessionId")
	entries, err := r.store.ListMetricCatalog(sessionID)
	if err != nil {
		respondErr(w, req, 500, err.Error())
		return
	}
	if entries == nil {
		entries = []*storage.MetricCatalogEntry{}
	}
	respond(w, entries, len(entries), 1)
}

// MetricSeriesPoint represents one metric data point with optional exemplars.
// Histogram percentiles arrive bucketed into p50/p95/p99 slices on the client;
// gauge/counter populate only Value. Exemplars link to traces via trace_id/span_id.
type MetricSeriesPoint struct {
	TimestampNs int64                `json:"timestamp_ns"`
	Value       float64              `json:"value"`
	Percentile  string               `json:"percentile,omitempty"`
	Exemplars   []MetricSeriesExemplar `json:"exemplars,omitempty"`
}

type MetricSeriesExemplar struct {
	TraceID string `json:"trace_id"`
	SpanID  string `json:"span_id"`
}

type MetricSeriesResponse struct {
	Name        string                  `json:"name"`
	ServiceName string                  `json:"service_name"`
	Type        string                  `json:"type"`
	Unit        string                  `json:"unit"`
	Description string                  `json:"description"`
	Points      []MetricSeriesPoint     `json:"points"`
	// Traces is populated only when ?with_traces=1 is passed. Always
	// materialized as [] (never null) so the frontend type can be
	// non-optional and rendering branches stay flat.
	Traces []*storage.TraceOverlay `json:"traces"`
}

// getMetricSeries returns the time series for one metric.
//   GET /api/metrics/series?name=&service=&sessionId=&from=&to=
func (r *Router) getMetricSeries(w http.ResponseWriter, req *http.Request) {
	q := req.URL.Query()
	name := q.Get("name")
	if name == "" {
		respondErr(w, req, 400, "name query param required")
		return
	}

	from, _ := strconv.ParseInt(q.Get("from"), 10, 64)
	to, _ := strconv.ParseInt(q.Get("to"), 10, 64)

	rows, err := r.store.GetMetricSeries(storage.MetricSeriesFilter{
		Name:      name,
		Service:   q.Get("service"),
		SessionID: q.Get("sessionId"),
		FromNs:    from,
		ToNs:      to,
	})
	if err != nil {
		respondErr(w, req, 500, err.Error())
		return
	}

	out := MetricSeriesResponse{
		Name:   name,
		Points: []MetricSeriesPoint{},
		Traces: []*storage.TraceOverlay{},
	}
	for _, m := range rows {
		if out.ServiceName == "" {
			out.ServiceName = m.ServiceName
			out.Type = m.Type
			out.Unit = m.Unit
			out.Description = m.Description
		}
		p := MetricSeriesPoint{TimestampNs: m.TimestampNs, Value: m.Value}
		if m.Type == "histogram" {
			p.Percentile = extractPercentile(m.Attributes)
		}
		// Extract exemplars if present
		if m.Exemplars != "" {
			var exemplars []MetricSeriesExemplar
			if err := json.Unmarshal([]byte(m.Exemplars), &exemplars); err == nil {
				p.Exemplars = exemplars
			}
		}
		out.Points = append(out.Points, p)
	}

	// Optional ?with_traces=1: attach the trace overlay for the chart's
	// time window. Use the min/max of the points we just returned as the
	// window when explicit ?from/?to weren't supplied.
	if q.Get("with_traces") == "1" && len(out.Points) > 0 {
		minNs, maxNs := out.Points[0].TimestampNs, out.Points[0].TimestampNs
		for _, p := range out.Points {
			if p.TimestampNs < minNs {
				minNs = p.TimestampNs
			}
			if p.TimestampNs > maxNs {
				maxNs = p.TimestampNs
			}
		}
		windowFrom, windowTo := from, to
		if windowFrom == 0 {
			windowFrom = minNs
		}
		if windowTo == 0 {
			windowTo = maxNs
		}
		traces, err := r.store.ListTracesInWindow(storage.TraceOverlayFilter{
			Service:   q.Get("service"),
			SessionID: q.Get("sessionId"),
			FromNs:    windowFrom,
			ToNs:      windowTo,
		})
		if err == nil && traces != nil {
			out.Traces = traces
		}
	}
	respond(w, out, len(out.Points), 1)
}

// extractPercentile pulls the "percentile" attribute out of the JSON-encoded
// attributes column. Returns "" if unset/unparseable.
func extractPercentile(attrsJSON string) string {
	const key = `"percentile":"`
	i := strings.Index(attrsJSON, key)
	if i < 0 {
		return ""
	}
	rest := attrsJSON[i+len(key):]
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		return ""
	}
	return rest[:end]
}
