package ingestion

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/telemetry"
	"github.com/zfogg/spaniel/internal/ws"
)

const redeliveryWindow = int64(10 * time.Minute)
const redeliveryMaxGroups = 4096
const redeliveryMaxSpans = 64

type deliveryEvidence struct {
	trace, span, parent string
	start               int64
	failed              bool
	attempt             int
}
type deliveryGroup struct {
	session, destination string
	issueID              string
	touched              int64
	evidence             map[string]deliveryEvidence
	anchor               deliveryEvidence
}
type redeliveryDetector struct {
	mu          sync.Mutex
	key         [32]byte
	initialized bool
	groups      map[string]*deliveryGroup
}

// analyze retains bounded, process-local evidence. Its caller serializes writes with the same lock.
func (d *redeliveryDetector) analyze(spans []*storage.Span, now int64) []*storage.TraceIssue {
	if !d.initialized {
		if _, err := rand.Read(d.key[:]); err != nil {
			return nil
		}
		d.initialized = true
		d.groups = make(map[string]*deliveryGroup)
	}
	for key, g := range d.groups {
		if now-g.touched > redeliveryWindow {
			delete(d.groups, key)
		}
	}
	changed := map[string]bool{}
	for _, s := range spans {
		if s.SessionID == "" || s.TraceID == "" || s.SpanID == "" || s.ServiceName == "" || s.StartNs <= 0 {
			continue
		}
		var a map[string]any
		if json.Unmarshal([]byte(s.Attributes), &a) != nil {
			continue
		}
		str := func(k string) string { v, _ := a[k].(string); return strings.TrimSpace(v) }
		op := str("messaging.operation.name")
		if op == "" {
			op = str("messaging.operation")
		}
		if op != "process" {
			continue
		}
		system, destination := str("messaging.system"), str("messaging.destination.name")
		if system == "" || destination == "" {
			continue
		}
		// Batch spans cannot reliably attribute their status to an individual message.
		if n, ok := a["messaging.batch.message_count"].(float64); ok && n != 1 {
			continue
		}
		id := str("messaging.message.id")
		attempt := 0
		if n, ok := a["messaging.message.delivery_attempt"].(float64); ok && n >= 1 && n <= 1000000 && n == float64(int(n)) {
			attempt = int(n)
		}
		if id == "" && (attempt < 3 || s.StatusCode != 2) {
			continue
		}
		// Explicit attempts without an identity can stand alone, but cannot correlate.
		identityType := "message"
		if id == "" {
			identityType = "span"
			id = s.TraceID + ":" + s.SpanID
		}
		mac := hmac.New(sha256.New, d.key[:])
		for _, part := range []string{s.SessionID, s.ServiceName, system, destination, identityType, id} {
			fmt.Fprintf(mac, "%d:%s", len(part), part)
		}
		key := hex.EncodeToString(mac.Sum(nil))
		g := d.groups[key]
		if g == nil {
			if len(d.groups) >= redeliveryMaxGroups {
				continue
			}
			var nonce [16]byte
			if _, err := rand.Read(nonce[:]); err != nil {
				continue
			}
			g = &deliveryGroup{session: s.SessionID, destination: destination, issueID: "messaging_redelivery-" + hex.EncodeToString(nonce[:]), evidence: make(map[string]deliveryEvidence)}
			d.groups[key] = g
		}
		evKey := s.TraceID + ":" + s.SpanID
		if _, exists := g.evidence[evKey]; !exists && len(g.evidence) >= redeliveryMaxSpans {
			continue
		}
		g.evidence[evKey] = deliveryEvidence{trace: s.TraceID, span: s.SpanID, parent: s.ParentSpanID, start: s.StartNs, failed: s.StatusCode == 2, attempt: attempt}
		g.touched = now
		changed[key] = true
	}
	var out []*storage.TraceIssue
	for key := range changed {
		g := d.groups[key]
		ev := make([]deliveryEvidence, 0, len(g.evidence))
		for _, e := range g.evidence {
			ev = append(ev, e)
		}
		sort.Slice(ev, func(i, j int) bool {
			if ev[i].start != ev[j].start {
				return ev[i].start < ev[j].start
			}
			if ev[i].trace != ev[j].trace {
				return ev[i].trace < ev[j].trace
			}
			return ev[i].span < ev[j].span
		})
		// Select the strongest bounded event-time window, regardless of arrival order.
		bestCount := 0
		var bestExample deliveryEvidence
		var bestFailures []deliveryEvidence
		for end := range ev {
			start := end
			for start > 0 && ev[end].start-ev[start-1].start <= redeliveryWindow {
				start--
			}
			failures, attempts := 0, 0
			var example deliveryEvidence
			var failed []deliveryEvidence
			for _, e := range ev[start : end+1] {
				if e.failed {
					failures++
					failed = append(failed, e)
					example = e
					if e.attempt > attempts {
						attempts = e.attempt
					}
				}
			}
			if failures < 3 && !(failures > 0 && attempts >= 3) {
				continue
			}
			count := failures
			if attempts > count {
				count = attempts
			}
			if count > bestCount {
				bestCount, bestExample = count, example
				bestFailures = failed
			}
		}
		if bestCount > 0 {
			if g.anchor.trace == "" {
				g.anchor = bestExample
			}
			refs := make([]string, 0, min(len(bestFailures), 8))
			for _, e := range bestFailures[:min(len(bestFailures), 8)] {
				refs = append(refs, e.trace+"/"+e.span)
			}
			fingerprint := fmt.Sprintf("%s (failed spans: %d; count is attempts or observed failures; trace/span: %s)", g.destination, len(bestFailures), strings.Join(refs, ", "))
			out = append(out, &storage.TraceIssue{ID: g.issueID, TraceID: g.anchor.trace, SessionID: g.session, Kind: "messaging_redelivery", Fingerprint: fingerprint, Count: bestCount, ParentSpanID: g.anchor.parent, ExampleSpanID: g.anchor.span, CreatedAt: now})
		}
	}
	return out
}

func (p *Pipeline) runRedeliveryDetector(traceID string) {
	spans, err := p.store.GetTrace(traceID)
	if err != nil {
		slog.Warn("redelivery detector read failed", "trace_id", traceID, "error", err)
		return
	}
	p.redeliveries.mu.Lock()
	defer p.redeliveries.mu.Unlock()
	for _, issue := range p.redeliveries.analyze(spans, time.Now().UnixNano()) {
		if err := p.store.UpsertTraceIssue(issue); err != nil {
			slog.Warn("redelivery finding write failed", "error", err)
		} else {
			telemetry.Catalog().RecordDetectorIssue(context.Background(), issue.Kind)
			if p.hub != nil {
				p.hub.Broadcast(ws.NewIssueEvent(&ws.IssuePayload{TraceID: issue.TraceID, Kind: issue.Kind, Fingerprint: issue.Fingerprint, Count: issue.Count}))
			}
		}
	}
}
