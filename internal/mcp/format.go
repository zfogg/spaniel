package mcp

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/zfogg/spaniel/internal/querydiagnostics"
	"github.com/zfogg/spaniel/internal/storage"
)

// nsToMs converts nanoseconds to milliseconds without discarding sub-ms data.
func nsToMs(ns int64) float64 {
	return float64(ns) / 1e6
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

func parseStatusFilter(value string) (int, bool, error) {
	switch value {
	case "":
		return 0, false, nil
	case "unset":
		return 0, true, nil
	case "ok":
		return 1, true, nil
	case "error":
		return 2, true, nil
	default:
		return 0, false, fmt.Errorf("invalid status %q; use unset, ok, or error", value)
	}
}

// UnixNanos is a decimal nanosecond timestamp. It is deliberately a string:
// JSON numbers cannot exactly represent current Unix nanoseconds in JavaScript.
type UnixNanos string

func (n *UnixNanos) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*n = ""
		return nil
	}
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("unix-nanosecond timestamps must be decimal strings: %w", err)
	}
	if value == "" {
		*n = ""
		return nil
	}
	if _, err := strconv.ParseInt(value, 10, 64); err != nil {
		return fmt.Errorf("invalid unix-nanosecond timestamp %q: %w", value, err)
	}
	*n = UnixNanos(value)
	return nil
}

func (n UnixNanos) Int64(field string) (int64, error) {
	if n == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(string(n), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: %w", field, n, err)
	}
	return value, nil
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

// spanAttrs retains the existing scalar presentation while exposing query
// diagnostics as actual structured results, including for older stored spans.
func spanAttrs(raw string, keysOnly bool) map[string]any {
	flat := parseAttrs(raw)
	if keysOnly {
		flat = selectKeyAttrs(flat)
	}
	out := make(map[string]any, len(flat))
	for key, value := range flat {
		out[key] = value
	}
	var attrs map[string]any
	if json.Unmarshal([]byte(raw), &attrs) == nil && attrs != nil {
		querydiagnostics.Normalize(attrs)
		if diagnostics, ok := attrs[querydiagnostics.Attribute]; ok {
			out[querydiagnostics.Attribute] = diagnostics
		}
	}
	return out
}

// keyAttrOrder lists the semantic-convention attributes most useful for
// debugging, in the order they should be summarized.
var keyAttrOrder = []string{
	"http.request.method", "http.method", "http.route", "url.path", "http.target",
	"http.response.status_code", "http.status_code",
	"db.system.name", "db.system", "db.query.text", "db.statement", "db.operation.name", "db.operation",
	"rpc.method", "rpc.service",
	"messaging.system", "messaging.destination.name",
	"exception.type", "exception.message", "error",
}

// oneLine keeps telemetry text from breaking the waterfall's row boundaries.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

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

// renderWaterfall uses the complete trace time range for every timing bar.
// Rows follow parent-child order; explicit offsets show concurrency and gaps.
func renderWaterfall(traceID string, spans []*storage.Span) string {
	if len(spans) == 0 {
		return fmt.Sprintf("trace %s: no spans found", traceID)
	}

	children := make(map[string][]*storage.Span)
	present := make(map[string]bool, len(spans))
	for _, s := range spans {
		present[s.SpanID] = true
	}
	var roots []*storage.Span
	for _, s := range spans {
		if s.ParentSpanID == "" || s.ParentSpanID == "0000000000000000" || !present[s.ParentSpanID] {
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

	traceStart, traceEnd := spans[0].StartNs, spans[0].EndNs
	for _, s := range spans {
		if s.StartNs < traceStart {
			traceStart = s.StartNs
		}
		if s.EndNs > traceEnd {
			traceEnd = s.EndNs
		}
	}
	traceDuration := max(int64(0), traceEnd-traceStart)

	var b strings.Builder
	fmt.Fprintf(&b, "trace %s: %d spans, %.3fms; start_unix_ns=%d\n", oneLine(traceID), len(spans), nsToMs(traceDuration), traceStart)
	b.WriteString("timeline (# = span, . = idle; shared trace scale)  start offset  duration  span\n")
	visited := make(map[*storage.Span]bool, len(spans))
	var walk func(s *storage.Span, depth int)
	walk = func(s *storage.Span, depth int) {
		if visited[s] {
			return
		}
		visited[s] = true
		indent := strings.Repeat("  ", depth)
		line := fmt.Sprintf("%s  %+11.3fms  %11.3fms  %s%s  [%s]  %s  span_id=%s",
			waterfallBar(s.StartNs-traceStart, s.EndNs-traceStart, traceDuration),
			nsToMs(s.StartNs-traceStart), nsToMs(s.DurationNs), indent,
			oneLine(s.Name), oneLine(s.ServiceName), statusString(s.StatusCode), oneLine(s.SpanID))
		if s.StatusMessage != "" {
			line += "  msg=" + oneLine(s.StatusMessage)
		}
		if s.ParentSpanID != "" && s.ParentSpanID != "0000000000000000" && !present[s.ParentSpanID] {
			line += "  missing_parent=" + oneLine(s.ParentSpanID)
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
	// Malformed parent cycles have no root. Keep their spans visible and bound
	// traversal with visited rather than silently omitting the component.
	for _, s := range spans {
		if !visited[s] {
			b.WriteString("unrooted span component (invalid parent cycle):\n")
			walk(s, 0)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func waterfallBar(start, end, duration int64) string {
	const width = 40
	left, right := 0, 1
	if duration > 0 {
		left = min(width-1, max(0, int(float64(start)/float64(duration)*width)))
		right = min(width, max(left+1, int(math.Ceil(float64(end)/float64(duration)*width))))
	}
	return "|" + strings.Repeat(".", left) + strings.Repeat("#", right-left) + strings.Repeat(".", width-right) + "|"
}
