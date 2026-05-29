package ingestion

import (
	"fmt"
	"net/url"
	"strings"

	json "github.com/goccy/go-json"
	"github.com/zfogg/spaniel/internal/storage"
)

// issueID builds a stable primary key for a TraceIssue.
func issueID(traceID, kind, discriminator string) string {
	return fmt.Sprintf("%s-%s-%s", traceID[:min(8, len(traceID))], kind, discriminator[:min(12, len(discriminator))])
}

// isDBSpan returns true when the span carries database attributes.
func isDBSpan(s *storage.Span) bool {
	return findSubstr(s.Attributes, `"db.system"`) ||
		findSubstr(s.Attributes, `"db.statement"`) ||
		findSubstr(s.Attributes, `"db.operation"`)
}

// extractAttrFloat64 parses a numeric attribute from the span attributes JSON.
// Returns 0 if the key is absent or not numeric.
func extractAttrFloat64(attrsJSON, key string) float64 {
	var m map[string]any
	if err := json.Unmarshal([]byte(attrsJSON), &m); err != nil {
		return 0
	}
	switch v := m[key].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	}
	return 0
}

// extractAttrBool parses a boolean attribute. Returns (value, found).
func extractAttrBool(attrsJSON, key string) (bool, bool) {
	var m map[string]any
	if err := json.Unmarshal([]byte(attrsJSON), &m); err != nil {
		return false, false
	}
	v, ok := m[key]
	if !ok {
		return false, false
	}
	b, ok := v.(bool)
	return b, ok
}

// httpHost extracts the effective hostname from a span's HTTP attributes,
// trying url.full, http.url, server.address in that order.
func httpHost(attrsJSON string) string {
	for _, key := range []string{"url.full", "http.url"} {
		if raw := extractAttrString(attrsJSON, key); raw != "" {
			if u, err := url.Parse(raw); err == nil && u.Host != "" {
				return strings.ToLower(u.Hostname())
			}
		}
	}
	return strings.ToLower(extractAttrString(attrsJSON, "server.address"))
}

// isCacheSpan returns true for spans that look like cache operations
// (Redis, Memcached, etc.).
func isCacheSpan(s *storage.Span) bool {
	sys := strings.ToLower(extractAttrString(s.Attributes, "db.system"))
	return sys == "redis" || sys == "memcache" || sys == "memcached" ||
		strings.Contains(strings.ToLower(s.Name), "cache") ||
		findSubstr(s.Attributes, `"cache.hit"`)
}

// isClientSpan returns true for outbound call spans.
func isClientSpan(s *storage.Span) bool {
	return s.Kind == 3 || // SPAN_KIND_CLIENT
		extractAttrString(s.Attributes, "http.method") != "" ||
		extractAttrString(s.Attributes, "http.request.method") != "" ||
		extractAttrString(s.Attributes, "rpc.system") != ""
}

// childrenOf returns spans whose parent is parentSpanID.
func childrenOf(parentSpanID string, spans []*storage.Span) []*storage.Span {
	var out []*storage.Span
	for _, s := range spans {
		if s.ParentSpanID == parentSpanID {
			out = append(out, s)
		}
	}
	return out
}

// spanByID returns the span with the given ID, or nil.
func spanByID(id string, spans []*storage.Span) *storage.Span {
	for _, s := range spans {
		if s.SpanID == id {
			return s
		}
	}
	return nil
}

// coveredNs computes how many nanoseconds of [parentStart, parentEnd] are
// covered by at least one child span. Children outside the parent window are
// clamped. Overlapping children are merged.
func coveredNs(parentStart, parentEnd int64, children []*storage.Span) int64 {
	if len(children) == 0 {
		return 0
	}
	type interval struct{ a, b int64 }
	ivs := make([]interval, 0, len(children))
	for _, c := range children {
		a, b := c.StartNs, c.EndNs
		if a < parentStart {
			a = parentStart
		}
		if b > parentEnd {
			b = parentEnd
		}
		if a < b {
			ivs = append(ivs, interval{a, b})
		}
	}
	if len(ivs) == 0 {
		return 0
	}
	// Sort by start, then merge.
	for i := 1; i < len(ivs); i++ {
		for j := i; j > 0 && ivs[j].a < ivs[j-1].a; j-- {
			ivs[j], ivs[j-1] = ivs[j-1], ivs[j]
		}
	}
	var total int64
	cur := ivs[0]
	for _, iv := range ivs[1:] {
		if iv.a <= cur.b {
			if iv.b > cur.b {
				cur.b = iv.b
			}
		} else {
			total += cur.b - cur.a
			cur = iv
		}
	}
	total += cur.b - cur.a
	return total
}

// areSerial returns true if all spans are non-overlapping (each starts after the previous ends).
func areSerial(spans []*storage.Span) bool {
	if len(spans) < 2 {
		return true
	}
	sorted := make([]*storage.Span, len(spans))
	copy(sorted, spans)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j].StartNs < sorted[j-1].StartNs; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	for i := 1; i < len(sorted); i++ {
		if sorted[i].StartNs < sorted[i-1].EndNs {
			return false
		}
	}
	return true
}
