package traces

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/zfogg/spaniel/internal/api/apigen"
	"github.com/zfogg/spaniel/internal/tui"
	"github.com/zfogg/spaniel/internal/watch/metadata"
	"github.com/zfogg/spaniel/internal/watch/terminal"
)

func spanContent(s apigen.Span, width int, now ...time.Time) []string {
	reference := terminal.BatchTime(now...)
	status := metadata.Field{Name: "Status", Value: statusText(s.StatusCode) + " · " + clean(s.StatusMessage)}
	if s.StatusCode >= 2 {
		status.Style = errorStyle
	}
	kinds := []string{"unspecified", "internal", "server", "client", "producer", "consumer"}
	kind := fmt.Sprintf("%.0f", s.Kind)
	if s.Kind >= 0 && s.Kind < float64(len(kinds)) {
		kind = kinds[int(s.Kind)]
	}
	lines := metadata.Table([]metadata.Field{
		{Name: "Service", Value: s.ServiceName}, {Name: "Operation", Value: s.Name}, status,
		{Name: "Trace ID", Value: s.TraceId}, {Name: "Span ID", Value: s.SpanId},
		{Name: "Parent ID", Value: fallback(s.ParentSpanId, "(root)")},
		{Name: "Session", Value: s.SessionId + " · " + s.SessionLabel},
		{Name: "Kind", Value: kind}, {Name: "Duration", Value: duration(s.DurationNs)},
		{Name: "Start", Value: stamp(s.StartNs, reference)}, {Name: "End", Value: stamp(s.EndNs, reference)},
		{Name: "Received", Value: stamp(s.ReceivedAt, reference)},
	}, width)
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
		lines = append(lines, stamp(e.TimeNs, reference)+" · "+clean(e.Name))
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
