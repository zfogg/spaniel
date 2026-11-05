package detectors

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/zfogg/spaniel/internal/storage"
)

// PoolSaturationDetector only evaluates explicitly instrumented pool acquisition spans.
type PoolSaturationDetector struct{}

func (*PoolSaturationDetector) Kind() string { return "pool_saturation" }

func (*PoolSaturationDetector) Analyze(traceID, sessionID string, spans []*storage.Span, now int64) []*storage.TraceIssue {
	type group struct {
		spans   []*storage.Span
		timeout bool
		wait    int64
	}
	type groupKey struct {
		session, service, pool string
		window                 int64
	}
	groups := map[groupKey]*group{}
	seen := map[string]bool{}
	for _, s := range spans {
		session := s.SessionID
		if session == "" {
			session = sessionID
		}
		identity := session + "\x00" + s.SpanID
		if seen[identity] {
			continue
		}
		seen[identity] = true
		operation := strings.ToLower(extractAttrString(s.Attributes, "db.operation.name"))
		if operation != "connection.acquire" && operation != "connection.wait" {
			continue
		}
		pool := extractAttrString(s.Attributes, "db.client.connection.pool.name")
		if pool == "" || s.ServiceName == "" || s.SpanID == "" {
			continue
		}
		key := groupKey{session, s.ServiceName, pool, s.StartNs / int64(5*time.Minute)}
		g := groups[key]
		if g == nil {
			g = &group{}
			groups[key] = g
		}
		errorType := strings.ToLower(extractAttrString(s.Attributes, "error.type"))
		timeout := s.StatusCode == 2 && (errorType == "connection_acquire_timeout" || errorType == "connection_pool_timeout")
		duration := s.EndNs - s.StartNs
		if timeout || duration >= int64(100*time.Millisecond) {
			g.spans = append(g.spans, s)
			g.timeout = g.timeout || timeout
			if duration > 0 {
				g.wait += duration
			}
		}
	}
	keys := make([]groupKey, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a.session != b.session {
			return a.session < b.session
		}
		if a.service != b.service {
			return a.service < b.service
		}
		if a.pool != b.pool {
			return a.pool < b.pool
		}
		return a.window < b.window
	})
	var issues []*storage.TraceIssue
	for _, key := range keys {
		g := groups[key]
		if !g.timeout && len(g.spans) < 3 {
			continue
		}
		sort.Slice(g.spans, func(i, j int) bool { return g.spans[i].SpanID < g.spans[j].SpanID })
		evidence := "repeated waits >=100ms"
		if g.timeout {
			evidence = "acquire timeout"
		}
		hash := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%d", key.session, traceID, key.service, key.pool, key.window)))
		s := g.spans[0]
		if g.timeout {
			for _, candidate := range g.spans {
				errorType := strings.ToLower(extractAttrString(candidate.Attributes, "error.type"))
				if candidate.StatusCode == 2 && (errorType == "connection_acquire_timeout" || errorType == "connection_pool_timeout") {
					s = candidate
					break
				}
			}
		}
		issues = append(issues, &storage.TraceIssue{ID: fmt.Sprintf("pool-%x", hash), TraceID: traceID, SessionID: key.session, Kind: "pool_saturation", Fingerprint: fmt.Sprintf("service=%s pool=%s; %s", key.service, key.pool, evidence), Count: len(g.spans), WastedNs: g.wait, ParentSpanID: s.ParentSpanID, ExampleSpanID: s.SpanID, CreatedAt: now})
	}
	return issues
}
