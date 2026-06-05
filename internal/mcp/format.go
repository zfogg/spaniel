package mcp

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/zfogg/spaniel/internal/storage"
)

// nsToMs converts nanoseconds to milliseconds, rounded to 0.1ms.
func nsToMs(ns int64) float64 {
	return float64(ns/1e5) / 10.0
}

// kindString maps an OTel SpanKind int to its name.
func kindString(kind int) string {
	switch kind {
	case 2:
		return "server"
	case 3:
		return "client"
	case 4:
		return "producer"
	case 5:
		return "consumer"
	default:
		return "internal"
	}
}

// statusString maps an OTel StatusCode int to its name (0=unset, 1=ok, 2=error).
func statusString(code int) string {
	switch code {
	case 1:
		return "OK"
	case 2:
		return "ERROR"
	default:
		return "unset"
	}
}

// severityString maps an OTel log severity number to its name.
func severityString(sev int) string {
	switch {
	case sev >= 21:
		return "FATAL"
	case sev >= 17:
		return "ERROR"
	case sev >= 13:
		return "WARN"
	case sev >= 9:
		return "INFO"
	case sev >= 5:
		return "DEBUG"
	case sev >= 1:
		return "TRACE"
	default:
		return "UNSET"
	}
}

// severityNum maps a severity name (case-insensitive) to its minimum OTel
// severity number, or a numeric string to itself. Returns 0 (no threshold) for
// empty or unrecognized input.
func severityNum(name string) int {
	switch strings.ToUpper(strings.TrimSpace(name)) {
	case "":
		return 0
	case "TRACE":
		return 1
	case "DEBUG":
		return 5
	case "INFO":
		return 9
	case "WARN", "WARNING":
		return 13
	case "ERROR":
		return 17
	case "FATAL":
		return 21
	default:
		if n, err := strconv.Atoi(strings.TrimSpace(name)); err == nil {
			return n
		}
		return 0
	}
}

// parseAttrs decodes a stored attributes/resource JSON string into a flat map of
// string values. Non-string values are rendered with %v. Invalid/empty JSON
// yields an empty map.
func parseAttrs(raw string) map[string]string {
	out := map[string]string{}
	if raw == "" {
		return out
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return out
	}
	for k, v := range m {
		out[k] = fmt.Sprintf("%v", v)
	}
	return out
}

// keyAttrOrder lists the semantic-convention attributes most useful for
// debugging, in the order they should be summarized.
var keyAttrOrder = []string{
	"http.request.method", "http.method", "http.route", "url.path", "http.target",
	"http.response.status_code", "http.status_code",
	"db.system", "db.statement", "db.operation",
	"rpc.method", "rpc.service",
	"messaging.system", "messaging.destination.name",
	"exception.type", "exception.message", "error",
}

// truncate shortens s to at most n runes, appending an ellipsis when cut.
func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// attrSummary produces a compact one-line summary of the most relevant
// attributes for a span, suitable for a waterfall line.
func attrSummary(attrs map[string]string) string {
	if len(attrs) == 0 {
		return ""
	}
	var parts []string
	seen := map[string]bool{}
	for _, k := range keyAttrOrder {
		if v, ok := attrs[k]; ok && v != "" && !seen[k] {
			seen[k] = true
			parts = append(parts, fmt.Sprintf("%s=%s", k, truncate(v, 80)))
		}
	}
	return strings.Join(parts, " ")
}

// selectKeyAttrs returns the subset of attrs that are useful for debugging,
// preserving full values. Falls back to all attrs when none of the known keys
// are present (so nothing important is hidden).
func selectKeyAttrs(attrs map[string]string) map[string]string {
	out := map[string]string{}
	for _, k := range keyAttrOrder {
		if v, ok := attrs[k]; ok {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return attrs
	}
	return out
}

// renderWaterfall renders a trace's spans as an indented tree ordered by start
// time, one line per span: name, service, duration, status, and a key-attribute
// summary. Spans are assumed pre-sorted by start_ns (as GetTrace returns them).
func renderWaterfall(traceID string, spans []*storage.Span) string {
	if len(spans) == 0 {
		return fmt.Sprintf("trace %s: no spans found", traceID)
	}

	bySpanID := make(map[string]*storage.Span, len(spans))
	children := make(map[string][]*storage.Span)
	present := make(map[string]bool, len(spans))
	for _, s := range spans {
		bySpanID[s.SpanID] = s
		present[s.SpanID] = true
	}
	var roots []*storage.Span
	for _, s := range spans {
		if s.ParentSpanID == "" || !present[s.ParentSpanID] {
			roots = append(roots, s)
		} else {
			children[s.ParentSpanID] = append(children[s.ParentSpanID], s)
		}
	}
	// Stable order: by start time, then name.
	sortSpans := func(ss []*storage.Span) {
		sort.SliceStable(ss, func(i, j int) bool {
			if ss[i].StartNs != ss[j].StartNs {
				return ss[i].StartNs < ss[j].StartNs
			}
			return ss[i].Name < ss[j].Name
		})
	}
	sortSpans(roots)

	var traceStart int64 = spans[0].StartNs
	for _, s := range spans {
		if s.StartNs < traceStart {
			traceStart = s.StartNs
		}
	}

	var b strings.Builder
	var walk func(s *storage.Span, depth int)
	walk = func(s *storage.Span, depth int) {
		indent := strings.Repeat("  ", depth)
		line := fmt.Sprintf("%s%s  [%s]  %.1fms  %s",
			indent, s.Name, s.ServiceName, nsToMs(s.DurationNs), statusString(s.StatusCode))
		if s.StatusMessage != "" {
			line += "  msg=" + truncate(s.StatusMessage, 80)
		}
		if sum := attrSummary(parseAttrs(s.Attributes)); sum != "" {
			line += "  " + sum
		}
		b.WriteString(line)
		b.WriteByte('\n')
		kids := children[s.SpanID]
		sortSpans(kids)
		for _, c := range kids {
			walk(c, depth+1)
		}
	}
	for _, r := range roots {
		walk(r, 0)
	}
	return strings.TrimRight(b.String(), "\n")
}
