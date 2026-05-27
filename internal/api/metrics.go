package api

import (
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
		respondErr(w, 500, err.Error())
		return
	}
	if entries == nil {
		entries = []*storage.MetricCatalogEntry{}
	}
	respond(w, entries, len(entries), 1)
}

// MetricSeriesResponse is the shape returned by GET /api/metrics/series.
// Histogram percentiles arrive bucketed into p50/p95/p99 slices on the
// client; gauge/counter populate only Value.
type MetricSeriesPoint struct {
	TimestampNs int64   `json:"timestamp_ns"`
	Value       float64 `json:"value"`
	Percentile  string  `json:"percentile,omitempty"`
}

type MetricSeriesResponse struct {
	Name        string              `json:"name"`
	ServiceName string              `json:"service_name"`
	Type        string              `json:"type"`
	Unit        string              `json:"unit"`
	Description string              `json:"description"`
	Points      []MetricSeriesPoint `json:"points"`
}

// getMetricSeries returns the time series for one metric.
//   GET /api/metrics/series?name=&service=&sessionId=&from=&to=
func (r *Router) getMetricSeries(w http.ResponseWriter, req *http.Request) {
	q := req.URL.Query()
	name := q.Get("name")
	if name == "" {
		respondErr(w, 400, "name query param required")
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
		respondErr(w, 500, err.Error())
		return
	}

	out := MetricSeriesResponse{Name: name, Points: []MetricSeriesPoint{}}
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
		out.Points = append(out.Points, p)
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
