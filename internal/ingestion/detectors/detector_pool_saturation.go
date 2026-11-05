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
	groups := map[string]*group{}
	for _, s := range spans {
		if s.SessionID != "" && s.SessionID != sessionID {
			continue
		}
		operation := strings.ToLower(extractAttrString(s.Attributes, "db.operation.name"))
		if operation != "connection.acquire" && operation != "connection.wait" {
			continue
		}
		pool := extractAttrString(s.Attributes, "db.client.connection.pool.name")
		if pool == "" || s.ServiceName == "" || s.SpanID == "" {
			continue
		}
		key := s.ServiceName + "\x00" + pool
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
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var issues []*storage.TraceIssue
	for _, key := range keys {
		g := groups[key]
		if !g.timeout && len(g.spans) < 3 {
			continue
		}
		sort.Slice(g.spans, func(i, j int) bool { return g.spans[i].SpanID < g.spans[j].SpanID })
		parts := strings.SplitN(key, "\x00", 2)
		evidence := "repeated waits >=100ms"
		if g.timeout {
			evidence = "acquire timeout"
		}
		hash := sha256.Sum256([]byte(sessionID + "\x00" + traceID + "\x00" + key))
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
		issues = append(issues, &storage.TraceIssue{ID: fmt.Sprintf("pool-%x", hash), TraceID: traceID, SessionID: sessionID, Kind: "pool_saturation", Fingerprint: fmt.Sprintf("service=%s pool=%s; %s", parts[0], parts[1], evidence), Count: len(g.spans), WastedNs: g.wait, ParentSpanID: s.ParentSpanID, ExampleSpanID: s.SpanID, CreatedAt: now})
	}
	return issues
}
