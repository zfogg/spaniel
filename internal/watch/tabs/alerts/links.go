package alerts

import (
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/zfogg/spaniel/internal/watch/protocol"
)

type telemetryLink struct{ trace, span, external, unavailable, source string }

func (l telemetryLink) key() string {
	return l.trace + "/" + l.span + "/" + l.external + "/" + l.unavailable
}
func validID(id string, n int) bool {
	if len(id) != n || strings.Trim(id, "0") == "" {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}
func first(v map[string]string, keys ...string) string {
	for _, key := range keys {
		if v[key] != "" {
			return v[key]
		}
	}
	return ""
}
func fromMap(v map[string]string, source string) []telemetryLink {
	var result []telemetryLink
	trace, span := first(v, "trace_id", "traceId", "traceID"), first(v, "span_id", "spanId", "spanID")
	if trace != "" || span != "" {
		l := telemetryLink{trace: trace, span: span, source: source}
		if (trace != "" && !validID(trace, 32)) || (span != "" && !validID(span, 16)) {
			l.unavailable = "Malformed telemetry ID"
		}
		result = append(result, l)
	}
	for _, key := range sortedKeys(v) {
		raw := v[key]
		if strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "https://") || strings.HasPrefix(raw, "http://") {
			result = append(result, parseLink(raw, source+"."+key))
		}
	}
	return result
}
func parseLink(raw, source string) telemetryLink {
	l := telemetryLink{source: source}
	u, err := url.Parse(raw)
	if err != nil || strings.ContainsAny(raw, "\x1b\r\n") {
		l.unavailable = "Invalid link"
		return l
	}
	// Absolute URLs are displayed as text only, even when their path resembles
	// this server's trace routes. Never send an external ID to the local viewer.
	if u.IsAbs() || u.Host != "" {
		if (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && u.User == nil {
			l.external = raw
		} else {
			l.unavailable = "Unsupported external link"
		}
		return l
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	q := u.Query()
	l.trace = first(map[string]string{"trace_id": q.Get("trace_id"), "traceId": q.Get("traceId")}, "trace_id", "traceId")
	l.span = first(map[string]string{"span_id": q.Get("span_id"), "spanId": q.Get("spanId")}, "span_id", "spanId")
	if l.span == "" {
		l.span = q.Get("span")
	}
	if len(parts) > 0 {
		switch parts[0] {
		case "traces", "trace":
			if len(parts) > 1 {
				l.trace = parts[1]
			} else if l.trace == "" {
				l.trace = q.Get("id")
			}
		case "spans", "span":
			if len(parts) > 1 {
				l.span = parts[1]
			} else if l.span == "" {
				l.span = q.Get("id")
			}
		default:
			l.unavailable = "No telemetry target: " + oneLine(raw)
			return l
		}
	}
	if (l.trace == "" && l.span == "") || (l.trace != "" && !validID(l.trace, 32)) || (l.span != "" && !validID(l.span, 16)) {
		l.unavailable = "Missing or invalid telemetry target: " + oneLine(raw)
	}
	return l
}
func (m *model) links() []telemetryLink {
	links := fromMap(parseMap(m.rule.AnnotationsJson), "rule annotations")
	for _, r := range m.rule.Instances {
		if r.GroupKey == m.instance {
			links = append(links, fromMap(parseMap(r.LabelsJson), "selected instance")...)
		}
	}
	for _, e := range m.events {
		if e.Id == m.event {
			links = append(links, fromMap(parseMap(e.Detail), "selected event")...)
			// A plain event detail may itself be a URL; other free text remains text.
			if strings.HasPrefix(e.Detail, "/") || strings.HasPrefix(e.Detail, "https://") || strings.HasPrefix(e.Detail, "http://") {
				links = append(links, parseLink(e.Detail, "selected event"))
			}
		}
	}
	seen := map[string]bool{}
	var result []telemetryLink
	for _, l := range links {
		if !seen[l.key()] {
			seen[l.key()] = true
			result = append(result, l)
		}
		if len(result) == 128 {
			break
		}
	}
	return result
}
func (m *model) linkLines() []string {
	lines := []string{"Links from rule annotations, selected instance and selected history event.", "Enter follows telemetry via the shared viewer. External URLs are text only."}
	var ids []string
	for _, l := range m.links() {
		ids = append(ids, l.key())
		text := "trace " + l.trace + " · span " + l.span
		if l.external != "" {
			text = "External (display only): " + l.external
		}
		if l.unavailable != "" {
			text = l.unavailable
		}
		lines = append(lines, marker(l.key() == m.link)+oneLine(text)+" · "+oneLine(l.source))
	}
	if len(ids) == 0 {
		lines = append(lines, "No correlated telemetry links exposed. Alert context remains available.")
	}
	lines = append(lines, "Missing or expired telemetry is reported by the shared viewer; Escape returns here.")
	return m.selectedWindow(lines, m.link, ids, 2)
}
func (m *model) navigate() tea.Cmd {
	if m.deleted() {
		m.notice = "Rule was deleted; retained links are stale."
		return nil
	}
	for _, l := range m.links() {
		if l.key() != m.link {
			continue
		}
		if l.external != "" {
			m.notice = "External URL displayed safely; no browser is launched."
			return nil
		}
		if l.unavailable != "" {
			m.notice = l.unavailable
			return nil
		}
		back := protocol.ReturnContext{Tab: protocol.Alerts, Selection: fmt.Sprintf("%s/%s", m.id, m.section)}
		if l.span != "" {
			return func() tea.Msg { return protocol.OpenSpan{TraceID: l.trace, SpanID: l.span, Return: back} }
		}
		if l.trace != "" {
			return func() tea.Msg { return protocol.OpenTrace{TraceID: l.trace, Return: back} }
		}
	}
	m.notice = "No correlated telemetry link available."
	return nil
}
