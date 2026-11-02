package client

import (
	"context"
	"github.com/zfogg/spaniel/internal/api/apigen"
	"net/url"
)

// Envelope preserves pagination and alert timeline metadata.
type Envelope[T any] struct {
	Data     T                     `json:"data"`
	Meta     *apigen.Meta          `json:"meta,omitempty"`
	Silences []apigen.AlertSilence `json:"silences,omitempty"`
}

func get[T any](ctx context.Context, c *Client, path string, q url.Values) (Envelope[T], error) {
	var out Envelope[T]
	err := c.read(ctx, "GET", path, q, nil, &out)
	return out, err
}

// Scope clones query parameters so concurrent feature requests never mutate
// each other's filters. An empty session means the API's default scope.
func Scope(q url.Values, session string) url.Values {
	out := url.Values{}
	for k, v := range q {
		out[k] = append([]string(nil), v...)
	}
	if session != "" {
		out.Set("sessionId", session)
	}
	return out
}
func (c *Client) Traces(ctx context.Context, q url.Values) (Envelope[[]apigen.TraceRow], error) {
	return get[[]apigen.TraceRow](ctx, c, "/api/traces", q)
}
func (c *Client) Spans(ctx context.Context, q url.Values) (Envelope[[]apigen.SpanRow], error) {
	return get[[]apigen.SpanRow](ctx, c, "/api/spans", q)
}
func (c *Client) Logs(ctx context.Context, q url.Values) (Envelope[[]apigen.Log], error) {
	return get[[]apigen.Log](ctx, c, "/api/logs", q)
}
func (c *Client) Metrics(ctx context.Context, q url.Values) (Envelope[[]apigen.MetricCatalogEntry], error) {
	return get[[]apigen.MetricCatalogEntry](ctx, c, "/api/metrics", q)
}
func (c *Client) MetricSeries(ctx context.Context, q url.Values) (Envelope[apigen.MetricSeries], error) {
	return get[apigen.MetricSeries](ctx, c, "/api/metrics/series", q)
}
func (c *Client) Lints(ctx context.Context, q url.Values) (Envelope[[]apigen.LintWarning], error) {
	return get[[]apigen.LintWarning](ctx, c, "/api/lint", q)
}
func (c *Client) Issues(ctx context.Context, q url.Values) (Envelope[[]apigen.TraceIssue], error) {
	return get[[]apigen.TraceIssue](ctx, c, "/api/issues", q)
}
func (c *Client) Dashboards(ctx context.Context, q url.Values) (Envelope[[]apigen.Dashboard], error) {
	return get[[]apigen.Dashboard](ctx, c, "/api/dashboards", q)
}
func (c *Client) Alerts(ctx context.Context, q url.Values) (Envelope[apigen.AlertList], error) {
	return get[apigen.AlertList](ctx, c, "/api/alerts", q)
}
func (c *Client) AlertHistory(ctx context.Context, q url.Values) (Envelope[[]apigen.AlertEvent], error) {
	return get[[]apigen.AlertEvent](ctx, c, "/api/alerts/history", q)
}
func (c *Client) Sessions(ctx context.Context, q url.Values) (Envelope[[]apigen.Session], error) {
	return get[[]apigen.Session](ctx, c, "/api/sessions", q)
}
func (c *Client) ActiveSession(ctx context.Context, q url.Values) (Envelope[apigen.ActiveSession], error) {
	return get[apigen.ActiveSession](ctx, c, "/api/sessions/active", q)
}
func (c *Client) Trace(ctx context.Context, id string, q url.Values) (Envelope[[]apigen.Span], error) {
	return get[[]apigen.Span](ctx, c, "/api/traces/"+url.PathEscape(id), q)
}
func (c *Client) Span(ctx context.Context, id string, q url.Values) (Envelope[apigen.Span], error) {
	return get[apigen.Span](ctx, c, "/api/spans/"+url.PathEscape(id), q)
}
func (c *Client) Dashboard(ctx context.Context, id string, q url.Values) (Envelope[apigen.Dashboard], error) {
	query := Scope(q, "")
	query.Set("markSeen", "false")
	return get[apigen.Dashboard](ctx, c, "/api/dashboards/"+url.PathEscape(id), query)
}
func (c *Client) Alert(ctx context.Context, id string, q url.Values) (Envelope[apigen.AlertRule], error) {
	query := Scope(q, "")
	query.Set("markSeen", "false")
	return get[apigen.AlertRule](ctx, c, "/api/alerts/"+url.PathEscape(id), query)
}
func (c *Client) AlertEvents(ctx context.Context, id string, q url.Values) (Envelope[[]apigen.AlertEvent], error) {
	return get[[]apigen.AlertEvent](ctx, c, "/api/alerts/"+url.PathEscape(id)+"/events", q)
}

func (c *Client) Preview(ctx context.Context, id string, in apigen.PreviewDashboardQueryJSONBody) (Envelope[apigen.QueryPreview], error) {
	var out Envelope[apigen.QueryPreview]
	err := c.read(ctx, "POST", "/api/dashboards/"+url.PathEscape(id)+"/query-preview", nil, in, &out)
	return out, err
}

func (c *Client) IncomingLinks(ctx context.Context, id string, q url.Values) (Envelope[[]apigen.SpanLink], error) {
	return get[[]apigen.SpanLink](ctx, c, "/api/traces/"+url.PathEscape(id)+"/incoming-links", q)
}
func (c *Client) SpanGroups(ctx context.Context, q url.Values) (Envelope[[]apigen.SpanGroup], error) {
	return get[[]apigen.SpanGroup](ctx, c, "/api/spans", q)
}
func (c *Client) QueryCatalog(ctx context.Context, q url.Values) (Envelope[[]apigen.QueryCatalogEntry], error) {
	return get[[]apigen.QueryCatalogEntry](ctx, c, "/api/query-catalog", q)
}

func (c *Client) MetricCardinality(ctx context.Context, q url.Values) (Envelope[[]apigen.MetricCardinalityStream], error) {
	return get[[]apigen.MetricCardinalityStream](ctx, c, "/api/metrics/cardinality", q)
}
