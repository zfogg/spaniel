package detectors

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"

	"github.com/zfogg/spaniel/internal/storage"
)

// FanoutDetector identifies broad outbound work beneath a single observed parent.
type FanoutDetector struct{}

func (*FanoutDetector) Kind() string { return "fanout_amplification" }

func (*FanoutDetector) Analyze(traceID, sessionID string, spans []*storage.Span, now int64) []*storage.TraceIssue {
	// Avoid unbounded per-trace memory and sorting on exceptionally large traces.
	if len(spans) > 10000 {
		return nil
	}
	parents := map[string]*storage.Span{}
	seen := map[string]bool{}
	for _, s := range spans {
		if s.SpanID != "" {
			parents[s.SpanID] = s
		}
	}
	type group struct {
		spans []*storage.Span
		label string
	}
	byParent := map[string]map[string]*group{}
	for _, s := range spans {
		if s.SpanID == "" || seen[s.SpanID] {
			continue
		}
		seen[s.SpanID] = true
		p := parents[s.ParentSpanID]
		// Database repetition belongs to N+1; server spans are never outbound.
		if p == nil || p.EndNs <= p.StartNs || (s.Kind != 3 && s.Kind != 4) || isDBSpan(s) || s.EndNs <= s.StartNs {
			continue
		}
		dep := extractAttrString(s.Attributes, "server.address")
		if dep == "" {
			dep = extractAttrString(s.Attributes, "peer.service")
		}
		if dep == "" {
			dep = extractAttrString(s.Attributes, "messaging.destination.name")
		}
		if dep == "" {
			dep = httpHost(s.Attributes)
		}
		if dep == "" || s.Name == "" {
			continue
		}
		dep = strings.ToLower(dep)
		key := dep + "\x00" + s.Name
		if byParent[p.SpanID] == nil {
			byParent[p.SpanID] = map[string]*group{}
		}
		g := byParent[p.SpanID][key]
		if g == nil {
			g = &group{label: dep + " / " + s.Name}
			byParent[p.SpanID][key] = g
		}
		g.spans = append(g.spans, s)
	}
	var issues []*storage.TraceIssue
	for pid, groups := range byParent {
		var children []*storage.Span
		var labels []string
		for _, g := range groups {
			// The repeated HTTP detector already explains groups of five.
			if len(g.spans) >= chattyHTTPThreshold {
				continue
			}
			children = append(children, g.spans...)
			example := g.spans[0].SpanID
			for _, span := range g.spans[1:] {
				if span.SpanID < example {
					example = span.SpanID
				}
			}
			labels = append(labels, fmt.Sprintf("%s (%d; example %s)", g.label, len(g.spans), example))
		}
		if len(children) < 5 || len(labels) < 3 {
			continue
		}
		sort.Strings(labels)
		sort.Slice(children, func(i, j int) bool { return children[i].SpanID < children[j].SpanID })
		p := parents[pid]
		var sum int64
		for _, c := range children {
			a, b := max(c.StartNs, p.StartNs), min(c.EndNs, p.EndNs)
			if b > a {
				sum += b - a
			}
		}
		covered := coveredNs(p.StartNs, p.EndNs, children)
		if covered == 0 {
			continue
		}
		// Bound the display even when many unique groups occur.
		groupCount := len(labels)
		if len(labels) > 8 {
			labels = append(labels[:8], "…")
		}
		fp := fmt.Sprintf("Potential fan-out: %s; %d calls across %d groups; covered %dns of parent, overlapping %dns; %s", p.Name, len(children), groupCount, covered, sum-covered, strings.Join(labels, ", "))
		issues = append(issues, &storage.TraceIssue{ID: fmt.Sprintf("%s-fanout-%x", traceID, sha256.Sum256([]byte(pid))), TraceID: traceID, SessionID: sessionID, Kind: "fanout_amplification", Fingerprint: fp, Count: len(children), ParentSpanID: pid, ExampleSpanID: children[0].SpanID, CreatedAt: now})
	}
	return issues
}
