package logs

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/zfogg/spaniel/internal/api/apigen"
	"github.com/zfogg/spaniel/internal/liveevent"
	"github.com/zfogg/spaniel/internal/watch/terminal"
)

// The wire contract has no row ID. All durable fields, including received_at,
// distinguish records; byte-identical records are indistinguishable on the wire.
func identity(row apigen.Log) string {
	b, _ := json.Marshal(row)
	return fmt.Sprintf("%x", sha256.Sum256(b))
}
func sortedUnique(rows []apigen.Log, limit int) []apigen.Log {
	out := make([]apigen.Log, 0, min(len(rows), limit))
	seen := make(map[string]bool, len(rows))
	for _, row := range rows {
		id := identity(row)
		if !seen[id] {
			seen[id] = true
			out = append(out, row)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TimestampNs != out[j].TimestampNs {
			return out[i].TimestampNs > out[j].TimestampNs
		}
		if out[i].ReceivedAt != out[j].ReceivedAt {
			return out[i].ReceivedAt > out[j].ReceivedAt
		}
		return identity(out[i]) < identity(out[j])
	})
	return out[:min(len(out), limit)]
}
func liveRow(event liveevent.Message) (apigen.Log, bool) {
	var payload *liveevent.SelfLogPayload
	switch p := event.Decoded.(type) {
	case *liveevent.LogPayload:
		payload = p.Row
	case *liveevent.SelfLogPayload:
		payload = p
	}
	if payload == nil {
		return apigen.Log{}, false
	} // Legacy notifications are repaired by HTTP.
	return apigen.Log{Attributes: payload.Attributes, Body: payload.Body, ReceivedAt: payload.ReceivedAt, ServiceName: payload.ServiceName, SessionId: payload.SessionID, Severity: float64(payload.Severity), SpanId: payload.SpanID, TimestampNs: payload.TimestampNs, TraceId: payload.TraceID}, true
}
func matches(row apigen.Log, session, service, severity, text string, errorsOnly bool) bool {
	if session != "" && row.SessionId != session {
		return false
	}
	if service != "" && row.ServiceName != service {
		return false
	}
	if severity != "" && strings.ToLower(severityName(row.Severity)) != severity {
		return false
	}
	if errorsOnly && row.Severity < 17 {
		return false
	}
	return text == "" || strings.Contains(strings.ToLower(row.Body+" "+row.Attributes+" "+row.ServiceName+" "+row.TraceId+" "+row.SpanId), strings.ToLower(text))
}
func severityName(severity float64) string {
	switch {
	case severity >= 21:
		return "FATAL"
	case severity >= 17:
		return "ERROR"
	case severity >= 13:
		return "WARN"
	case severity >= 9:
		return "INFO"
	case severity >= 5:
		return "DEBUG"
	case severity >= 1:
		return "TRACE"
	default:
		return "UNSET"
	}
}

// Telemetry is untrusted terminal text: remove escape sequences and controls,
// preserving line breaks for the inspector. No body/attribute truncation there.
func clean(s string) string { return terminal.Multiline(s) }

func preview(s string) string { return strings.Join(strings.Fields(clean(s)), " ") }
func cell(s string, width int) string {
	return ansi.Truncate(s, width, "…") + strings.Repeat(" ", max(0, width-ansi.StringWidth(s)))
}
func fit(lines []string, width, height int) string {
	lines = append([]string(nil), lines[:min(len(lines), max(1, height))]...)
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], max(1, width), "…")
	}
	return strings.Join(lines, "\n")
}
func stamp(ns int64, full bool) string {
	if ns == 0 {
		return "unknown"
	}
	if full {
		return time.Unix(0, ns).Format("2006-01-02 15:04:05.000 -07:00")
	}
	return time.Unix(0, ns).Format("15:04:05.000")
}
