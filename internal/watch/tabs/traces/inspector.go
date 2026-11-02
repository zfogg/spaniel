package traces

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/zfogg/spaniel/internal/api/apigen"
	"github.com/zfogg/spaniel/internal/tui"
)

func spanContent(s apigen.Span, width int) []string {
	status := statusText(s.StatusCode) + " · " + clean(s.StatusMessage)
	if s.StatusCode >= 2 {
		status = errorStyle.Render(status)
	}
	kinds := []string{"unspecified", "internal", "server", "client", "producer", "consumer"}
	kind := fmt.Sprintf("%.0f", s.Kind)
	if s.Kind >= 0 && s.Kind < float64(len(kinds)) {
		kind = kinds[int(s.Kind)]
	}
	lines := []string{
		tui.Title.Render(clean(s.ServiceName) + " / " + clean(s.Name)), status,
		"Trace ID: " + clean(s.TraceId), "Span ID: " + clean(s.SpanId), "Parent ID: " + clean(fallback(s.ParentSpanId, "(root)")),
		"Session: " + clean(s.SessionId) + " · " + clean(s.SessionLabel), "Kind: " + kind, "Duration: " + duration(s.DurationNs),
		"Start: " + stamp(s.StartNs), "End: " + stamp(s.EndNs), "Received: " + stamp(s.ReceivedAt),
	}
	if _, _, ok := timing(s); !ok || s.EndNs == 0 {
		lines = append(lines, "! Timing incomplete or invalid.")
	}
	lines = append(lines, attributeLines("Attributes", s.Attributes, width)...)
	lines = append(lines, attributeLines("Resource", s.Resource, width)...)
	lines = append(lines, "", tui.Title.Render("Events"))
	events := append(s.Events[:0:0], s.Events...)
	sort.SliceStable(events, func(i, j int) bool { return events[i].TimeNs < events[j].TimeNs })
	seen := map[string]bool{}
	for _, e := range events {
		if (e.SessionId != "" && e.SessionId != s.SessionId) || (e.TraceId != "" && e.TraceId != s.TraceId) || (e.SpanId != "" && e.SpanId != s.SpanId) {
			continue
		}
		key := jsonIdentity(e)
		if seen[key] {
			continue
		}
		seen[key] = true
		lines = append(lines, stamp(e.TimeNs)+" · "+clean(e.Name))
		lines = append(lines, attributeLines("Event attributes", e.Attributes, width)...)
	}
	if len(seen) == 0 {
		lines = append(lines, "No events available.")
	}
	lines = append(lines, "", tui.Title.Render("Links"))
	seen = map[string]bool{}
	for _, l := range s.Links {
		if (l.SessionId != "" && l.SessionId != s.SessionId) || (l.TraceId != "" && l.TraceId != s.TraceId) || (l.SpanId != "" && l.SpanId != s.SpanId) {
			continue
		}
		key := jsonIdentity(l)
		if seen[key] {
			continue
		}
		seen[key] = true
		lines = append(lines, "Trace: "+clean(l.LinkedTraceId), "Span: "+clean(l.LinkedSpanId), "Trace state: "+clean(l.TraceState))
		lines = append(lines, attributeLines("Link attributes", l.Attributes, width)...)
	}
	if len(seen) == 0 {
		lines = append(lines, "No links available.")
	}
	return lines
}
func jsonIdentity(v any) string { data, _ := json.Marshal(v); return string(data) }

// List-row notifications do not hydrate events/links. Keep detail-only metadata
// until the next authoritative detail snapshot, as the web inspector does.
func patchSpan(old, incoming apigen.Span) apigen.Span {
	if len(incoming.Events) == 0 {
		incoming.Events = old.Events
	}
	if len(incoming.Links) == 0 {
		incoming.Links = old.Links
	}
	return incoming
}
func mergeLogs(logs []apigen.Log) []apigen.Log {
	seen := map[string]bool{}
	out := make([]apigen.Log, 0, min(pageSize, len(logs)))
	for _, l := range logs {
		key := jsonIdentity(l)
		if !seen[key] {
			seen[key] = true
			out = append(out, l)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].TimestampNs != out[j].TimestampNs {
			return out[i].TimestampNs > out[j].TimestampNs
		}
		return out[i].ReceivedAt > out[j].ReceivedAt
	})
	if len(out) > pageSize {
		out = out[:pageSize]
	}
	return out
}
