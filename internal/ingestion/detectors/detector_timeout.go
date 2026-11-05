package detectors

import (
	"fmt"
	"sort"
	"strings"

	json "github.com/goccy/go-json"
	"github.com/zfogg/spaniel/internal/storage"
)

// TimeoutDetector summarizes explicit timeout evidence in the observed trace.
// It does not infer timeouts from latency or arbitrary exception messages.
type TimeoutDetector struct{}

func (*TimeoutDetector) Kind() string { return "timeout" }

func timeoutSignal(s *storage.Span) string {
	var attrs map[string]any
	_ = json.Unmarshal([]byte(s.Attributes), &attrs)
	val := func(k string) string { v, _ := attrs[k].(string); return strings.ToLower(strings.TrimSpace(v)) }
	// Cancellation takes precedence over generic status text.
	if val("operation.termination") == "caller_cancelled" || val("error.type") == "context_canceled" || val("error.type") == "context_cancelled" {
		return "cancelled"
	}
	for _, k := range []string{"error.type", "exception.type"} {
		switch val(k) {
		case "context_deadline_exceeded", "deadline_exceeded", "deadlineexceeded", "timeouterror", "timeout_error", "timeout":
			return "timeout"
		case "context_canceled", "context_cancelled", "canceled", "cancelled":
			return "cancelled"
		}
	}
	// gRPC status codes are protocol-defined, including on successful handlers.
	switch v := attrs["rpc.grpc.status_code"].(type) {
	case float64:
		if v == 4 {
			return "timeout"
		}
		if v == 1 {
			return "cancelled"
		}
	case string:
		if v == "4" || strings.EqualFold(v, "DEADLINE_EXCEEDED") {
			return "timeout"
		}
		if v == "1" || strings.EqualFold(v, "CANCELLED") {
			return "cancelled"
		}
	}
	switch strings.ToLower(strings.TrimSpace(s.StatusMessage)) {
	case "context deadline exceeded", "deadline exceeded":
		return "timeout"
	case "context canceled", "context cancelled":
		return "cancelled"
	}
	return ""
}

func (*TimeoutDetector) Analyze(traceID, sessionID string, spans []*storage.Span, now int64) []*storage.TraceIssue {
	byID := make(map[string]*storage.Span, len(spans))
	signals := make(map[string]string, len(spans))
	var timed []*storage.Span
	cancelledChildren := make(map[string]int)
	for _, s := range spans {
		byID[s.SpanID] = s
		signals[s.SpanID] = timeoutSignal(s)
		if signals[s.SpanID] == "cancelled" {
			cancelledChildren[s.ParentSpanID]++
		}
		if signals[s.SpanID] == "timeout" {
			timed = append(timed, s)
		}
	}
	sort.Slice(timed, func(i, j int) bool { return timed[i].SpanID < timed[j].SpanID })
	// Join timeout evidence only along observed parent-child ancestry. Bound walks
	// and detect cycles so malformed or partial traces cannot loop indefinitely.
	parent := make(map[string]string, len(timed))
	for _, s := range timed {
		parent[s.SpanID] = s.SpanID
	}
	var root func(string) string
	root = func(id string) string {
		for parent[id] != id {
			parent[id] = parent[parent[id]]
			id = parent[id]
		}
		return id
	}
	hasTimedDescendant := make(map[string]bool)
	for _, s := range timed {
		seen := map[string]bool{s.SpanID: true}
		id := s.ParentSpanID
		for steps := 0; id != "" && steps < 1024 && !seen[id]; steps++ {
			seen[id] = true
			ancestor := byID[id]
			if ancestor == nil {
				break
			}
			if signals[id] == "timeout" {
				a, b := root(s.SpanID), root(id)
				if a != b {
					parent[a] = b
				}
				hasTimedDescendant[id] = true
				break
			}
			id = ancestor.ParentSpanID
		}
	}
	groups := make(map[string][]*storage.Span)
	for _, s := range timed {
		r := root(s.SpanID)
		groups[r] = append(groups[r], s)
	}
	var issues []*storage.TraceIssue
	for _, group := range groups {
		// A leaf remains the anchor when an upstream timeout arrives late.
		representative := group[0]
		for _, s := range group {
			if !hasTimedDescendant[s.SpanID] {
				representative = s
				break
			}
		}
		handled := 0
		for _, s := range group {
			if p := byID[s.ParentSpanID]; p != nil && p.StatusCode == 1 && signals[p.SpanID] == "" {
				handled++
			}
		}
		cancelled := 0
		for _, s := range group {
			cancelled += cancelledChildren[s.SpanID]
		}
		summary := fmt.Sprintf("%d observed timeout span(s); %d with successful parent; %d adjacent cancellation span(s)", len(group), handled, cancelled)
		summary += "; representative evidence: " + timeoutEvidence(representative)
		var attrs map[string]any
		_ = json.Unmarshal([]byte(representative.Attributes), &attrs)
		if raw, ok := attrs["deadline.unix_nano"].(string); ok {
			var deadline int64
			if _, err := fmt.Sscan(raw, &deadline); err == nil && fmt.Sprint(deadline) == raw && representative.StartNs > 0 && representative.EndNs >= representative.StartNs && deadline > representative.StartNs {
				summary += fmt.Sprintf("; representative deadline budget %d ns, remaining %d ns", deadline-representative.StartNs, deadline-representative.EndNs)
			}
		}
		issues = append(issues, &storage.TraceIssue{ID: fmt.Sprintf("%d:%s:%s-timeout-%s", len(sessionID), sessionID, traceID, representative.SpanID), TraceID: traceID, SessionID: sessionID, Kind: "timeout", Fingerprint: summary, Count: len(group), ParentSpanID: representative.ParentSpanID, ExampleSpanID: representative.SpanID, CreatedAt: now})
	}
	sort.Slice(issues, func(i, j int) bool { return issues[i].ID < issues[j].ID })
	return issues
}

// timeoutEvidence names the source without copying untrusted attribute values.
func timeoutEvidence(s *storage.Span) string {
	var attrs map[string]any
	_ = json.Unmarshal([]byte(s.Attributes), &attrs)
	if timeoutSignal(&storage.Span{Attributes: s.Attributes}) == "timeout" {
		for _, key := range []string{"error.type", "exception.type"} {
			if v, ok := attrs[key].(string); ok {
				switch strings.ToLower(strings.TrimSpace(v)) {
				case "context_deadline_exceeded", "deadline_exceeded", "deadlineexceeded", "timeouterror", "timeout_error", "timeout":
					return key
				}
			}
		}
		return "gRPC deadline exceeded"
	}
	return "span status deadline exceeded"
}
