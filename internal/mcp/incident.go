package mcp

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/zfogg/spaniel/internal/errtext"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type IncidentObservation struct {
	Kind          string            `json:"kind"`
	Severity      string            `json:"severity"`
	Summary       string            `json:"summary"`
	Count         int64             `json:"count,omitempty"`
	SampleCount   int64             `json:"sample_count,omitempty"`
	TraceID       string            `json:"trace_id,omitempty"`
	SpanID        string            `json:"span_id,omitempty"`
	Link          string            `json:"link,omitempty"`
	NextTool      string            `json:"next_tool,omitempty"`
	NextArguments map[string]string `json:"next_arguments,omitempty"`
}

type IncidentInvestigationOutput struct {
	Description     string                `json:"description"`
	Instructions    string                `json:"instructions"`
	SessionID       string                `json:"session_id"`
	FromNs          string                `json:"from_ns"`
	ToNs            string                `json:"to_ns"`
	EvaluatedAtNs   string                `json:"evaluated_at_ns"`
	IngestionStatus string                `json:"ingestion_status"`
	Observations    []IncidentObservation `json:"observations"`
	Hypotheses      []string              `json:"hypotheses"`
	Warnings        []string              `json:"warnings"`
}

func (h *handler) investigateIncident(ctx context.Context, _ *mcpsdk.CallToolRequest, in InvestigateIncidentInput) (*mcpsdk.CallToolResult, IncidentInvestigationOutput, error) {
	desc, instructions := investigateIncidentWorkflow(in.Focus)
	now := time.Now().UnixNano()
	from, to, err := parseTimeWindow(in.FromNs, in.ToNs)
	if err != nil {
		return nil, IncidentInvestigationOutput{}, err
	}
	if to == 0 {
		to = now
	}
	if from == 0 {
		from = max(int64(0), to-int64(time.Hour))
	}
	if from > to {
		return nil, IncidentInvestigationOutput{}, fmt.Errorf("invalid time window: from_ns exceeds to_ns")
	}
	fromText, toText := strconv.FormatInt(from, 10), strconv.FormatInt(to, 10)
	session := h.resolveSession(in.SessionID)
	out := IncidentInvestigationOutput{Description: desc, Instructions: instructions, SessionID: session, FromNs: fromText, ToNs: toText, EvaluatedAtNs: strconv.FormatInt(now, 10), IngestionStatus: "unknown", Observations: []IncidentObservation{}, Hypotheses: []string{}, Warnings: []string{"Alert and ingestion states are current snapshots; span observations use the requested time window. Correlation does not establish causation."}}
	if in.Focus != "" {
		out.Warnings = append(out.Warnings, "The free-text focus is guidance, not a data filter; use service to scope spans.")
	}
	_, health, err := h.getIngestionHealth(ctx, nil, emptyInput{})
	if err != nil {
		out.Warnings = append(out.Warnings, "Ingestion health unavailable: "+errtext.Message(err.Error()))
	} else {
		out.IngestionStatus = health.Status
		if health.Status == "degraded" {
			out.Observations = append(out.Observations, IncidentObservation{Kind: "ingestion", Severity: "warning", Summary: fmt.Sprintf("Ingestion is degraded: %v", health.Reasons), NextTool: "get_ingestion_health"})
		}
		if health.Status == "unknown" {
			out.Warnings = append(out.Warnings, "Ingestion health is unknown; missing telemetry does not establish service health.")
		}
	}
	_, alerts, err := h.listAlerts(ctx, nil, ListAlertsInput{State: "attention", Limit: 5})
	if err != nil {
		out.Warnings = append(out.Warnings, "Alert state unavailable: "+errtext.Message(err.Error()))
	} else {
		for _, a := range alerts.Items {
			out.Observations = append(out.Observations, IncidentObservation{Kind: "alert", Severity: a.Rule.Severity, Summary: fmt.Sprintf("Alert %q is %s", a.Rule.Name, a.State), Link: a.Link, NextTool: "get_alert", NextArguments: map[string]string{"alert_id": a.Rule.ID}})
		}
		if alerts.Total > len(alerts.Items) {
			out.Warnings = append(out.Warnings, "Alert observations are limited to five; call list_alerts for the full set.")
		}
	}
	if in.Service != "" {
		_, summary, err := h.getServiceSummary(ctx, nil, GetServiceSummaryInput{Service: in.Service, SessionID: session, FromNs: UnixNanos(fromText), ToNs: UnixNanos(toText), Limit: 5})
		if err != nil {
			out.Warnings = append(out.Warnings, "Service summary unavailable: "+errtext.Message(err.Error()))
		} else {
			out.Warnings = append(out.Warnings, summary.Warnings...)
			if summary.ErrorSpanCount > 0 {
				out.Observations = append(out.Observations, IncidentObservation{Kind: "span_errors", Severity: "warning", Summary: fmt.Sprintf("Service %s has %d error spans among %d spans in the window", in.Service, summary.ErrorSpanCount, summary.SpanCount), Count: summary.ErrorSpanCount, SampleCount: summary.SpanCount, NextTool: "get_service_summary", NextArguments: map[string]string{"service": in.Service, "session_id": session, "from_ns": fromText, "to_ns": toText}})
			}
		}
	}
	_, spans, err := h.listSlowSpans(ctx, nil, ListSlowSpansInput{SessionID: session, Service: in.Service, FromNs: UnixNanos(fromText), ToNs: UnixNanos(toText), Limit: 6})
	if err != nil {
		out.Warnings = append(out.Warnings, "Span evidence unavailable: "+errtext.Message(err.Error()))
	} else {
		if len(spans.Spans) > 5 {
			out.Warnings = append(out.Warnings, "Slowest-span observations are limited to five; call list_slow_spans for more.")
			spans.Spans = spans.Spans[:5]
		}
		if len(spans.Spans) == 0 {
			out.Warnings = append(out.Warnings, "No spans matched the selected session and time window.")
		}
		for _, span := range spans.Spans {
			out.Observations = append(out.Observations, IncidentObservation{Kind: "slow_span", Severity: "info", Summary: fmt.Sprintf("Observed %s / %s at %.2f ms", span.Service, span.Name, span.DurationMs), TraceID: span.TraceID, SpanID: span.SpanID, NextTool: "get_trace", NextArguments: map[string]string{"trace_id": span.TraceID, "session_id": session}})
		}
	}
	return nil, out, nil
}
