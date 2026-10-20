package mcp

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	toongo "github.com/toon-format/toon-go"

	"github.com/zfogg/spaniel/internal/storage"
)

func (h *handler) registerDynamicResources(s *mcpsdk.Server) {
	templates := []struct {
		uri, name, title, description string
	}{
		{"spaniel://dashboards/{dashboard_id}", "spaniel-dashboard", "Spaniel dashboard", "A saved dashboard with panels and variables, encoded as TOON."},
		{"spaniel://alerts/{alert_id}", "spaniel-alert", "Spaniel alert", "An alert definition and current instances, encoded as TOON."},
		{"spaniel://spans/{span_id}", "spaniel-span", "Spaniel span", "A stored OpenTelemetry span, encoded as TOON."},
		{"spaniel://traces/{trace_id}", "spaniel-trace", "Spaniel trace", "All spans in a stored trace, encoded as TOON."},
		{"spaniel://logs/{session_id}/{timestamp_ns}", "spaniel-log-records", "Spaniel log records", "Log records at one timestamp in a session, encoded as TOON."},
		{"spaniel://metrics/{session_id}/{service}/{name}", "spaniel-metric-series", "Spaniel metric series", "Up to the most recent 1000 points for a metric stream, encoded as TOON."},
	}
	for _, template := range templates {
		s.AddResourceTemplate(&mcpsdk.ResourceTemplate{URITemplate: template.uri, Name: template.name, Title: template.title, Description: template.description, MIMEType: "text/toon"}, h.readDynamicResource)
	}
}

func (h *handler) readDynamicResource(ctx context.Context, req *mcpsdk.ReadResourceRequest) (*mcpsdk.ReadResourceResult, error) {
	if req == nil || req.Params == nil {
		return nil, fmt.Errorf("resource URI is required")
	}
	raw := req.Params.URI
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "spaniel" {
		return nil, mcpsdk.ResourceNotFoundError(raw)
	}
	parts, err := resourcePathParts(u)
	if err != nil {
		return nil, err
	}
	var value any
	switch u.Host {
	case "dashboards":
		if len(parts) != 1 {
			return nil, mcpsdk.ResourceNotFoundError(raw)
		}
		value, err = h.store.WithContext(ctx).GetDashboard(parts[0])
	case "alerts":
		if len(parts) != 1 {
			return nil, mcpsdk.ResourceNotFoundError(raw)
		}
		value, err = h.store.WithContext(ctx).GetAlertRule(parts[0])
	case "spans":
		if len(parts) != 1 {
			return nil, mcpsdk.ResourceNotFoundError(raw)
		}
		value, err = h.store.WithContext(ctx).GetSpan(parts[0])
	case "traces":
		if len(parts) != 1 {
			return nil, mcpsdk.ResourceNotFoundError(raw)
		}
		value, err = h.store.WithContext(ctx).GetTrace(parts[0])
	case "logs":
		if len(parts) != 2 {
			return nil, mcpsdk.ResourceNotFoundError(raw)
		}
		timestamp, parseErr := strconv.ParseInt(parts[1], 10, 64)
		if parseErr != nil {
			return nil, mcpsdk.ResourceNotFoundError(raw)
		}
		value, err = h.store.WithContext(ctx).ListLogs(storage.LogFilter{SessionID: parts[0], FromNs: timestamp, ToNs: timestamp, Limit: 1000})
	case "metrics":
		if len(parts) != 3 {
			return nil, mcpsdk.ResourceNotFoundError(raw)
		}
		points, queryErr := h.store.WithContext(ctx).GetMetricSeries(storage.MetricSeriesFilter{SessionID: parts[0], Service: parts[1], Name: parts[2], Limit: 1000})
		value, err = points, queryErr
	default:
		return nil, mcpsdk.ResourceNotFoundError(raw)
	}
	if err != nil || value == nil {
		return nil, mcpsdk.ResourceNotFoundError(raw)
	}
	data, err := toongo.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode resource as TOON: %w", err)
	}
	return &mcpsdk.ReadResourceResult{Contents: []*mcpsdk.ResourceContents{{URI: raw, MIMEType: "text/toon", Text: string(data)}}}, nil
}

func resourcePathParts(u *url.URL) ([]string, error) {
	escaped := strings.Trim(u.EscapedPath(), "/")
	if escaped == "" {
		return nil, nil
	}
	rawParts := strings.Split(escaped, "/")
	parts := make([]string, len(rawParts))
	for i, part := range rawParts {
		decoded, err := url.PathUnescape(part)
		if err != nil || decoded == "" {
			return nil, mcpsdk.ResourceNotFoundError(u.String())
		}
		parts[i] = decoded
	}
	return parts, nil
}
